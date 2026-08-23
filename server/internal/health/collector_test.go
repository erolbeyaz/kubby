package health

import (
	"context"
	"errors"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type stubDetector struct {
	name     string
	findings []Finding
	err      error
	delay    time.Duration
}

func (s stubDetector) Name() string { return s.name }

func (s stubDetector) Detect(ctx context.Context, _ Reader) ([]Finding, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.findings, s.err
}

// A user denied access to nodes should still see their crash-looping pods. Returning
// nothing because one list was forbidden is the difference between a useful panel and a
// useless one.
func TestOneFailedDetectorDoesNotHideTheRest(t *testing.T) {
	c := &Collector{Detectors: []Detector{
		stubDetector{name: "node", err: errors.New("nodes is forbidden")},
		stubDetector{name: "workload", findings: []Finding{
			{Severity: SeverityCritical, Kind: "Pod", Name: "api-1", Reason: "CrashLoopBackOff"},
		}},
	}}

	report := c.Collect(context.Background())

	if len(report.Findings) != 1 {
		t.Fatalf("findings = %+v, want the workload finding", report.Findings)
	}
	if report.Failed["node"] == "" {
		t.Fatal("the failed detector must be reported, not swallowed")
	}
	if report.Counts[SeverityCritical] != 1 {
		t.Fatalf("counts = %+v", report.Counts)
	}
}

// One slow list must not hold the panel.
func TestSlowDetectorIsBounded(t *testing.T) {
	c := &Collector{
		Timeout: 20 * time.Millisecond,
		Detectors: []Detector{
			stubDetector{name: "slow", delay: time.Second},
			stubDetector{name: "fast", findings: []Finding{{Severity: SeverityWarning, Name: "a"}}},
		},
	}

	start := time.Now()
	report := c.Collect(context.Background())

	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("collect took %s; the timeout did not bound the slow detector", elapsed)
	}
	if len(report.Findings) != 1 || report.Failed["slow"] == "" {
		t.Fatalf("report = %+v", report)
	}
}

// Severity first, then a stable order, so a refresh does not move the row the user is
// reading.
func TestFindingsAreSortedWorstFirstAndStably(t *testing.T) {
	c := &Collector{Detectors: []Detector{stubDetector{name: "all", findings: []Finding{
		{Severity: SeverityInfo, Kind: "Node", Name: "node-b"},
		{Severity: SeverityCritical, Kind: "Pod", Namespace: "payments", Name: "z"},
		{Severity: SeverityWarning, Kind: "Pod", Namespace: "payments", Name: "a"},
		{Severity: SeverityCritical, Kind: "Pod", Namespace: "payments", Name: "a"},
	}}}}

	report := c.Collect(context.Background())

	got := make([]string, 0, len(report.Findings))
	for _, finding := range report.Findings {
		got = append(got, finding.Severity+":"+finding.Name)
	}
	want := []string{"critical:a", "critical:z", "warning:a", "info:node-b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestEventsAreGroupedByObjectAndReason(t *testing.T) {
	now := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	event := func(reason, name, at string, count int64) unstructured.Unstructured {
		return unstructured.Unstructured{Object: map[string]any{
			"type":          "Warning",
			"reason":        reason,
			"message":       "Back-off restarting failed container",
			"count":         count,
			"lastTimestamp": at,
			"involvedObject": map[string]any{
				"kind": "Pod", "name": name, "namespace": "payments",
			},
		}}
	}

	d := &EventDetector{Now: func() time.Time { return now }}
	findings, err := d.Detect(context.Background(), fakeReader{
		objects: map[schema.GroupVersionResource][]unstructured.Unstructured{eventsGVR: {
			event("BackOff", "api-1", "2026-08-23T09:50:00Z", 40),
			event("BackOff", "api-1", "2026-08-23T09:55:00Z", 2),
			event("Unhealthy", "api-1", "2026-08-23T09:56:00Z", 1),
			// Yesterday's noise is outside the window.
			event("BackOff", "old-1", "2026-08-22T09:00:00Z", 5),
		}},
	})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}

	if len(findings) != 2 {
		t.Fatalf("findings = %+v, want one row per object and reason", findings)
	}
	if findings[0].Reason != "BackOff" || findings[0].Count != 42 {
		t.Fatalf("first finding = %+v, want BackOff counted 42", findings[0])
	}
	if findings[0].LastSeen != "2026-08-23T09:55:00Z" {
		t.Fatalf("lastSeen = %q, want the most recent occurrence", findings[0].LastSeen)
	}
}

func TestNonWarningEventsAreIgnored(t *testing.T) {
	d := &EventDetector{Now: func() time.Time { return time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC) }}
	findings, err := d.Detect(context.Background(), fakeReader{
		objects: map[schema.GroupVersionResource][]unstructured.Unstructured{eventsGVR: {{Object: map[string]any{
			"type":          "Normal",
			"reason":        "Scheduled",
			"lastTimestamp": "2026-08-23T09:59:00Z",
			"involvedObject": map[string]any{
				"kind": "Pod", "name": "api-1", "namespace": "payments",
			},
		}}}},
	})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}
