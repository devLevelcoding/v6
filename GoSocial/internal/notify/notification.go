package notify

import "time"

// Notification is the payload pushed to a connected client.
type Notification struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // follow_request | follow_accepted | new_message | new_post
	ToUserID  string    `json:"to_user_id"`
	FromID    string    `json:"from_id,omitempty"`
	FromName  string    `json:"from_name,omitempty"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}
