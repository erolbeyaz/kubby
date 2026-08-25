package promql

import (
	"strings"
	"testing"
	"time"
)

// Every duration query has the same two traps, and both were live at one point.
//
// The first is `timestamp(x > 0)`, which reports the sample's own evaluation time and so
// always answers "just now" no matter how long something has been broken. The second is
// operator precedence: `A or B and C` parses as `A or (B and C)`, which lets every
// healthy object through the filter that was meant to exclude them.
func TestDurationQueriesMeasureFromTheLastHealthySample(t *testing.T) {
	for name, query := range map[string]string{
		"unavailable": queryUnavailableFor,
		"nodes":       queryNodeConditions,
	} {
		if strings.Contains(query, "timestamp(kube_deployment_status_replicas_unavailable > 0)") ||
			strings.Contains(query, `status="true"} > 0)`) {
			t.Errorf("%s measures from timestamp(broken), which is always now", name)
		}
		if !strings.Contains(query, "last_over_time") {
			t.Errorf("%s does not look back for the last healthy sample", name)
		}
		if !strings.Contains(query, "and on") {
			t.Errorf("%s does not restrict itself to what is currently broken", name)
		}
	}
}

// A deployment whose very first rollout failed has no earlier healthy sample to measure
// from. Without a fallback the most broken case is the one that never appears.
func TestUnavailableFallsBackToCreationTime(t *testing.T) {
	if !strings.Contains(queryUnavailableFor, "kube_deployment_created") {
		t.Fatal("a deployment that was never healthy would be invisible")
	}
	if !strings.Contains(queryNodeConditions, "kube_node_created") {
		t.Fatal("a node condition that was always true would be invisible")
	}
}

// `A or B and C` binds as `A or (B and C)`. Both branches have to sit inside the
// parentheses the filter applies to, or the fallback smuggles every healthy object in.
func TestTheCurrentlyBrokenFilterAppliesToBothBranches(t *testing.T) {
	for name, query := range map[string]string{
		"unavailable": queryUnavailableFor,
		"nodes":       queryNodeConditions,
	} {
		lookback := strings.Index(query, "last_over_time")
		fallback := strings.LastIndex(query, "_created")
		filter := strings.Index(query, "and on")

		if lookback < 0 || fallback < 0 || filter < 0 {
			t.Fatalf("%s no longer has the shape this test checks", name)
		}
		// Both branches have to come before the filter, which is what makes the filter
		// cover the fallback rather than only the lookback.
		if lookback >= fallback || fallback >= filter {
			t.Errorf("%s: the filter does not close over both branches", name)
		}
	}
}

func TestRestartWindowIsWrittenTheWayPromQLWantsIt(t *testing.T) {
	cases := map[time.Duration]string{
		15 * time.Minute: "15m",
		time.Hour:        "1h",
		6 * time.Hour:    "6h",
		24 * time.Hour:   "24h",
		90 * time.Minute: "90m",
	}

	for window, want := range cases {
		if got := promDuration(window); got != want {
			t.Errorf("%s became %q, want %q", window, got, want)
		}
		if !strings.Contains(queryRestartsIn(window), "["+want+"]") {
			t.Errorf("the restart query does not carry the %s window", want)
		}
	}
}

// A sparkline is a few hundred pixels wide. Asking Prometheus for a point per second over
// a day costs it real work and shows the reader nothing extra.
func TestStepKeepsSeriesToASensibleLength(t *testing.T) {
	for _, window := range []time.Duration{15 * time.Minute, time.Hour, 6 * time.Hour, 24 * time.Hour} {
		step := stepFor(window)
		if step < 15*time.Second {
			t.Errorf("%s: step %s is finer than Prometheus scrapes", window, step)
		}
		if points := int(window / step); points > 200 {
			t.Errorf("%s: %d points is more than a sparkline can show", window, points)
		}
	}
}
