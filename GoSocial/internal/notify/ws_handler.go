package notify

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"gosocial/internal/auth"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// WSHandler upgrades /ws/notifications connections and registers them
// with the Hub under the authenticated user's ID as "room".
type WSHandler struct {
	hub    *Hub
	secret string
}

func NewWSHandler(h *Hub, jwtSecret string) *WSHandler {
	return &WSHandler{hub: h, secret: jwtSecret}
}

// ServeHTTP upgrades the connection. Query params: ?token=<JWT access token>
func (wh *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	claims, err := auth.ValidateToken(token, wh.secret)
	if err != nil {
		http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
		return
	}
	userID := claims.Subject

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[notify] upgrade error: %v", err)
		return
	}

	clientID := fmt.Sprintf("%s-%d", userID, time.Now().UnixNano())
	client := &Client{
		ID:     clientID,
		UserID: userID,
		Room:   userID, // one "room" per user == their personal notification channel
		Conn:   conn,
		Send:   make(chan []byte, 256),
	}
	wh.hub.Register(client)

	go wh.writePump(client)
	wh.readPump(client)
}

func (wh *WSHandler) readPump(c *Client) {
	defer func() {
		wh.hub.Unregister(c)
		c.Conn.Close()
	}()
	c.Conn.SetReadLimit(2048)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	for {
		// Notifications are server-push only; clients don't send content,
		// but we still need to read to process control frames (ping/close)
		// and detect disconnects.
		if _, _, err := c.Conn.ReadMessage(); err != nil {
			break
		}
	}
}

func (wh *WSHandler) writePump(c *Client) {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
