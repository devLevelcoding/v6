package events

import (
	"testing"
	"time"
)

func TestPublishReachesSubscribers(t *testing.T) {
	b := NewBroker()
	ch1, c1 := b.Subscribe("job1")
	ch2, c2 := b.Subscribe("job1")
	defer c1()
	defer c2()

	b.Publish(Event{JobID: "job1", Status: "running", Progress: 0.5})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case e := <-ch:
			if e.Progress != 0.5 {
				t.Fatalf("progress = %v, want 0.5", e.Progress)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber did not receive event")
		}
	}
}

func TestPublishIsolatedByJob(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe("job1")
	defer cancel()
	b.Publish(Event{JobID: "job2", Progress: 1})
	select {
	case e := <-ch:
		t.Fatalf("received cross-job event: %+v", e)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestCancelUnsubscribesAndCloses(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe("job1")
	if b.SubscriberCount("job1") != 1 {
		t.Fatal("want 1 subscriber")
	}
	cancel()
	if b.SubscriberCount("job1") != 0 {
		t.Fatal("subscriber not removed after cancel")
	}
	if _, open := <-ch; open {
		t.Fatal("channel should be closed after cancel")
	}
	cancel() // idempotent, must not panic
}

func TestPublishDropsWhenBufferFull(t *testing.T) {
	b := NewBroker()
	_, cancel := b.Subscribe("job1")
	defer cancel()
	// 16-deep buffer; 100 publishes must not block or panic
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.Publish(Event{JobID: "job1", Progress: float64(i)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full subscriber buffer")
	}
}
