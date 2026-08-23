package cluster

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func object(labels, annotations map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": "payments-api", "namespace": "payments"},
	}}
	if labels != nil {
		obj.SetLabels(labels)
	}
	if annotations != nil {
		obj.SetAnnotations(annotations)
	}
	return obj
}

// Editing a GitOps-managed object is not forbidden, but doing it without knowing is how a
// change quietly disappears and nobody understands why (ADR-028).
func TestOwnerOfRecognisesArgoCD(t *testing.T) {
	owner := OwnerOf(object(map[string]string{"argocd.argoproj.io/instance": "payments"}, nil))

	if owner == nil {
		t.Fatal("an Argo-labelled object reported no owner")
	}
	if owner.Controller != "argocd" || owner.Instance != "payments" {
		t.Fatalf("owner = %+v", owner)
	}
	if !owner.SelfHeal {
		t.Error("self-heal should be assumed on unless the object opts out")
	}
}

func TestOwnerOfReadsTheTrackingAnnotation(t *testing.T) {
	owner := OwnerOf(object(nil, map[string]string{
		"argocd.argoproj.io/tracking-id": "payments:apps/Deployment:payments/payments-api",
	}))

	if owner == nil || owner.Controller != "argocd" {
		t.Fatalf("owner = %+v", owner)
	}
}

// An object that opts out of pruning will not be reverted, so warning about it would be
// a warning the user learns to ignore.
func TestOwnerOfHonoursOptOut(t *testing.T) {
	owner := OwnerOf(object(
		map[string]string{"argocd.argoproj.io/instance": "payments"},
		map[string]string{"argocd.argoproj.io/sync-options": "Prune=false"},
	))

	if owner == nil {
		t.Fatal("the object is still owned; only self-heal changes")
	}
	if owner.SelfHeal {
		t.Error("an object that opts out of pruning must not be reported as self-healing")
	}
}

func TestOwnerOfRecognisesFlux(t *testing.T) {
	owner := OwnerOf(object(map[string]string{
		"kustomize.toolkit.fluxcd.io/name":      "apps",
		"kustomize.toolkit.fluxcd.io/namespace": "flux-system",
	}, nil))

	if owner == nil || owner.Controller != "flux" || owner.Instance != "flux-system/apps" {
		t.Fatalf("owner = %+v", owner)
	}
}

func TestOwnerOfReportsNothingForAPlainObject(t *testing.T) {
	if owner := OwnerOf(object(map[string]string{"app": "payments"}, nil)); owner != nil {
		t.Fatalf("owner = %+v, want none", owner)
	}
}
