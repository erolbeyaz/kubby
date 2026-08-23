package k8s

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Trouble is what is wrong with an object, in the object's own words.
type Trouble struct {
	// Reason is the machine word: CrashLoopBackOff, OOMKilled, Unschedulable.
	Reason string
	// Detail is the sentence to show instead of making someone open the object.
	Detail string
	// Container names the container at fault, where one is.
	Container string
	// Severity is "error" or "warning"; empty means nothing is wrong.
	Severity string
	// Transient marks a state that resolves on its own: a pod being created is not
	// settled, but it is not a problem either. A list marks it so the row is not
	// silently unfinished; the health panel skips it, because a panel that fills up
	// during every rollout teaches people to stop reading it.
	Transient bool
}

const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// waitingIsNormal are waiting reasons that mean "starting", not "broken".
var waitingIsNormal = map[string]bool{
	"ContainerCreating": true,
	"PodInitializing":   true,
}

// PodTrouble reads why a pod is not doing its job.
//
// This is the one place that decides what is wrong with a pod: the list's warning mark
// and the health panel both read it, so a row and the panel can never disagree about the
// same pod. "Pending" and "Failed" are not answers — they are the question — and the
// answer is always somewhere in the object already.
func PodTrouble(pod *unstructured.Unstructured) *Trouble {
	if reason := str(pod, "status", "reason"); reason == "Evicted" {
		return &Trouble{
			Reason:   "Evicted",
			Detail:   orElse(str(pod, "status", "message"), "The node evicted this pod."),
			Severity: SeverityWarning,
		}
	}

	phase := str(pod, "status", "phase")

	// The scheduler explains itself in a condition, and its message names the constraint
	// that could not be met — insufficient cpu, an unbound claim, a taint.
	if phase == "Pending" {
		if trouble := schedulingTrouble(pod); trouble != nil {
			return trouble
		}
	}

	if trouble := containerTrouble(pod, "containerStatuses", ""); trouble != nil {
		return trouble
	}
	if trouble := containerTrouble(pod, "initContainerStatuses", "Init container "); trouble != nil {
		return trouble
	}

	switch phase {
	case "Failed":
		return &Trouble{
			Reason:   orElse(str(pod, "status", "reason"), "Failed"),
			Detail:   orElse(str(pod, "status", "message"), "The pod ran and did not succeed."),
			Severity: SeverityError,
		}
	case "Pending":
		return &Trouble{
			Reason:    "Pending",
			Detail:    "Waiting to be scheduled or for its containers to be created.",
			Severity:  SeverityWarning,
			Transient: true,
		}
	case "Running":
		return notReadyTrouble(pod)
	}
	return nil
}

func schedulingTrouble(pod *unstructured.Unstructured) *Trouble {
	for _, condition := range conditions(pod) {
		if condition["type"] != "PodScheduled" || condition["status"] == "True" {
			continue
		}

		reason, _ := condition["reason"].(string)
		message, _ := condition["message"].(string)
		if reason == "" {
			reason = "Unschedulable"
		}

		return &Trouble{
			Reason:   reason,
			Detail:   orElse(message, "No node can accept this pod."),
			Severity: SeverityError,
		}
	}
	return nil
}

func containerTrouble(pod *unstructured.Unstructured, key, prefix string) *Trouble {
	for _, status := range mapsAt(pod, "status", key) {
		name, _ := status["name"].(string)
		state, _ := status["state"].(map[string]any)

		if waiting, ok := state["waiting"].(map[string]any); ok {
			reason, _ := waiting["reason"].(string)
			if reason == "" || waitingIsNormal[reason] {
				continue
			}
			message, _ := waiting["message"].(string)

			return &Trouble{
				Reason:    reason,
				Detail:    prefix + orElse(message, waitingDetail(reason)),
				Container: name,
				Severity:  SeverityError,
			}
		}

		if terminated, ok := state["terminated"].(map[string]any); ok {
			if trouble := terminationTrouble(terminated, name, prefix, false); trouble != nil {
				return trouble
			}
		}

		// A container running now but killed for memory a minute ago is what a snapshot
		// of current state hides, and the thing most worth surfacing.
		if last, ok := status["lastState"].(map[string]any); ok {
			if terminated, ok := last["terminated"].(map[string]any); ok {
				if trouble := terminationTrouble(terminated, name, prefix, true); trouble != nil {
					return trouble
				}
			}
		}
	}
	return nil
}

func terminationTrouble(terminated map[string]any, name, prefix string, past bool) *Trouble {
	reason, _ := terminated["reason"].(string)
	exitCode := intOf(terminated["exitCode"])

	if reason == "Completed" || (reason == "" && exitCode == 0) {
		return nil
	}
	if past && reason != "OOMKilled" {
		// Any other past exit is already told by the restart count; only the memory kill
		// is invisible once the container is up again.
		return nil
	}

	detail := explainExit(reason, exitCode)
	if past {
		detail = "Was " + strings.ToLower(detail[:1]) + detail[1:]
	}

	return &Trouble{
		Reason:    orElse(reason, fmt.Sprintf("Exit %d", exitCode)),
		Detail:    prefix + detail,
		Container: name,
		Severity:  SeverityError,
	}
}

func notReadyTrouble(pod *unstructured.Unstructured) *Trouble {
	for _, condition := range conditions(pod) {
		if condition["type"] != "Ready" || condition["status"] == "True" {
			continue
		}
		message, _ := condition["message"].(string)

		return &Trouble{
			Reason:   "NotReady",
			Detail:   orElse(message, "Running, but failing its readiness probe, so it receives no traffic."),
			Severity: SeverityWarning,
		}
	}
	return nil
}

