package health

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestDetectsFailedJob(t *testing.T) {
	job := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "nightly", "namespace": "payments"},
		"status": map[string]any{"conditions": []any{map[string]any{
			"type": "Failed", "status": "True",
			"reason":  "BackoffLimitExceeded",
			"message": "Job has reached the specified backoff limit",
		}}},
	}}

	findings, err := (&BatchDetector{}).Detect(context.Background(), fakeReader{
		objects: map[schema.GroupVersionResource][]unstructured.Unstructured{jobsGVR: {job}},
	})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(findings) != 1 || findings[0].Reason != "BackoffLimitExceeded" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDetectsUnboundAndLostVolumes(t *testing.T) {
	claim := func(name, phase string) unstructured.Unstructured {
		return unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{"name": name, "namespace": "payments"},
			"status":   map[string]any{"phase": phase},
		}}
	}

	findings, err := (&StorageDetector{}).Detect(context.Background(), fakeReader{
		objects: map[schema.GroupVersionResource][]unstructured.Unstructured{
			pvcsGVR: {claim("data-0", "Bound"), claim("data-1", "Pending"), claim("data-2", "Lost")},
		},
	})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}

	if len(findings) != 2 {
		t.Fatalf("findings = %+v, want the pending and lost claims only", findings)
	}
	// A lost volume stops a pod outright; a pending one may still be provisioning.
	bySeverity := map[string]string{}
	for _, finding := range findings {
		bySeverity[finding.Reason] = finding.Severity
	}
	if bySeverity["Lost"] != SeverityCritical || bySeverity["Pending"] != SeverityWarning {
		t.Fatalf("severities = %+v", bySeverity)
	}
}
