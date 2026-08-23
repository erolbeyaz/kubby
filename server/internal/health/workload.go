package health

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/erolbeyaz/kubby/internal/k8s"
)

var podsGVR = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

// waitingIsNormal are waiting reasons that mean "starting", not "broken". Reporting them
// would fill the panel during every rollout and teach the user to ignore it.
var waitingIsNormal = map[string]bool{
	"ContainerCreating": true,
	"PodInitializing":   true,
}

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
	}

	if phase := nested(pod, "status", "phase"); phase == "Pending" {
		if finding, found := d.pending(pod, base); found {
			return []Finding{finding}
		}
	}

	if reason := nested(pod, "status", "reason"); reason == "Evicted" {
		base.Category = CategoryWorkload
		base.Severity = SeverityWarning
		base.Reason = "Evicted"
		base.Detail = orDefault(nested(pod, "status", "message"), "The node evicted this pod.")
		return []Finding{base}
	}

	findings := d.containers(pod, base)
	if len(findings) > 0 {
		return findings
	}
	return d.unready(pod, base)
}

// pending separates "cannot be scheduled" from "is being scheduled". Only the first is a
// problem; the second resolves itself in seconds.
func (d *WorkloadDetector) pending(pod *unstructured.Unstructured, base Finding) (Finding, bool) {
	for _, condition := range conditions(pod) {
		if condition["type"] != "PodScheduled" || condition["status"] == "True" {
			continue
		}
		reason, _ := condition["reason"].(string)
		if reason != "Unschedulable" {
			continue
		}
		message, _ := condition["message"].(string)

		base.Category = CategoryWorkload
		base.Severity = SeverityCritical
		base.Reason = "Unschedulable"
		base.Detail = orDefault(message, "No node can accept this pod.")
		base.LastSeen, _ = condition["lastTransitionTime"].(string)
		return base, true
	}
	return Finding{}, false
}

func (d *WorkloadDetector) containers(pod *unstructured.Unstructured, base Finding) []Finding {
	var findings []Finding

	for _, status := range containerStatuses(pod, "containerStatuses") {
		name, _ := status["name"].(string)
		finding, found := d.containerProblem(status, base)
		if !found {
			continue
		}
		finding.Container = name

		// A sidecar in CrashLoopBackOff is not ignored, but it is not an application
		// fault either; mixing the two is how a mesh restart reads as an outage.
		if d.classifier().IsSidecar(name) {
			finding.Category = CategorySidecar
			if finding.Severity == SeverityCritical {
				finding.Severity = SeverityWarning
			}
		}
		findings = append(findings, finding)
	}

	for _, status := range containerStatuses(pod, "initContainerStatuses") {
		name, _ := status["name"].(string)
		finding, found := d.containerProblem(status, base)
		if !found {
			continue
		}
		finding.Container = name
		finding.Detail = "Init container: " + finding.Detail
		findings = append(findings, finding)
	}
	return findings
}

func (d *WorkloadDetector) containerProblem(status map[string]any, base Finding) (Finding, bool) {
	base.Category = CategoryWorkload
	state, _ := status["state"].(map[string]any)

	if waiting, ok := state["waiting"].(map[string]any); ok {
		reason, _ := waiting["reason"].(string)
		if reason == "" || waitingIsNormal[reason] {
			return Finding{}, false
		}
		message, _ := waiting["message"].(string)

		base.Severity = SeverityCritical
		base.Reason = reason
		base.Detail = orDefault(message, waitingDetail(reason))
		return base, true
	}

	// A container that is running now but was killed for memory is the case a snapshot
	// of current state hides, and the one most worth surfacing.
	if last, ok := status["lastState"].(map[string]any); ok {
		if terminated, ok := last["terminated"].(map[string]any); ok {
			reason, _ := terminated["reason"].(string)
			if reason != "OOMKilled" {
				return Finding{}, false
			}
			finished, _ := terminated["finishedAt"].(string)

			base.Severity = SeverityCritical
			base.Reason = "OOMKilled"
			base.Detail = "The kernel killed this container for exceeding its memory limit."
			base.LastSeen = finished
			base.Count = intOf(status["restartCount"])
			return base, true
		}
	}
	return Finding{}, false
}

// unready catches the pod that says Running and serves nothing.
func (d *WorkloadDetector) unready(pod *unstructured.Unstructured, base Finding) []Finding {
	if nested(pod, "status", "phase") != "Running" {
		return nil
	}
	for _, condition := range conditions(pod) {
		if condition["type"] != "Ready" || condition["status"] == "True" {
			continue
		}
		message, _ := condition["message"].(string)
		since, _ := condition["lastTransitionTime"].(string)

		base.Category = CategoryWorkload
		base.Severity = SeverityWarning
		base.Reason = "NotReady"
		base.Detail = orDefault(message, "Running, but failing its readiness probe, so it receives no traffic.")
		base.LastSeen = since
		return []Finding{base}
	}
	return nil
}

func (d *WorkloadDetector) classifier() *k8s.Classifier {
	if d.Containers == nil {
		d.Containers = k8s.NewClassifier(nil)
	}
	return d.Containers
}

func waitingDetail(reason string) string {
	switch {
	case strings.HasPrefix(reason, "ErrImagePull"), reason == "ImagePullBackOff", reason == "InvalidImageName":
		return "The image could not be pulled. Check the name, the tag and the pull secret."
	case reason == "CrashLoopBackOff":
		return "The container keeps exiting and Kubernetes is backing off before trying again."
	case reason == "CreateContainerConfigError":
		return "A referenced ConfigMap, Secret or key is missing."
	}
	return fmt.Sprintf("The container is waiting: %s.", reason)
}
