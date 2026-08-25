package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// ShipperStatsFunc reports the audit shipper's counters. It is a function rather than a
// struct so this package does not import the audit package, which imports the store.
type ShipperStatsFunc func() (sink string, queued int, sent, failed, dropped, retries uint64, running bool)

// auditCollector publishes the audit shipper's counters.
//
// Collected on scrape rather than mirrored into counters as events happen: the shipper
// already keeps these numbers, and a second copy updated from another goroutine is one
// more thing that can disagree with the first.
//
// The one that matters is dropped. A rising drop count means audit records are not
// reaching the SIEM, which is a compliance failure that is otherwise completely silent —
// nothing else in the system notices, because Kubby carries on working perfectly.
type auditCollector struct {
	stats ShipperStatsFunc

	running *prometheus.Desc
	queued  *prometheus.Desc
	sent    *prometheus.Desc
	failed  *prometheus.Desc
	dropped *prometheus.Desc
	retries *prometheus.Desc
}

// RegisterAuditShipper publishes the shipper's counters through this registry.
func (r *Registry) RegisterAuditShipper(stats ShipperStatsFunc) error {
	return r.Register(&auditCollector{
		stats: stats,
		running: prometheus.NewDesc("kubby_audit_shipping_up",
			"1 when audit shipping is configured and running.", nil, nil),
		queued: prometheus.NewDesc("kubby_audit_shipping_queued",
			"Audit events waiting to be sent. A number that only climbs means the sink cannot keep up.",
			[]string{"sink"}, nil),
		sent: prometheus.NewDesc("kubby_audit_shipping_sent_total",
			"Audit events delivered to the sink.", []string{"sink"}, nil),
		failed: prometheus.NewDesc("kubby_audit_shipping_failures_total",
			"Batches the sink refused. Retried, so this rising alone is not yet loss.",
			[]string{"sink"}, nil),
		dropped: prometheus.NewDesc("kubby_audit_shipping_dropped_total",
			"Audit events that never reached the sink. Any increase is a gap in the trail.",
			[]string{"sink"}, nil),
		retries: prometheus.NewDesc("kubby_audit_shipping_retries_total",
			"Retry attempts made.", []string{"sink"}, nil),
	})
}

func (c *auditCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.running
	ch <- c.queued
	ch <- c.sent
	ch <- c.failed
	ch <- c.dropped
	ch <- c.retries
}

func (c *auditCollector) Collect(ch chan<- prometheus.Metric) {
	sink, queued, sent, failed, dropped, retries, running := c.stats()

	up := 0.0
	if running {
		up = 1
	}
	ch <- prometheus.MustNewConstMetric(c.running, prometheus.GaugeValue, up)

	if !running {
		// No per-sink series while nothing is configured: emitting them with an empty
		// label would create a series that means "not shipping", which reads as zero
		// drops rather than as no shipping at all.
		return
	}

	ch <- prometheus.MustNewConstMetric(c.queued, prometheus.GaugeValue, float64(queued), sink)
	ch <- prometheus.MustNewConstMetric(c.sent, prometheus.CounterValue, float64(sent), sink)
	ch <- prometheus.MustNewConstMetric(c.failed, prometheus.CounterValue, float64(failed), sink)
	ch <- prometheus.MustNewConstMetric(c.dropped, prometheus.CounterValue, float64(dropped), sink)
	ch <- prometheus.MustNewConstMetric(c.retries, prometheus.CounterValue, float64(retries), sink)
}
