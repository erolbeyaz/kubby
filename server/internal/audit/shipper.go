package audit

import (
	"context"
	"log/slog"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// Sink is somewhere audit events are copied to — a SIEM, a log store, a collector.
//
// Copied, never moved: the database and the log stream are written first and are not
// conditional on this succeeding. An audit trail that a misconfigured shipper can silence
// is not an audit trail (ADR-013 #5).
type Sink interface {
	// Name identifies the sink in logs and metrics.
	Name() string
	// Send delivers a batch. Returning an error asks for a retry; the shipper decides
	// how many times and how long to wait.
	Send(ctx context.Context, events []Shipped) error
	Close() error
}

// Shipped is an audit event flattened for transport. It is deliberately a separate type
// from Event: what goes to a third-party system is a contract, and letting it drift with
// an internal struct is how a SIEM query breaks silently.
type Shipped struct {
	Timestamp    time.Time      `json:"@timestamp"`
	Action       string         `json:"action"`
	Result       string         `json:"result"`
	ActorEmail   string         `json:"actor,omitempty"`
	ActorID      string         `json:"actorId,omitempty"`
	ClusterID    string         `json:"clusterId,omitempty"`
	Namespace    string         `json:"namespace,omitempty"`
	ResourceKind string         `json:"resourceKind,omitempty"`
	ResourceName string         `json:"resourceName,omitempty"`
	IPAddress    string         `json:"clientIp,omitempty"`
	RequestID    string         `json:"requestId,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
}

// ShipperStats is what the shipper will report through /metrics.
type ShipperStats struct {
	Sink     string `json:"sink"`
	Queued   int    `json:"queued"`
	Sent     uint64 `json:"sent"`
	Failed   uint64 `json:"failed"`
	Dropped  uint64 `json:"dropped"`
	Retries  uint64 `json:"retries"`
	LastFail string `json:"lastError,omitempty"`
}

// Shipper copies audit events to a sink without ever making the caller wait.
//
// The queue is bounded and Enqueue never blocks. A sink that is slow, unreachable or
// wedged must not slow down the request that produced the event, and must never be able
// to stop one being recorded — so when the queue is full the event is dropped here and
// counted. A dropped audit record is itself worth an alarm, which is what the counter and
// the error log are for; losing them silently would be the worst outcome of the three.
type Shipper struct {
	sink   Sink
	logger *slog.Logger

	queue chan Shipped
	done  chan struct{}
	stop  context.CancelFunc
	once  sync.Once

	sent    atomic.Uint64
	failed  atomic.Uint64
	dropped atomic.Uint64
	retries atomic.Uint64

	mu       sync.Mutex
	lastFail string

	batchSize int
	batchWait time.Duration
	maxTries  int
}

// ShipperOptions tune the batching. The defaults suit a SIEM: batch enough to be cheap,
// flush often enough that an event is not sitting in memory when the process is killed.
type ShipperOptions struct {
	QueueSize int
	BatchSize int
	BatchWait time.Duration
	MaxTries  int
}

func NewShipper(sink Sink, logger *slog.Logger, opts ShipperOptions) *Shipper {
	if opts.QueueSize <= 0 {
		opts.QueueSize = defaultQueueSize
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = defaultBatchSize
	}
	if opts.BatchWait <= 0 {
		opts.BatchWait = defaultBatchWait
	}
	if opts.MaxTries <= 0 {
		opts.MaxTries = defaultMaxTries
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Shipper{
		sink:      sink,
		logger:    logger.With(slog.String("component", "audit-shipper"), slog.String("sink", sink.Name())),
		queue:     make(chan Shipped, opts.QueueSize),
		done:      make(chan struct{}),
		stop:      cancel,
		batchSize: opts.BatchSize,
		batchWait: opts.BatchWait,
		maxTries:  opts.MaxTries,
	}

	go s.run(ctx)
	return s
}

// Enqueue offers an event. It never blocks and never fails: a full queue drops the event
// and counts it, because the alternative is holding up the request that produced it.
func (s *Shipper) Enqueue(ev Shipped) {
	select {
	case s.queue <- ev:
	default:
		dropped := s.dropped.Add(1)
		// Logged every time, not sampled: each line is one audit record that did not
		// reach the SIEM, and the count is what an alert would fire on.
		s.logger.Error("audit event dropped: the shipping queue is full",
			slog.String("action", ev.Action),
			slog.Uint64("dropped_total", dropped))
	}
}

func (s *Shipper) Stats() ShipperStats {
	s.mu.Lock()
	lastFail := s.lastFail
	s.mu.Unlock()

	return ShipperStats{
		Sink:     s.sink.Name(),
		Queued:   len(s.queue),
		Sent:     s.sent.Load(),
		Failed:   s.failed.Load(),
		Dropped:  s.dropped.Load(),
		Retries:  s.retries.Load(),
		LastFail: lastFail,
	}
}

// Close stops the worker and gives it a moment to flush what is queued. Events still in
// the queue when the deadline passes are counted as dropped rather than forgotten.
func (s *Shipper) Close(ctx context.Context) error {
	s.once.Do(func() {
		close(s.queue)
		select {
		case <-s.done:
		case <-ctx.Done():
			// The worker is still going; stopping it is better than hanging shutdown.
			s.stop()
			<-s.done
		}
		s.stop()
	})
	return s.sink.Close()
}

func (s *Shipper) run(ctx context.Context) {
	defer close(s.done)

	batch := make([]Shipped, 0, s.batchSize)
	timer := time.NewTimer(s.batchWait)
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		s.deliver(ctx, batch)
		batch = batch[:0]
	}

	for {
		select {
		case ev, ok := <-s.queue:
			if !ok {
				// Closed: send what is left and finish.
				flush()
				return
			}
			batch = append(batch, ev)
			if len(batch) >= s.batchSize {
				flush()
				resetTimer(timer, s.batchWait)
			}

		case <-timer.C:
			flush()
			resetTimer(timer, s.batchWait)

		case <-ctx.Done():
			return
		}
	}
}

// deliver retries with exponential backoff and jitter, then gives up and counts the loss.
//
// Bounded rather than infinite: a permanently misconfigured sink would otherwise retry
// the same batch forever while everything behind it queues up and is dropped, which turns
// one broken batch into total loss.
func (s *Shipper) deliver(ctx context.Context, batch []Shipped) {
	events := make([]Shipped, len(batch))
	copy(events, batch)

	for attempt := 0; attempt < s.maxTries; attempt++ {
		if attempt > 0 {
			s.retries.Add(1)
			if !sleepCtx(ctx, backoff(attempt)) {
				s.countLoss(events, "shutdown during retry")
				return
			}
		}

		sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
		err := s.sink.Send(sendCtx, events)
		cancel()

		if err == nil {
			s.sent.Add(uint64(len(events)))
			return
		}

		s.failed.Add(1)
		s.mu.Lock()
		s.lastFail = err.Error()
		s.mu.Unlock()

		s.logger.Warn("audit batch failed",
			slog.Int("events", len(events)),
			slog.Int("attempt", attempt+1),
			slog.String("error", err.Error()))
	}

	s.countLoss(events, "the sink refused the batch after every retry")
}

func (s *Shipper) countLoss(events []Shipped, why string) {
	dropped := s.dropped.Add(uint64(len(events)))
	s.logger.Error("audit events lost",
		slog.Int("events", len(events)),
		slog.String("reason", why),
		slog.Uint64("dropped_total", dropped))
}

// backoff grows exponentially and is jittered, so several shippers recovering from the
// same outage do not retry in lockstep.
func backoff(attempt int) time.Duration {
	wait := time.Duration(math.Pow(2, float64(attempt))) * baseBackoff
	if wait > maxBackoff {
		wait = maxBackoff
	}
	jitter := time.Duration(rand.Int64N(int64(wait / 2)))
	return wait/2 + jitter
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

const (
	// Large enough to ride out a restart of the sink, small enough that a wedged one
	// costs bounded memory rather than the process.
	defaultQueueSize = 4096
	defaultBatchSize = 100
	defaultBatchWait = 2 * time.Second
	defaultMaxTries  = 4

	baseBackoff = 500 * time.Millisecond
	maxBackoff  = 30 * time.Second
	sendTimeout = 20 * time.Second
)
