// Package metrics exposes what Kubby itself is doing.
//
// About Kubby, not about the clusters it watches: those have their own Prometheus and
// duplicating them here would produce a second set of numbers that disagrees with the one
// the team already trusts. What belongs here is what nothing else can see — how long
// Kubby's own handlers take, how much its caches hold, and whether the audit trail is
// actually reaching the SIEM.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Registry holds Kubby's collectors.
//
// Its own registry rather than the default one: the default is global, anything a
// dependency imports can add to it, and a library quietly registering its own metrics is
// how an endpoint starts exporting something nobody chose to publish.
type Registry struct {
	prom *prometheus.Registry

	HTTPRequests *prometheus.CounterVec
	HTTPDuration *prometheus.HistogramVec
	HTTPInFlight prometheus.Gauge

	ClusterCalls    *prometheus.CounterVec
	ClusterFailures *prometheus.CounterVec

	SessionsActive prometheus.Gauge
	LoginAttempts  *prometheus.CounterVec

	StreamsOpen *prometheus.GaugeVec
}

func New() *Registry {
	reg := prometheus.NewRegistry()

	r := &Registry{
		prom: reg,

		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubby_http_requests_total",
			Help: "HTTP requests handled, by route pattern rather than by path.",
		}, []string{"method", "route", "status"}),

		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "kubby_http_request_duration_seconds",
			Help: "How long handlers take.",
			// Tuned for this application rather than left at the default: a list of pods
			// is expected in tens of milliseconds, and the interesting question is which
			// requests cross a second.
			Buckets: []float64{0.005, 0.025, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"method", "route"}),

		HTTPInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kubby_http_requests_in_flight",
			Help: "Requests being handled right now.",
		}),

		ClusterCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubby_cluster_api_calls_total",
			Help: "Calls Kubby made to a cluster's API server.",
		}, []string{"cluster", "verb"}),

		ClusterFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubby_cluster_api_failures_total",
			Help: "Calls to a cluster's API server that failed.",
		}, []string{"cluster", "reason"}),

		SessionsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "kubby_sessions_active",
			Help: "Signed-in sessions that have not expired.",
		}),

		LoginAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kubby_login_attempts_total",
			Help: "Sign-in attempts, by outcome. A rising failure count is the first sign of a password spray.",
		}, []string{"result"}),

		StreamsOpen: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "kubby_streams_open",
			Help: "Long-lived connections open right now: logs, shells, port forwards, resource streams.",
		}, []string{"kind"}),
	}

	reg.MustRegister(
		r.HTTPRequests, r.HTTPDuration, r.HTTPInFlight,
		r.ClusterCalls, r.ClusterFailures,
		r.SessionsActive, r.LoginAttempts, r.StreamsOpen,
		// The process and Go runtime collectors: memory, goroutines, file descriptors.
		// A tool that holds informer caches for a fleet of clusters is one whose memory
		// is worth watching.
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)

	return r
}

// Gatherer is what the handler serves.
func (r *Registry) Gatherer() prometheus.Gatherer { return r.prom }

// Register adds a collector supplied by another package, so a component can publish its
// own numbers without this package importing it.
func (r *Registry) Register(c prometheus.Collector) error { return r.prom.Register(c) }
