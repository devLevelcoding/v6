package events

import (
	"encoding/json"
	"time"
)

// Event type constants for the social domain.
const (
	EventUserRegistered  = "UserRegistered"
	EventPostCreated     = "PostCreated"
	EventFollowRequested = "FollowRequested"
	EventFollowAccepted  = "FollowAccepted"
	EventFollowRejected  = "FollowRejected"
	EventMessageSent     = "MessageSent"
)

// FeedStreamID is the single global stream every social event (posts,
// follows, messages) is appended to, so ReplayAll reconstructs the
// entire app's read state from one stream.
const FeedStreamID = "feed"

// ── Event payloads ──────────────────────────────────────────────────────────

type UserRegisteredPayload struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

type PostCreatedPayload struct {
	PostID   string `json:"post_id"`
	AuthorID string `json:"author_id"`
	Type     string `json:"type"` // post|story|real
	Content  string `json:"content"`
	SongID   int    `json:"song_id,omitempty"`
}

type FollowRequestedPayload struct {
	RequestID  string `json:"request_id"`
	FollowerID string `json:"follower_id"`
	FolloweeID string `json:"followee_id"`
}

type FollowAcceptedPayload struct {
	RequestID  string `json:"request_id"`
	FollowerID string `json:"follower_id"`
	FolloweeID string `json:"followee_id"`
}

type FollowRejectedPayload struct {
	RequestID string `json:"request_id"`
}

type MessageSentPayload struct {
	MessageID string `json:"message_id"`
	FromID    string `json:"from_id"`
	ToID      string `json:"to_id"`
	Content   string `json:"content"`
}

// ── Read model (the "event-sourced feed") ───────────────────────────────────

type User struct {
	ID          string
	Username    string
	DisplayName string
}

type Post struct {
	ID         string
	AuthorID   string
	Type       string
	Content    string
	SongID     int
	OccurredAt time.Time
}

// FollowEdge tracks one direction of a follow relationship.
type FollowEdge struct {
	FollowerID string
	FolloweeID string
	Status     string // pending|accepted|rejected
	RequestID  string
}

// FeedState is the fully-projected read model, rebuilt entirely by
// replaying events -- never mutated directly by request handlers.
type FeedState struct {
	Version int
	Users   map[string]*User
	Posts   []*Post
	// Follows keyed by requestID so accept/reject can find the right edge.
	Follows map[string]*FollowEdge
	// Messages is a flat log of sent DMs (gating is enforced by the caller
	// checking mutual-follow via IsMutualFollow before raising the event).
	Messages []MessageSentPayload
}

// NewFeedState returns an empty, ready-to-replay-into FeedState.
func NewFeedState() *FeedState {
	return &FeedState{
		Version: -1,
		Users:   map[string]*User{},
		Follows: map[string]*FollowEdge{},
	}
}

// Apply mutates the read model based on one event.
func (f *FeedState) Apply(ev EventRecord) {
	switch ev.EventType {
	case EventUserRegistered:
		var p UserRegisteredPayload
		if json.Unmarshal(ev.Payload, &p) == nil {
			f.Users[p.UserID] = &User{ID: p.UserID, Username: p.Username, DisplayName: p.DisplayName}
		}
	case EventPostCreated:
		var p PostCreatedPayload
		if json.Unmarshal(ev.Payload, &p) == nil {
			f.Posts = append(f.Posts, &Post{
				ID: p.PostID, AuthorID: p.AuthorID, Type: p.Type,
				Content: p.Content, SongID: p.SongID, OccurredAt: ev.OccurredAt,
			})
		}
	case EventFollowRequested:
		var p FollowRequestedPayload
		if json.Unmarshal(ev.Payload, &p) == nil {
			f.Follows[p.RequestID] = &FollowEdge{
				FollowerID: p.FollowerID, FolloweeID: p.FolloweeID,
				Status: "pending", RequestID: p.RequestID,
			}
		}
	case EventFollowAccepted:
		var p FollowAcceptedPayload
		if json.Unmarshal(ev.Payload, &p) == nil {
			if edge, ok := f.Follows[p.RequestID]; ok {
				edge.Status = "accepted"
			}
		}
	case EventFollowRejected:
		var p FollowRejectedPayload
		if json.Unmarshal(ev.Payload, &p) == nil {
			if edge, ok := f.Follows[p.RequestID]; ok {
				edge.Status = "rejected"
			}
		}
	case EventMessageSent:
		var p MessageSentPayload
		if json.Unmarshal(ev.Payload, &p) == nil {
			f.Messages = append(f.Messages, p)
		}
	}
	f.Version = ev.Version
}

// Rebuild replays a full slice of persisted events to construct a
// FeedState from scratch. See replay.go for point-in-time variants.
func Rebuild(evs []EventRecord) *FeedState {
	f := NewFeedState()
	for _, ev := range evs {
		f.Apply(ev)
	}
	return f
}

// IsMutualFollow reports whether both directions between a and b are
// "accepted" -- gates whether the two users are allowed to DM each other.
func (f *FeedState) IsMutualFollow(a, b string) bool {
	var aFollowsB, bFollowsA bool
	for _, edge := range f.Follows {
		if edge.Status != "accepted" {
			continue
		}
		if edge.FollowerID == a && edge.FolloweeID == b {
			aFollowsB = true
		}
		if edge.FollowerID == b && edge.FolloweeID == a {
			bFollowsA = true
		}
	}
	return aFollowsB && bFollowsA
}
