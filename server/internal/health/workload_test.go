package health

import (
	"context"
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/erolbeyaz/kubby/internal/k8s"
)

type fakeReader struct {
	objects map[schema.GroupVersionResource][]unstructured.Unstructured
	err     error
}

func (f fakeReader) List(_ context.Context, gvr schema.GroupVersionResource, _ string) ([]unstructured.Unstructured, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.objects[gvr], nil
}

func podWith(name string, status map[string]any) unstructured.Unstructured {
	return unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": name, "namespace": "payments"},
		"status":     status,
	}}
}

func detect(t *testing.T, pods ...unstructured.Unstructured) []Finding {
	t.Helper()

	d := &WorkloadDetector{Containers: k8s.NewClassifier(nil)}
	findings, err := d.Detect(context.Background(), fakeReader{
		objects: map[schema.GroupVersionResource][]unstructured.Unstructured{podsGVR: pods},
	})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	return findings
}

func TestDetectsCrashLoop(t *testing.T) {
	findings := detect(t, podWith("api-1", map[string]any{
		"phase": "Running",
		"containerStatuses": []any{map[string]any{
			"name":  "api",
			"state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
		}},
	}))

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(findings), findings)
	}
	got := findings[0]
	if got.Reason != "CrashLoopBackOff" || got.Severity != SeverityCritical || got.Container != "api" {
		t.Fatalf("finding = %+v", got)
	}
	if got.Category != CategoryWorkload {
		t.Fatalf("category = %q, want workload", got.Category)
	}
}

// ADR-030 §4: a mesh sidecar restarting is not an application outage. It is reported,
// but not in the category the user scans for broken workloads.
func TestSidecarCrashLoopIsCategorisedApart(t *testing.T) {
	findings := detect(t, podWith("api-1", map[string]any{
		"phase": "Running",
		"containerStatuses": []any{map[string]any{
			"name":  "istio-proxy",
			"state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
		}},
	}))

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	if findings[0].Category != CategorySidecar {
		t.Fatalf("category = %q, want sidecar", findings[0].Category)
	}
	if findings[0].Severity != SeverityWarning {
		t.Fatalf("severity = %q, want warning", findings[0].Severity)
	}
}

// Every rollout puts pods through ContainerCreating. Reporting it would fill the panel
// with noise and teach the user to ignore it.
func TestStartingContainersAreNotProblems(t *testing.T) {
	findings := detect(t, podWith("api-1", map[string]any{
		"phase": "Pending",
		"containerStatuses": []any{map[string]any{
			"name":  "api",
			"state": map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}},
		}},
	}))

	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}

func TestDetectsUnschedulable(t *testing.T) {
	findings := detect(t, podWith("api-1", map[string]any{
		"phase": "Pending",
		"conditions": []any{map[string]any{
			"type":               "PodScheduled",
			"status":             "False",
			"reason":             "Unschedulable",
			"message":            "0/3 nodes are available: insufficient cpu.",
			"lastTransitionTime": "2026-08-23T09:00:00Z",
		}},
	}))

	if len(findings) != 1 || findings[0].Reason != "Unschedulable" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Detail != "0/3 nodes are available: insufficient cpu." {
		t.Fatalf("detail = %q, want the scheduler's own message", findings[0].Detail)
	}
}

// A container running now but killed for memory a minute ago is exactly what a snapshot
// of current state hides.
func TestDetectsPastOOMKill(t *testing.T) {
	findings := detect(t, podWith("api-1", map[string]any{
		"phase": "Running",
		"containerStatuses": []any{map[string]any{
			"name":         "api",
			"restartCount": int64(3),
			"state":        map[string]any{"running": map[string]any{}},
			"lastState": map[string]any{"terminated": map[string]any{
				"reason":     "OOMKilled",
				"finishedAt": "2026-08-23T09:10:00Z",
			}},
		}},
	}))

	if len(findings) != 1 || findings[0].Reason != "OOMKilled" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Count != 3 {
		t.Fatalf("count = %d, want the restart count", findings[0].Count)
	}
}

// A pod that says Running and serves nothing is the failure that looks healthy.
func TestDetectsRunningButNotReady(t *testing.T) {
	findings := detect(t, podWith("api-1", map[string]any{
		"phase": "Running",
		"conditions": []any{map[string]any{
			"type":               "Ready",
			"status":             "False",
			"message":            "containers with unready status: [api]",
			"lastTransitionTime": "2026-08-23T09:00:00Z",
		}},
	}))

	if len(findings) != 1 || findings[0].Reason != "NotReady" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestHealthyPodProducesNothing(t *testing.T) {
	findings := detect(t, podWith("api-1", map[string]any{
		"phase":      "Running",
		"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
		"containerStatuses": []any{map[string]any{
			"name":  "api",
			"state": map[string]any{"running": map[string]any{}},
		}},
	}))

	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}

func TestDetectorReportsListFailure(t *testing.T) {
	d := &WorkloadDetector{}
	_, err := d.Detect(context.Background(), fakeReader{err: errors.New("forbidden")})

	if err == nil {
		t.Fatal("a failed list must be reported, not silently treated as a healthy cluster")
	}
}