// explainExit turns a reason and an exit code into a sentence. Reading a failure should
// not require knowing that 137 means the kernel killed the process.
func explainExit(reason string, exitCode int) string {
	switch {
	case reason == "OOMKilled":
		return "Killed for exceeding its memory limit."
	case reason == "Error" && exitCode == 1:
		return "The process exited with code 1. Its last log lines say why."
	case exitCode == 137:
		return "Exit code 137: killed by SIGKILL, most often the memory limit."
	case exitCode == 143:
		return "Exit code 143: terminated by SIGTERM during shutdown."
	case exitCode != 0:
		return fmt.Sprintf("The process exited with code %d.", exitCode)
	}
	return orElse(reason, "The container stopped.")
}

func waitingDetail(reason string) string {
	switch {
	case strings.HasPrefix(reason, "ErrImagePull"), reason == "ImagePullBackOff", reason == "InvalidImageName":
		return "The image could not be pulled. Check the name, the tag and the pull secret."
	case reason == "CrashLoopBackOff":
		return "The container keeps exiting; Kubernetes is backing off before trying again."
	case reason == "CreateContainerConfigError":
		return "A referenced ConfigMap, Secret or key is missing."
	case reason == "CreateContainerError":
		return "The container could not be created. The kubelet's message says what it objected to."
	}
	return fmt.Sprintf("Waiting: %s.", reason)
}

func str(obj *unstructured.Unstructured, path ...string) string {
	value, _, _ := unstructured.NestedString(obj.Object, path...)
	return value
}

func conditions(obj *unstructured.Unstructured) []map[string]any {
	return mapsAt(obj, "status", "conditions")
}

func mapsAt(obj *unstructured.Unstructured, fields ...string) []map[string]any {
	raw, _, _ := unstructured.NestedSlice(obj.Object, fields...)

	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(map[string]any); ok {
			out = append(out, value)
		}
	}
	return out
}

func intOf(value any) int {
	switch typed := value.(type) {
	case int64:
		return int(typed)
	case int:
		return typed
	case float64:
		return int(typed)
	}
	return 0
}

func orElse(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// PodStatus is the status column as kubectl computes it, which is not the pod's phase.
//
// A pod whose container is in CrashLoopBackOff has phase "Running": the pod exists and
// its container was created. Showing the phase says a crash-looping pod is Running,
// which is true of the pod and useless to the reader. kubectl lets the container's own
// state win, and so does this.
func PodStatus(pod *unstructured.Unstructured) string {
	if pod.GetDeletionTimestamp() != nil {
		return "Terminating"
	}

	status := orElse(str(pod, "status", "reason"), str(pod, "status", "phase"))

	// Init containers first: while one is failing, nothing else has started.
	for _, container := range mapsAt(pod, "status", "initContainerStatuses") {
		if reason := stateReason(container); reason != "" {
			return reason
		}
	}
	for _, container := range mapsAt(pod, "status", "containerStatuses") {
		if reason := stateReason(container); reason != "" {
			return reason
		}
	}
	return status
}

// stateReason is a container's own word for what it is doing, when that word is worth
// more than the pod's phase.
func stateReason(container map[string]any) string {
	state, _ := container["state"].(map[string]any)

	if waiting, ok := state["waiting"].(map[string]any); ok {
		reason, _ := waiting["reason"].(string)
		if reason != "" && !waitingIsNormal[reason] {
			return reason
		}
	}
	if terminated, ok := state["terminated"].(map[string]any); ok {
		reason, _ := terminated["reason"].(string)
		if reason != "" && reason != "Completed" {
			return reason
		}
	}
	return ""
}

// WorkloadTrouble reads why a workload is not at the size it was asked to be.
//
// A Deployment's own page should say what its pods' page says. Reading "3/5 ready" and
// having to open the pods to learn why is the round trip this exists to remove: the
// controller already wrote the reason into a condition.
func WorkloadTrouble(obj *unstructured.Unstructured, desired, ready int64) *Trouble {
	// A controller states its own unhappiness in conditions, and the message names the
	// constraint: a quota, an unavailable ReplicaSet, a failed create.
	for _, condition := range conditions(obj) {
		conditionType, _ := condition["type"].(string)
		status, _ := condition["status"].(string)
		reason, _ := condition["reason"].(string)
		message, _ := condition["message"].(string)

		switch {
		case conditionType == "Available" && status == "False",
			conditionType == "ReplicaFailure" && status == "True",
			conditionType == "Failed" && status == "True":
			return &Trouble{
				Reason:   orElse(reason, conditionType),
				Detail:   orElse(message, "The controller reports it is not available."),
				Severity: SeverityError,
			}
		case conditionType == "Progressing" && status == "False":
			return &Trouble{
				Reason:   orElse(reason, "NotProgressing"),
				Detail:   orElse(message, "The rollout has stopped making progress."),
				Severity: SeverityError,
			}
		}
	}

	if desired > 0 && ready == 0 {
		return &Trouble{
			Reason:   "Unavailable",
			Detail:   fmt.Sprintf("None of the %d wanted replicas are ready.", desired),
			Severity: SeverityError,
		}
	}
	if ready < desired {
		return &Trouble{
			Reason: "Degraded",
			Detail: fmt.Sprintf("%d of %d replicas are ready; the rest are starting or failing.", ready, desired),
			// Fewer than wanted is normal during a rollout, so this is a warning until a
			// condition says otherwise — and a condition usually does.
			Severity:  SeverityWarning,
			Transient: true,
		}
	}
	return nil
}
