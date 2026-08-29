package logsearch

import "strings"

// Fields names the parts of a log document Kubby needs.
//
// Every shipper spells them differently — Fluent Bit's Kubernetes filter writes
// `kubernetes.pod_name` and puts the message in `log`, while an ECS pipeline writes
// `kubernetes.pod.name` and `message`. Neither is more correct, so neither is compiled
// in as the only possibility.
//
// TODO: not persisted yet. The defaults match Fluent Bit, which is what the target
// deployments run; the settings screen that stores a different mapping is still to come.
type Fields struct {
	Timestamp string
	Message   string
	Pod       string
	Namespace string
	Container string
}

// DefaultFields is Fluent Bit's Kubernetes filter as it writes documents by default.
func DefaultFields() Fields {
	return Fields{
		Timestamp: "@timestamp",
		Message:   "log",
		Pod:       "kubernetes.pod_name",
		Namespace: "kubernetes.namespace_name",
		Container: "kubernetes.container_name",
	}
}

// withDefaults fills in whatever was left blank, so a partial mapping is usable.
func (f Fields) withDefaults() Fields {
	defaults := DefaultFields()
	if strings.TrimSpace(f.Timestamp) == "" {
		f.Timestamp = defaults.Timestamp
	}
	if strings.TrimSpace(f.Message) == "" {
		f.Message = defaults.Message
	}
	if strings.TrimSpace(f.Pod) == "" {
		f.Pod = defaults.Pod
	}
	if strings.TrimSpace(f.Namespace) == "" {
		f.Namespace = defaults.Namespace
	}
	if strings.TrimSpace(f.Container) == "" {
		f.Container = defaults.Container
	}
	return f
}
