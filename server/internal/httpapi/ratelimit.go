package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// rateLimiter applies a token bucket per key, with an idle sweeper so the map cannot
// grow without bound.
type rateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	limit    rate.Limit
	burst    int
	idleTTL  time.Duration
	stopOnce sync.Once
	stop     chan struct{}
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newRateLimiter(perMinute float64, burst int) *rateLimiter {
	rl := &rateLimiter{
		buckets: make(map[string]*bucket),
		limit:   rate.Limit(perMinute / 60.0),
		burst:   burst,
		idleTTL: 15 * time.Minute,
		stop:    make(chan struct{}),
	}
	go rl.sweep()
	return rl
}

// allow reports whether the key may proceed and consumes a token if so.
func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(rl.limit, rl.burst)}
		rl.buckets[key] = b
	}
	b.lastSeen = time.Now()
	return b.limiter.Allow()
}

// reset drops a key's bucket, used after a successful login so a legitimate user is not
// penalised for earlier failures.
func (rl *rateLimiter) reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.buckets, key)
}

func (rl *rateLimiter) sweep() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cutoff := time.Now().Add(-rl.idleTTL)
			rl.mu.Lock()
			for key, b := range rl.buckets {
				if b.lastSeen.Before(cutoff) {
					delete(rl.buckets, key)
				}
			}
			rl.mu.Unlock()
		case <-rl.stop:
			return
		}
	}
}

func (rl *rateLimiter) close() {
	rl.stopOnce.Do(func() { close(rl.stop) })
}

// rateLimit rejects requests once a client exceeds the budget.
//
// The key is the client address resolved by realIP, which only trusts forwarding
// headers from configured proxies (ADR-032) — otherwise a client could rotate the
// header and never be limited.
func rateLimit(rl *rateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.allow(clientKey(r)) {
				w.Header().Set("Retry-After", "60")
				writeError(w, r, http.StatusTooManyRequests, "too many requests, please slow down")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
