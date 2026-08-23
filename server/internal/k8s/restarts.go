package k8s

import "fmt"

// Termination is why a container instance ended.
type Termination struct {
	Reason     string `json:"reason,omitempty"`
	ExitCode   int32  `json:"exitCode"`
	Signal     int32  `json:"signal,omitempty"`
	Message    string `json:"message,omitempty"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

// ContainerRestarts is one container's restart history.
type ContainerRestarts struct {
	Name  string        `json:"name"`
	Role  ContainerRole `json:"role"`
	Count int32         `json:"count"`
	Last  *Termination  `json:"last,omitempty"`
}

// RestartSummary separates the workload's restarts from its platform's.
//
// This split is the point (ADR-030 §3). istio-proxy restarting a few times at pod start
// is not an application fault, and a single total that mixes the two sends the reader
// looking in the wrong place — a correct number producing a wrong conclusion.
type RestartSummary struct {
	App     int32               `json:"app"`
	Sidecar int32               `json:"sidecar"`
	Init    int32               `json:"init"`
	Details []ContainerRestarts `json:"details,omitempty"`
}

// Explain turns a termination into the sentence a reader needs instead of the object.
func (t *Termination) Explain() string {
	if t == nil {
		return ""
	}
	switch {
	case t.Reason == "OOMKilled":
		return "Killed for exceeding its memory limit."
	case t.Reason == "Completed" && t.ExitCode == 0:
		return "Exited normally."
	case t.Signal == 9:
		return "Killed (SIGKILL) — usually the node or an operator, not the process itself."
	case t.Signal == 15:
		return "Terminated (SIGTERM) — a shutdown it did not finish in time."
	case t.ExitCode == 1:
		return "Exited with code 1: the process failed. Its last log lines say why."
	case t.ExitCode == 137:
		return "Exit code 137: killed by SIGKILL, most often the memory limit."
	case t.ExitCode == 143:
		return "Exit code 143: terminated by SIGTERM during shutdown."
	case t.ExitCode != 0:
		return fmt.Sprintf("Exited with code %d.", t.ExitCode)
	}
	return ""
}

// Summarise builds the restart picture for one pod.
func (c *Classifier) Summarise(containers, initContainers []ContainerRestarts) RestartSummary {
	summary := RestartSummary{}

	for _, container := range containers {
		container.Role = RoleApp
		if c.IsSidecar(container.Name) {
			container.Role = RoleSidecar
			summary.Sidecar += container.Count
		} else {
			summary.App += container.Count
		}
		summary.Details = append(summary.Details, container)
	}

	for _, container := range initContainers {
		container.Role = RoleInit
		summary.Init += container.Count
		summary.Details = append(summary.Details, container)
	}
	return summary
}

// Badge is what a list row shows: the workload's own count, with the platform's noted
// separately rather than folded in.
func (s RestartSummary) Badge() (count int32, note string) {
	if s.Sidecar > 0 {
		return s.App, fmt.Sprintf("+%d sidecar", s.Sidecar)
	}
	return s.App, ""
}
