package health

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	jobsGVR    = schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	pvcsGVR    = schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
	volumesGVR = schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}
)

// BatchDetector finds jobs that gave up.
type BatchDetector struct {
	Namespaces []string
}

func (d *BatchDetector) Name() string { return "batch" }

func (d *BatchDetector) Detect(ctx context.Context, r Reader) ([]Finding, error) {
	var findings []Finding

	for _, namespace := range namespacesOr(d.Namespaces) {
		jobs, err := r.List(ctx, jobsGVR, namespace)
		if err != nil {
			return nil, err
		}
		for i := range jobs {
			if finding, found := failedJob(&jobs[i]); found {
				findings = append(findings, finding)
			}
		}
	}
	return findings, nil
}

func failedJob(job *unstructured.Unstructured) (Finding, bool) {
	for _, condition := range conditions(job) {
		if condition["type"] != "Failed" || condition["status"] != "True" {
			continue
		}
		reason, _ := condition["reason"].(string)
		message, _ := condition["message"].(string)
		since, _ := condition["lastTransitionTime"].(string)

		return Finding{
			Category:  CategoryBatch,
			Severity:  SeverityCritical,
			Kind:      "Job",
			Namespace: job.GetNamespace(),
			Name:      job.GetName(),
			Reason:    orDefault(reason, "Failed"),
			Detail:    orDefault(message, "The job exhausted its retries without succeeding."),
			LastSeen:  since,
			TypeKey:   "batch/jobs",
		}, true
	}
	return Finding{}, false
}

// StorageDetector finds volumes that will not be there when a pod asks for them.
type StorageDetector struct {
	Namespaces []string
}

func (d *StorageDetector) Name() string { return "storage" }

func (d *StorageDetector) Detect(ctx context.Context, r Reader) ([]Finding, error) {
	var findings []Finding

	for _, namespace := range namespacesOr(d.Namespaces) {
		claims, err := r.List(ctx, pvcsGVR, namespace)
		if err != nil {
			return nil, err
		}
		for i := range claims {
			phase := nested(&claims[i], "status", "phase")
			if phase == "Bound" {
				continue
			}
			severity, detail := claimProblem(phase)
			findings = append(findings, Finding{
				Category:  CategoryStorage,
				Severity:  severity,
				Kind:      "PersistentVolumeClaim",
				Namespace: claims[i].GetNamespace(),
				Name:      claims[i].GetName(),
				Reason:    orDefault(phase, "Unknown"),
				Detail:    detail,
				TypeKey:   "persistentvolumeclaims",
			})
		}
	}

	volumes, err := r.List(ctx, volumesGVR, "")
	if err != nil {
		return nil, err
	}
	for i := range volumes {
		if nested(&volumes[i], "status", "phase") != "Failed" {
			continue
		}
		findings = append(findings, Finding{
			Category: CategoryStorage,
			Severity: SeverityCritical,
			Kind:     "PersistentVolume",
			Name:     volumes[i].GetName(),
			Reason:   "Failed",
			Detail:   orDefault(nested(&volumes[i], "status", "message"), "The volume failed its automatic reclamation."),
			TypeKey:  "persistentvolumes",
		})
	}
	return findings, nil
}

func claimProblem(phase string) (severity, detail string) {
	switch phase {
	case "Pending":
		return SeverityWarning, "No volume satisfies this claim yet. Check the storage class and whether a provisioner is running."
	case "Lost":
		return SeverityCritical, "The bound volume no longer exists; any pod using this claim will not start."
	}
	return SeverityWarning, fmt.Sprintf("The claim is %s rather than Bound.", orDefault(phase, "in an unknown phase"))
}

func namespacesOr(namespaces []string) []string {
	if len(namespaces) == 0 {
		return []string{""}
	}
	return namespaces
}
