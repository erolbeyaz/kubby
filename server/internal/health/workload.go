package health

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/erolbeyaz/kubby/internal/k8s"
)

var podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

// WorkloadDetector finds pods that are not running when they should be.
type WorkloadDetector struct {
	Containers *k8s.Classifier
	Namespaces []string
}

func (d *WorkloadDetector) Name() string { return "workload" }

func (d *WorkloadDetector) Detect(ctx context.Context, r Reader) ([]Finding, error) {
	var findings []Finding

	for _, namespace := range namespacesOr(d.Namespaces) {
		pods, err := r.List(ctx, podsGVR, namespace)
		if err != nil {
			return nil, err
		}
		for i := range pods {
			findings = append(findings, d.inspect(&pods[i])...)
		}
	}
	return findings, nil
}

func (d *WorkloadDetector) inspect(pod *unstructured.Unstructured) []Finding {
	base := Finding{
		Kind:      "Pod",
		Namespace: pod.GetNamespace(),
		Name:      pod.GetName(),
		TypeKey:   "pods",
		Category:  CategoryWorkload,
	}

	// One reading of what is wrong with a pod, shared with the list's warning mark
	// (internal/k8s). Two copies would drift, and then a row and this panel would
	// describe the same pod differently.
	trouble := k8s.PodTrouble(pod)
	if trouble == nil || trouble.Transient {
		return nil
	}

	base.Reason = trouble.Reason
	base.Detail = trouble.Detail
	base.Container = trouble.Container
	base.Severity = severityOf(trouble.Severity)

	// A sidecar in trouble is not ignored, but it is not an application fault either;
	// mixing the two is how a mesh restart reads as an outage (ADR-030 §4).
	if trouble.Container != "" && d.classifier().IsSidecar(trouble.Container) {
		base.Category = CategorySidecar
		if base.Severity == SeverityCritical {
			base.Severity = SeverityWarning
		}
	}

	if count := restartsOf(pod, trouble.Container); count > 0 {
		base.Count = count
	}
	return []Finding{base}
}

func severityOf(severity string) string {
	if severity == k8s.SeverityWarning {
		return SeverityWarning
	}
	return SeverityCritical
}

func restartsOf(pod *unstructured.Unstructured, container string) int {
	for _, status := range containerStatuses(pod, "containerStatuses") {
		if name, _ := status["name"].(string); name == container {
			return intOf(status["restartCount"])
		}
	}
	return 0
}

func (d *WorkloadDetector) classifier() *k8s.Classifier {
	if d.Containers == nil {
		d.Containers = k8s.NewClassifier(nil)
	}
	return d.Containers
}
