package promql

import (
	"context"
	"os"
	"testing"
	"time"
)

// The summary's whole claim is that it can tell a real zero from a metric nobody is
// collecting. That can only be checked against a real Prometheus:
//
//	KUBBY_PROBE_URL=http://prometheus:9090 go test -run TestSummary ./internal/promql
func TestSummaryTellsZeroFromUnknown(t *testing.T) {
	url := os.Getenv("KUBBY_PROBE_URL")
	if url == "" {
		t.Skip("KUBBY_PROBE_URL is not set")
	}
	client, err := New(Config{URL: url, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s := readSummary(ctx, client)
	t.Logf("status=%s reasons=%v", s.Status, s.Reasons)
	for _, row := range []struct {
		name string
		r    Reading
	}{
		{"nodesReady", s.NodesReady}, {"nodesTotal", s.NodesTotal},
		{"nodesNotReady", s.NodesNotReady}, {"nodesUnderPressure", s.NodesUnderPressure},
		{"podsReady", s.PodsReady}, {"podsTotal", s.PodsTotal},
		{"podsPending", s.PodsPending}, {"longestPending", s.LongestPendingSeconds},
		{"restarts1h", s.Restarts1h}, {"oomKilled", s.OOMKilled},
		{"evicted", s.Evicted}, {"unavailable", s.Unavailable},
		{"alertsCritical", s.AlertsCritical}, {"alertsWarning", s.AlertsWarning},
		{"apiErrorRate", s.APIErrorRate},
		{"targetsDown", s.TargetsDown}, {"targetsTotal", s.TargetsTotal},
	} {
		if row.r.Known {
			t.Logf("  %-20s %.2f", row.name, row.r.Value)
		} else {
			t.Logf("  %-20s N/A", row.name)
		}
	}

	if s.Status == "" {
		t.Fatal("no status was derived")
	}
	// A cluster this screen cannot read must never be reported as healthy.
	if s.Status == StatusHealthy && !s.NodesReady.Known && !s.PodsReady.Known {
		t.Error("reported Healthy without being able to read a single cluster metric")
	}
}
