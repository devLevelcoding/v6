// Package profilesvc implements the internal ProfileService gRPC server:
// an in-memory map guarded by a mutex, returning gRPC status codes
// (codes.NotFound etc.) instead of plain Go errors.
package profilesvc

import (
	"context"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gosocial/pb"
)

// Service implements pb.ProfileServiceServer with an in-memory store.
type Service struct {
	pb.UnimplementedProfileServiceServer
	mu       sync.RWMutex
	profiles map[string]*pb.Profile
}

func New() *Service {
	return &Service{profiles: make(map[string]*pb.Profile)}
}

// GetProfile returns the profile for the given user ID.
func (s *Service) GetProfile(_ context.Context, req *pb.GetProfileRequest) (*pb.Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.profiles[req.UserId]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "profile %q not found", req.UserId)
	}
	return p, nil
}

// ListProfiles streams profiles for a batch of user IDs, one by one.
func (s *Service) ListProfiles(req *pb.ListProfilesRequest, stream pb.ProfileService_ListProfilesServer) error {
	s.mu.RLock()
	var matches []*pb.Profile
	for _, id := range req.UserIds {
		if p, ok := s.profiles[id]; ok {
			matches = append(matches, p)
		}
	}
	s.mu.RUnlock()

	for _, p := range matches {
		if err := stream.Send(p); err != nil {
			return err
		}
	}
	return nil
}

// UpsertProfile creates or updates a user's profile record.
func (s *Service) UpsertProfile(_ context.Context, req *pb.Profile) (*pb.Profile, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	s.mu.Lock()
	s.profiles[req.UserId] = req
	s.mu.Unlock()
	return req, nil
}

// Count returns the number of stored profiles (for diagnostics).
func (s *Service) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.profiles)
}
