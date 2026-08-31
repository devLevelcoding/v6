// Package ws is a small, dependency-free RFC 6455 WebSocket server: the HTTP
// Upgrade handshake and a Conn that reads and writes messages (text/binary),
// answers pings, and does the close handshake. No extensions (no
// permessage-deflate), no fragmented sends. It is deliberately just enough for
// browsers and GoStream's own clients — the fan-out logic lives in
// internal/hub.
package ws

import (
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
)

// guid is the RFC 6455 magic string appended to the client key before hashing.
const guid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// ErrBadHandshake is returned when the request is not a valid WebSocket upgrade.
var ErrBadHandshake = errors.New("ws: not a websocket upgrade request")

// Upgrade completes the handshake and hijacks the connection. On success the
// caller owns the returned Conn and must Close it.
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !headerContains(r.Header, "Connection", "upgrade") ||
		!strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, ErrBadHandshake
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		return nil, ErrBadHandshake
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, ErrBadHandshake
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("ws: ResponseWriter does not support Hijack")
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}

	accept := acceptKey(key)
	_, err = rw.WriteString(
		"HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n")
	if err == nil {
		err = rw.Flush()
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	return newConn(conn, rw.Reader), nil
}

func acceptKey(clientKey string) string {
	h := sha1.New()
	h.Write([]byte(clientKey + guid))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func headerContains(h http.Header, name, token string) bool {
	for _, v := range h[http.CanonicalHeaderKey(name)] {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}
	return false
}
