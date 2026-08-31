package queue

import (
	"context"
	"testing"
)

// CoverGo U1 — queue hand-off throughput. The Mem queue is a bounded channel;
// this fixes a baseline before a later phase swaps in Redis/NATS.

func BenchmarkPushClaimSerial(b *testing.B) {
	q := NewMem(1024)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_ = q.Push(ctx, "job-x")
		_, _ = q.Claim(ctx)
	}
}

func BenchmarkPushClaimParallel(b *testing.B) {
	q := NewMem(1024)
	ctx := context.Background()
	done := make(chan struct{})
	defer close(done)
	// one consumer keeping the queue drained
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				_, _ = q.Claim(ctx)
			}
		}
	}()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = q.Push(ctx, "job-x")
		}
	})
}
