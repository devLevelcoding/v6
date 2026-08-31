package notify

import (
	"encoding/json"
	"fmt"
	"time"
)

// Notifier marshals and pushes real-time notifications through a Hub.
type Notifier struct {
	hub *Hub
}

func NewNotifier(hub *Hub) *Notifier { return &Notifier{hub: hub} }

// Push sends ntype/message to toUserID's currently-connected WebSocket
// clients. If the user isn't connected, this is a no-op.
func (n *Notifier) Push(ntype, toUserID, fromID, fromName, message string) {
	note := Notification{
		ID:        fmt.Sprintf("notif-%d", time.Now().UnixNano()),
		Type:      ntype,
		ToUserID:  toUserID,
		FromID:    fromID,
		FromName:  fromName,
		Message:   message,
		Timestamp: time.Now().UTC(),
	}
	data, err := json.Marshal(note)
	if err != nil {
		return
	}
	n.hub.BroadcastToRoom(toUserID, data)
}
