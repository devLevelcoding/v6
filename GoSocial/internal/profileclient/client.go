// Package profileclient dials the profilesvc gRPC service and wraps every
// call in a circuit breaker so a dead or flaky profilesvc fails fast
// (ErrCircuitOpen) instead of hanging on dead-TCP timeouts, letting the
// caller degrade gracefully rather than stall the request.
package profileclient

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"gosocial/internal/breaker"
	"gosocial/pb"
)

// Client wraps a gRPC connection to profilesvc behind a circuit breaker.
type Client struct {
	conn    *grpc.ClientConn
	rpc     pb.ProfileServiceClient
	breaker *breaker.CircuitBreaker[*pb.Profile]
}

// Dial connects to profilesvc at addr (e.g. "profilesvc:9091") and
// configures a breaker that trips after 3 consecutive failures, probes
// again after 2s.
func Dial(addr string) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial profilesvc: %w", err)
	}
	cb := breaker.New[*pb.Profile](breaker.Config{
		MaxFailures:      3,
		Timeout:          2 * time.Second,
		SuccessThreshold: 1,
		OnStateChange: func(from, to breaker.State) {
			fmt.Printf("[profileclient breaker] %s -> %s\n", from, to)
		},
	})
	return &Client{conn: conn, rpc: pb.NewProfileServiceClient(conn), breaker: cb}, nil
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error { return c.conn.Close() }

// Breaker exposes the underlying breaker for /debug inspection.
func (c *Client) Breaker() *breaker.CircuitBreaker[*pb.Profile] { return c.breaker }

// GetProfile calls profilesvc.GetProfile through the circuit breaker.
func (c *Client) GetProfile(ctx context.Context, userID string) (*pb.Profile, error) {
	return c.breaker.Execute(ctx, func(ctx context.Context) (*pb.Profile, error) {
		ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		defer cancel()
		return c.rpc.GetProfile(ctx, &pb.GetProfileRequest{UserId: userID})
	})
}

// RPCUpsert calls profilesvc.UpsertProfile through the circuit breaker.
// Called by the main API's /auth/register handler to write profile data.
func (c *Client) RPCUpsert(ctx context.Context, userID, username, displayName string) (*pb.Profile, error) {
	return c.breaker.Execute(ctx, func(ctx context.Context) (*pb.Profile, error) {
		ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		defer cancel()
		return c.rpc.UpsertProfile(ctx, &pb.Profile{
			UserId: userID, Username: username, DisplayName: displayName,
		})
	})
}
