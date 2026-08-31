// store.go is the event-sourced domain layer: every mutation appends an
// event to internal/events.EventStore under one global stream, and the
// read model is whatever internal/events.Rebuild/ReplayAll/ReplayUpTo
// project from that stream -- there is no other source of truth.
package social

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"gosocial/internal/events"
)

// SocialStore owns the real event store and provides domain operations
// that raise real events instead of mutating a CRUD map directly.
type SocialStore struct {
	mu sync.Mutex
	es *events.InMemoryEventStore
}

func NewSocialStore() *SocialStore {
	return &SocialStore{es: events.NewInMemoryEventStore()}
}

// currentVersion returns the current version of the feed stream, or -1
// if it doesn't exist yet.
func (s *SocialStore) currentVersion() int {
	evs, err := s.es.LoadEvents(events.FeedStreamID)
	if err != nil {
		return -1
	}
	return len(evs) - 1
}

func (s *SocialStore) append(ev events.EventRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	expected := s.currentVersion()
	return s.es.AppendEvents(events.FeedStreamID, expected, []events.EventRecord{ev})
}

func marshalEvent(eventType string, payload any) events.EventRecord {
	// Marshal errors are impossible for the fixed payload shapes this
	// package raises (all plain structs of strings/ints), so it's ignored.
	data, _ := json.Marshal(payload)
	return events.EventRecord{EventType: eventType, Payload: data}
}

// RegisterUser raises a UserRegistered event.
func (s *SocialStore) RegisterUser(userID, username, displayName string) error {
	return s.append(marshalEvent(events.EventUserRegistered, events.UserRegisteredPayload{
		UserID: userID, Username: username, DisplayName: displayName,
	}))
}

// CreatePost raises a PostCreated event and returns the new post's ID.
func (s *SocialStore) CreatePost(authorID, ptype, content string, songID int) (string, error) {
	postID := uuid.NewString()
	err := s.append(marshalEvent(events.EventPostCreated, events.PostCreatedPayload{
		PostID: postID, AuthorID: authorID, Type: ptype, Content: content, SongID: songID,
	}))
	return postID, err
}

// RequestFollow raises a FollowRequested event and returns the new
// request's ID.
func (s *SocialStore) RequestFollow(followerID, followeeID string) (string, error) {
	if followerID == followeeID {
		return "", fmt.Errorf("cannot follow yourself")
	}
	reqID := uuid.NewString()
	err := s.append(marshalEvent(events.EventFollowRequested, events.FollowRequestedPayload{
		RequestID: reqID, FollowerID: followerID, FolloweeID: followeeID,
	}))
	return reqID, err
}

// AcceptFollow raises a FollowAccepted event. actingUserID must be the
// followee (only the person being followed can accept).
func (s *SocialStore) AcceptFollow(requestID, actingUserID string) error {
	state := s.State()
	edge, ok := state.Follows[requestID]
	if !ok {
		return fmt.Errorf("follow request %q not found", requestID)
	}
	if edge.FolloweeID != actingUserID {
		return fmt.Errorf("only the target user can accept a follow request")
	}
	return s.append(marshalEvent(events.EventFollowAccepted, events.FollowAcceptedPayload{
		RequestID: requestID, FollowerID: edge.FollowerID, FolloweeID: edge.FolloweeID,
	}))
}

// RejectFollow raises a FollowRejected event.
func (s *SocialStore) RejectFollow(requestID, actingUserID string) error {
	state := s.State()
	edge, ok := state.Follows[requestID]
	if !ok {
		return fmt.Errorf("follow request %q not found", requestID)
	}
	if edge.FolloweeID != actingUserID {
		return fmt.Errorf("only the target user can reject a follow request")
	}
	return s.append(marshalEvent(events.EventFollowRejected, events.FollowRejectedPayload{RequestID: requestID}))
}

// SendMessage raises a MessageSent event, but only if fromID and toID
// mutually follow each other.
func (s *SocialStore) SendMessage(fromID, toID, content string) (string, error) {
	state := s.State()
	if !state.IsMutualFollow(fromID, toID) {
		return "", fmt.Errorf("messages require a mutual (accepted, both directions) follow")
	}
	msgID := uuid.NewString()
	err := s.append(marshalEvent(events.EventMessageSent, events.MessageSentPayload{
		MessageID: msgID, FromID: fromID, ToID: toID, Content: content,
	}))
	return msgID, err
}

// State returns the current read model, built by replaying every event
// in the stream from scratch.
func (s *SocialStore) State() *events.FeedState {
	evs, err := s.es.LoadEvents(events.FeedStreamID)
	if err != nil {
		return events.NewFeedState()
	}
	return events.Rebuild(evs)
}

// Events returns a copy of every raw event in the feed stream, used by
// the /debug/replay endpoint for point-in-time replay.
func (s *SocialStore) Events() []events.EventRecord {
	evs, err := s.es.LoadEvents(events.FeedStreamID)
	if err != nil {
		return nil
	}
	return evs
}
