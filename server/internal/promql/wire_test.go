package promql

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

// The dashboard validates every field it receives and discards the whole response if one
// name is wrong. That makes the JSON names part of the contract rather than an accident
// of how the structs happen to be spelled, and worth a test that reads them back.
//
// This is not hypothetical: Point shipped untagged, so it serialised as "At" and "Value"
// while the client validated for "at" and "value". The response failed to parse, the
// panel fell back to zeros, and an unreachable cluster and an empty one looked identical.
func TestTheWireNamesAreTheOnesTheClientValidates(t *testing.T) {
	health := ClusterHealth{
		CPU:    []Point{{At: time.Unix(0, 0).UTC(), Value: 27.5}},
		Memory: []Point{{At: time.Unix(0, 0).UTC(), Value: 53}},
		Disk:   []NodeGauge{{Node: "n1", Percent: 10}},
	}

	body, err := json.Marshal(health)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"cpu", "memory", "disk", "pods", "nodes", "restarts24h"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("the response has no %q; the client validates for it", key)
		}
	}

	points, ok := decoded["cpu"].([]any)
	if !ok || len(points) == 0 {
		t.Fatalf("cpu is not a list of points: %v", decoded["cpu"])
	}
	point, ok := points[0].(map[string]any)
	if !ok {
		t.Fatalf("a point is not an object: %v", points[0])
	}
	for _, key := range []string{"at", "value"} {
		if _, ok := point[key]; !ok {
			t.Errorf("a point has no %q — it has %v", key, keysOf(point))
		}
	}

	// RFC 3339 in UTC (ADR-026): the browser does the converting, the server never does.
	at, _ := point["at"].(string)
	if _, err := time.Parse(time.RFC3339, at); err != nil {
		t.Errorf("a point's time %q is not RFC 3339: %v", at, err)
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// One NaN would fail the encoding of the whole response, and the dashboard would show
// nothing rather than one empty panel.
//
// Prometheus produces them routinely: histogram_quantile over an empty histogram is NaN,
// and a rate divided by zero is Inf. Both were arriving from a real cluster.
func TestNotANumberNeverReachesTheWire(t *testing.T) {
	for _, text := range []string{"NaN", "+Inf", "-Inf"} {
		if _, ok := readValue([]any{float64(0), text}); ok {
			t.Errorf("%q was accepted as a value", text)
		}
	}

	// And a Reading built from one is not a reading at all.
	if r := known(math.NaN()); r.Known {
		t.Error("NaN was recorded as a known value")
	}

	health := ClusterHealth{
		Summary: ClusterSummary{APIErrorRate: known(math.Inf(1))},
		CPU:     []Point{{At: time.Unix(0, 0).UTC(), Value: 1}},
	}
	if _, err := json.Marshal(health); err != nil {
		t.Fatalf("the response does not encode: %v", err)
	}
}
