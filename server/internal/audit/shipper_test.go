package audit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// recordingSink stands in for a SIEM and can be told to misbehave.
type recordingSink struct {
	mu       sync.Mutex
	batches  [][]Shipped
	failNext int
	block    chan struct{}
	closed   bool
}

func (s *recordingSink) Name() string { return "test" }

func (s *recordingSink) Send(ctx context.Context, events []Shipped) error {
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failNext > 0 {
		s.failNext--
		return errors.New("the sink is having a bad day")
	}
	s.batches = append(s.batches, append([]Shipped(nil), events...))
	return nil
}

func (s *recordingSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *recordingSink) delivered() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for _, batch := range s.batches {
		count += len(batch)
	}
	return count
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestShipperDeliversWhatItIsGiven(t *testing.T) {
	sink := &recordingSink{}
	shipper := NewShipper(sink, quietLogger(), ShipperOptions{BatchSize: 2, BatchWait: 20 * time.Millisecond})
	t.Cleanup(func() { _ = shipper.Close(context.Background()) })

	for i := 0; i < 5; i++ {
		shipper.Enqueue(Shipped{Action: "cluster.read", Result: "success"})
	}

	waitFor(t, "five events", func() bool { return sink.delivered() == 5 })
}

// The whole reason the queue is bounded and Enqueue never blocks: a slow sink must not
// hold up the request that produced the event, and must never stop one being recorded.
func TestAStalledSinkNeverBlocksTheCaller(t *testing.T) {
	sink := &recordingSink{block: make(chan struct{})}
	shipper := NewShipper(sink, quietLogger(), ShipperOptions{
		QueueSize: 4, BatchSize: 1, BatchWait: time.Millisecond,
	})
	t.Cleanup(func() {
		close(sink.block)
		_ = shipper.Close(context.Background())
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Far more than the queue holds. If Enqueue blocked, this would never finish.
		for i := 0; i < 500; i++ {
			shipper.Enqueue(Shipped{Action: "pod.deleted"})
		}
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Enqueue blocked on a stalled sink; audit writing must never wait on shipping")
	}

	// And the loss is counted rather than silent, because a dropped audit record is
	// itself worth an alarm.
	waitFor(t, "dropped events to be counted", func() bool { return shipper.Stats().Dropped > 0 })
}

func TestShipperRetriesThenGivesUpAndCountsTheLoss(t *testing.T) {
	// One more failure than it will tolerate.
	sink := &recordingSink{failNext: 4}
	shipper := NewShipper(sink, quietLogger(), ShipperOptions{
		BatchSize: 1, BatchWait: time.Millisecond, MaxTries: 3,
	})
	t.Cleanup(func() { _ = shipper.Close(context.Background()) })

	shipper.Enqueue(Shipped{Action: "cluster.deleted"})

	waitFor(t, "the batch to be given up on", func() bool { return shipper.Stats().Dropped == 1 })

	stats := shipper.Stats()
	if stats.Retries == 0 {
		t.Error("the batch was dropped without being retried")
	}
	if stats.Sent != 0 {
		t.Errorf("nothing was delivered, but %d were counted as sent", stats.Sent)
	}
	if stats.LastFail == "" {
		t.Error("the last failure should be reported, so an admin can see why")
	}
}

// A transient failure is exactly what the retry is for.
func TestShipperRecoversFromATemporaryFailure(t *testing.T) {
	sink := &recordingSink{failNext: 2}
	shipper := NewShipper(sink, quietLogger(), ShipperOptions{
		BatchSize: 1, BatchWait: time.Millisecond, MaxTries: 5,
	})
	t.Cleanup(func() { _ = shipper.Close(context.Background()) })

	shipper.Enqueue(Shipped{Action: "node.drained"})

	waitFor(t, "delivery after retries", func() bool { return sink.delivered() == 1 })
	if dropped := shipper.Stats().Dropped; dropped != 0 {
		t.Errorf("recovered but %d events were counted as dropped", dropped)
	}
}

// What is queued at shutdown was produced by something that already happened; it is
// flushed rather than thrown away.
func TestCloseFlushesWhatIsQueued(t *testing.T) {
	sink := &recordingSink{}
	shipper := NewShipper(sink, quietLogger(), ShipperOptions{
		BatchSize: 1000, BatchWait: time.Hour, // neither would fire on its own
	})

	for i := 0; i < 7; i++ {
		shipper.Enqueue(Shipped{Action: "settings.changed"})
	}

	if err := shipper.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if delivered := sink.delivered(); delivered != 7 {
		t.Fatalf("shutdown delivered %d of 7 queued events", delivered)
	}
	if !sink.closed {
		t.Error("the sink was not closed")
	}
}

func TestBackoffGrowsAndStaysBounded(t *testing.T) {
	previous := time.Duration(0)
	for attempt := 1; attempt <= 8; attempt++ {
		wait := backoff(attempt)
		if wait > maxBackoff {
			t.Fatalf("attempt %d waits %s, past the %s ceiling", attempt, wait, maxBackoff)
		}
		// Jitter means it is not strictly monotonic, but the floor must still climb.
		if attempt < 6 && wait < previous/2 {
			t.Errorf("attempt %d waited %s, less than half of the previous %s", attempt, wait, previous)
		}
		previous = wait
	}
}
