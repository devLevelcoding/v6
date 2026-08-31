// Package notify is a WebSocket pub/sub hub: clients register into a
// "room" (here, always the receiving user's ID -- see ws_handler.go),
// and BroadcastToRoom delivers a real-time notification ("someone
// followed you", "someone messaged you") to every connection that user
// currently has open.
package notify

import (
	"sync"

	"github.com/gorilla/websocket"
)

// Client is a connected WebSocket client.
type Client struct {
	ID     string
	UserID string
	Room   string
	Conn   *websocket.Conn
	Send   chan []byte
}

type registerMsg struct{ client *Client }
type unregisterMsg struct{ client *Client }
type broadcastRoomMsg struct {
	room string
	msg  []byte
}
type broadcastAllMsg struct{ msg []byte }

// Hub maintains the set of active clients and broadcasts messages.
type Hub struct {
	mu        sync.RWMutex
	clients   map[string]*Client
	rooms     map[string]map[string]*Client
	register  chan *Client
	unregist  chan *Client
	bcastRoom chan broadcastRoomMsg
	bcastAll  chan broadcastAllMsg
}

// New creates a new Hub.
func New() *Hub {
	return &Hub{
		clients:   make(map[string]*Client),
		rooms:     make(map[string]map[string]*Client),
		register:  make(chan *Client, 64),
		unregist:  make(chan *Client, 64),
		bcastRoom: make(chan broadcastRoomMsg, 256),
		bcastAll:  make(chan broadcastAllMsg, 256),
	}
}

// Register adds a client to the hub (non-blocking send).
func (h *Hub) Register(c *Client) { h.register <- c }

// Unregister removes a client from the hub.
func (h *Hub) Unregister(c *Client) { h.unregist <- c }

// BroadcastToRoom sends a message to every client in the given room.
func (h *Hub) BroadcastToRoom(room string, msg []byte) {
	h.bcastRoom <- broadcastRoomMsg{room: room, msg: msg}
}

// BroadcastToAll sends a message to every connected client.
func (h *Hub) BroadcastToAll(msg []byte) {
	h.bcastAll <- broadcastAllMsg{msg: msg}
}

// RoomStats returns a snapshot of {room: memberCount}.
func (h *Hub) RoomStats() map[string]int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[string]int, len(h.rooms))
	for room, members := range h.rooms {
		out[room] = len(members)
	}
	return out
}

// Run starts the hub event loop. Call in a goroutine.
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.mu.Lock()
			h.clients[c.ID] = c
			if h.rooms[c.Room] == nil {
				h.rooms[c.Room] = make(map[string]*Client)
			}
			h.rooms[c.Room][c.ID] = c
			h.mu.Unlock()

		case c := <-h.unregist:
			h.mu.Lock()
			if _, ok := h.clients[c.ID]; ok {
				delete(h.clients, c.ID)
				if room, ok2 := h.rooms[c.Room]; ok2 {
					delete(room, c.ID)
					if len(room) == 0 {
						delete(h.rooms, c.Room)
					}
				}
				close(c.Send)
			}
			h.mu.Unlock()

		case bm := <-h.bcastRoom:
			h.mu.RLock()
			members := h.rooms[bm.room]
			h.mu.RUnlock()
			for _, c := range members {
				select {
				case c.Send <- bm.msg:
				default:
				}
			}

		case bm := <-h.bcastAll:
			h.mu.RLock()
			all := make([]*Client, 0, len(h.clients))
			for _, c := range h.clients {
				all = append(all, c)
			}
			h.mu.RUnlock()
			for _, c := range all {
				select {
				case c.Send <- bm.msg:
				default:
				}
			}
		}
	}
}
