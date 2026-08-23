package cluster

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/erolbeyaz/kubby/internal/k8s"
	"github.com/erolbeyaz/kubby/internal/store"
)

// PodRestarts explains why a pod's containers have restarted.
//
// This is the question a restart count raises and does not answer. Reading it should not
// require opening the pod, and it should not require knowing that exit code 137 means
// the kernel killed the process.
func (s *Service) PodRestarts(ctx context.Context, cluster *store.Cluster, namespace, name string, sidecars []string, impersonate *ImpersonationConfig) (*k8s.RestartSummary, error) {
	pod, err := s.podSpec(ctx, cluster, namespace, name, impersonate)
	if err != nil {
		return nil, err
	}

	summary := k8s.NewClassifier(sidecars).Summarise(
		restartsOf(pod.Status.ContainerStatuses),
		restartsOf(pod.Status.InitContainerStatuses),
	)
	return &summary, nil
}

func restartsOf(statuses []corev1.ContainerStatus) []k8s.ContainerRestarts {
	out := make([]k8s.ContainerRestarts, 0, len(statuses))

	for _, status := range statuses {
		entry := k8s.ContainerRestarts{Name: status.Name, Count: status.RestartCount}
		// LastTerminationState, not State: the current state of a container that came
		// back is "running", and the reason it went away is only recorded in the last.
		if terminated := status.LastTerminationState.Terminated; terminated != nil {
			entry.Last = &k8s.Termination{
				Reason:     terminated.Reason,
				ExitCode:   terminated.ExitCode,
				Signal:     terminated.Signal,
				Message:    terminated.Message,
				StartedAt:  utcOrEmpty(terminated.StartedAt.Time),
				FinishedAt: utcOrEmpty(terminated.FinishedAt.Time),
			}
		}
		out = append(out, entry)
	}
	return out
}

// utcOrEmpty keeps every timestamp the server emits in UTC RFC 3339; conversion to local
// time happens in the browser (ADR-026).
func utcOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
