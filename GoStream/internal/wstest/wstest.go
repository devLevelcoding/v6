// Package wstest is a minimal WebSocket *client* for exercising GoStream's
// hand-rolled server (internal/ws) in tests: it does the client handshake,
// masks what it sends and unmasks what it reads. Not for production use.
package wstest

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

const guid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Client is a connected test client.
type Client struct {
	conn net.Conn
	br   *bufio.Reader
}

// Dial connects to a ws:// or http:// URL and completes the handshake.
func Dial(rawURL string) (*Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if u.Port() == "" {
		host += ":80"
	}
	conn, err := net.DialTimeout("tcp", host, 5*time.Second)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	var kb [16]byte
	_, _ = rand.Read(kb[:])
	key := base64.StdEncoding.EncodeToString(kb[:])

	path := u.RequestURI()
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", path, u.Host, key)
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, fmt.Errorf("wstest: handshake status %d", resp.StatusCode)
	}
	h := sha1.Sum([]byte(key + guid))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(h[:]) {
		conn.Close()
		return nil, fmt.Errorf("wstest: bad Sec-WebSocket-Accept")
	}
	// Keep a generous deadline so a test bug surfaces as an error, not a hang.
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	return &Client{conn: conn, br: br}, nil
}

// SetDeadline bounds every subsequent read/write.
func (c *Client) SetDeadline(t time.Time) { _ = c.conn.SetDeadline(t) }

// WriteText sends a masked text frame.
func (c *Client) WriteText(s string) error { return c.write(0x1, []byte(s)) }

// WriteJSON marshals v and sends it as a text frame.
func (c *Client) WriteJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.write(0x1, b)
}

// WriteRaw sends a frame with an arbitrary opcode (for protocol tests).
func (c *Client) WriteRaw(opcode byte, payload []byte) error { return c.write(opcode, payload) }

// WriteUnmasked sends a client frame WITHOUT masking — the server must reject it.
func (c *Client) WriteUnmasked(opcode byte, payload []byte) error {
	head := []byte{0x80 | opcode, byte(len(payload))}
	if _, err := c.conn.Write(head); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}

func (c *Client) write(opcode byte, payload []byte) error {
	var mask [4]byte
	_, _ = rand.Read(mask[:])

	var head []byte
	b0 := byte(0x80) | opcode
	switch n := len(payload); {
	case n <= 125:
		head = []byte{b0, 0x80 | byte(n)}
	case n <= 0xffff:
		head = []byte{b0, 0x80 | 126, byte(n >> 8), byte(n)}
	default:
		head = make([]byte, 10)
		head[0], head[1] = b0, 0x80|127
		binary.BigEndian.PutUint64(head[2:], uint64(n))
	}
	head = append(head, mask[:]...)
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i&3]
	}
	if _, err := c.conn.Write(head); err != nil {
		return err
	}
	_, err := c.conn.Write(masked)
	return err
}
