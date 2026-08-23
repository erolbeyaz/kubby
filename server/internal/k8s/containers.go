// Package k8s holds reasoning about Kubernetes objects that several domains share.
package k8s

import (
	"sort"
	"strings"
)

// DefaultSidecars are containers injected by a platform rather than written by whoever
// deployed the workload. The list is a default, not a truth: KUBBY_SIDECAR_CONTAINERS
// replaces it for clusters that inject something else.
var DefaultSidecars = []string{
	"istio-proxy",
	"istio-init",
	"linkerd-proxy",
	"linkerd-init",
	"envoy",
	"vault-agent",
	"vault-agent-init",
	"oauth2-proxy",
	"fluent-bit",
	"fluentd",
	"filebeat",
	"vector",
	"promtail",
	"otel-collector",
	"cloudsql-proxy",
	"config-reloader",
}

// ContainerRole says how a container should be treated by log, exec and restart views.
type ContainerRole string

const (
	// RoleApp is the container the workload exists to run.
	RoleApp ContainerRole = "app"
	// RoleSidecar is injected alongside it.
	RoleSidecar ContainerRole = "sidecar"
	// RoleInit runs to completion before the others start.
	RoleInit ContainerRole = "init"
)

// Container is one container in a pod, classified.
type Container struct {
	Name string        `json:"name"`
	Role ContainerRole `json:"role"`
}

// Classifier decides which containers are the workload's own.
//
// Opening the wrong container's log is the failure this exists to prevent: a user reading
// istio-proxy access records while believing they are reading application output reaches a
// wrong conclusion with a correct tool.
type Classifier struct {
	sidecars map[string]struct{}
}

// NewClassifier builds a classifier from container names. An empty list means the
// defaults, so a cluster that injects nothing unusual needs no configuration.
func NewClassifier(names []string) *Classifier {
	if len(names) == 0 {
		names = DefaultSidecars
	}

	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	return &Classifier{sidecars: set}
}

// IsSidecar reports whether a container name is platform-injected.
func (c *Classifier) IsSidecar(name string) bool {
	_, found := c.sidecars[name]
	return found
}

// Classify labels a pod's containers. Init containers are always their own group.
func (c *Classifier) Classify(containers, initContainers []string) []Container {
	out := make([]Container, 0, len(containers)+len(initContainers))

	for _, name := range containers {
		role := RoleApp
		if c.IsSidecar(name) {
			role = RoleSidecar
		}
		out = append(out, Container{Name: name, Role: role})
	}
	for _, name := range initContainers {
		out = append(out, Container{Name: name, Role: RoleInit})
	}
	return out
}

// DefaultContainer is the one a log or exec view should open with: the first container
// that is not platform-injected. A pod made entirely of sidecars falls back to its first
// container, because showing nothing would be worse than showing a proxy.
func (c *Classifier) DefaultContainer(containers []string) string {
	for _, name := range containers {
		if !c.IsSidecar(name) {
			return name
		}
	}
	if len(containers) > 0 {
		return containers[0]
	}
	return ""
}

// Ordered returns containers with the workload's own first and sidecars grouped after,
// each group alphabetical. Init containers keep their declared order, which is the order
// they run in and the only order that means anything.
func (c *Classifier) Ordered(containers, initContainers []string) []Container {
	classified := c.Classify(containers, nil)

	sort.SliceStable(classified, func(i, j int) bool {
		if classified[i].Role != classified[j].Role {
			return classified[i].Role == RoleApp
		}
		return classified[i].Name < classified[j].Name
	})

	for _, name := range initContainers {
		classified = append(classified, Container{Name: name, Role: RoleInit})
	}
	return classified
}
