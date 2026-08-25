package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/erolbeyaz/kubby/internal/store"
)

// NodeShellSettings is where the shell pod comes from.
type NodeShellSettings struct {
	Enabled    bool
	Image      string
	Namespace  string
	PullSecret string
}

// nodeShellCommand enters the host's namespaces from inside the pod. There is no
// Kubernetes API for a shell on a node; a privileged pod plus nsenter is the way, and it
// is the way every tool that offers this uses.
var nodeShellCommand = []string{
	"nsenter", "--target", "1", "--mount", "--uts", "--ipc", "--net", "--pid", "--",
	"/bin/sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh",
}

// StartNodeShell creates the privileged pod a node shell runs in and waits for it.
//
// Everything about this is deliberate and none of it is on by default: the pod is root
// on the machine, so it is admin-only, refused on a locked cluster, and removed when the
// session ends (ADR-064).
func (s *Service) StartNodeShell(ctx context.Context, cluster *store.Cluster, node string, settings NodeShellSettings, impersonate *ImpersonationConfig) (namespace, name string, err error) {
	if !settings.Enabled {
		return "", "", fmt.Errorf("node shells are turned off; an admin enables them in Kubby Settings")
	}
	if settings.Image == "" || settings.Namespace == "" {
		return "", "", fmt.Errorf("node shells have no image or namespace configured")
	}

	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return "", "", err
	}

	podType, err := LookupType("pods")
	if err != nil {
		return "", "", err
	}

	name = "kubby-node-shell-" + strings.ToLower(uuid.NewString()[:8])
	pod := nodeShellPod(name, node, settings)

	created, err := client.Resource(podType.GVR()).Namespace(settings.Namespace).
		Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return "", "", translateNodeShellError(err, settings)
	}

	if err := s.waitForShell(ctx, cluster, settings.Namespace, created.GetName(), impersonate); err != nil {
		// A pod that never started is not left behind to be found by someone else later.
		_ = s.Delete(context.WithoutCancel(ctx), cluster,
			DeleteRequest{Type: podType, Namespace: settings.Namespace, Name: created.GetName()}, impersonate)
		return "", "", err
	}
	return settings.Namespace, created.GetName(), nil
}

// StopNodeShell removes the pod. Called when the session ends, and again by the sweeper
// for anything a crash left behind.
func (s *Service) StopNodeShell(ctx context.Context, cluster *store.Cluster, namespace, name string, impersonate *ImpersonationConfig) error {
	podType, err := LookupType("pods")
	if err != nil {
		return err
	}
	return s.Delete(ctx, cluster, DeleteRequest{Type: podType, Namespace: namespace, Name: name}, impersonate)
}

// waitForShell blocks until the container is running, or explains why it never will be.
//
// Waiting for a timeout and then saying "timed out" is the worst answer available: the
// kubelet already said what is wrong, usually within a second.
func (s *Service) waitForShell(ctx context.Context, cluster *store.Cluster, namespace, name string, impersonate *ImpersonationConfig) error {
	deadline := time.Now().Add(nodeShellTimeout)

	for time.Now().Before(deadline) {
		pod, err := s.podSpec(ctx, cluster, namespace, name, impersonate)
		if err != nil {
			return err
		}

		for _, status := range pod.Status.ContainerStatuses {
			if status.State.Running != nil {
				return nil
			}
			if waiting := status.State.Waiting; waiting != nil && !nodeShellStarting[waiting.Reason] {
				return fmt.Errorf("the node shell could not start: %s. %s",
					waiting.Reason, orShrug(waiting.Message))
			}
			if terminated := status.State.Terminated; terminated != nil {
				return fmt.Errorf("the node shell exited immediately: %s", orShrug(terminated.Reason))
			}
		}

		for _, condition := range pod.Status.Conditions {
			if condition.Type == "PodScheduled" && condition.Status == "False" {
				return fmt.Errorf("the node shell could not be scheduled: %s", orShrug(condition.Message))
			}
		}

		if !sleep(ctx, 300*time.Millisecond) {
			return ctx.Err()
		}
	}
	return fmt.Errorf("the node shell did not start within %s and the cluster said nothing about why",
		nodeShellTimeout)
}

// nodeShellStarting are the waiting reasons that mean "working on it".
var nodeShellStarting = map[string]bool{"ContainerCreating": true, "PodInitializing": true}

func nodeShellPod(name, node string, settings NodeShellSettings) *unstructured.Unstructured {
	container := map[string]any{
		"name":    "shell",
		"image":   settings.Image,
		"command": toAny(nodeShellCommand),
		"stdin":   true,
		"tty":     true,
		"securityContext": map[string]any{
			"privileged": true,
		},
	}

	spec := map[string]any{
		"nodeName":                      node,
		"restartPolicy":                 "Never",
		"terminationGracePeriodSeconds": int64(0),
		"hostPID":                       true,
		"hostIPC":                       true,
		"hostNetwork":                   true,
		// A node worth opening a shell on is often one that is tainted, and refusing to
		// schedule there would make the feature useless exactly when it is needed.
		"tolerations": []any{map[string]any{"operator": "Exists"}},
		"containers":  []any{container},
	}
	if settings.PullSecret != "" {
		spec["imagePullSecrets"] = []any{map[string]any{"name": settings.PullSecret}}
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":      name,
			"namespace": settings.Namespace,
			"labels": map[string]any{
				// The sweeper finds abandoned shells by this label, so a crash cannot
				// leave a privileged pod running in someone's cluster.
				"app.kubernetes.io/managed-by": "kubby",
				"kubby.io/node-shell":          "true",
			},
			"annotations": map[string]any{"kubby.io/node": node},
		},
		"spec": spec,
	}}
}

// translateNodeShellError names the refusal, because the two likely ones need different
// things done about them.
func translateNodeShellError(err error, settings NodeShellSettings) error {
	text := err.Error()

	switch {
	case apierrors.IsForbidden(err) && strings.Contains(text, "PodSecurity"):
		return fmt.Errorf("namespace %q refuses privileged pods (PodSecurity admission). "+
			"A node shell needs one; choose a namespace that allows it in Kubby Settings",
			settings.Namespace)
	case apierrors.IsForbidden(err):
		return fmt.Errorf("%w: the cluster credential may not create pods in %q",
			ErrClusterDenied, settings.Namespace)
	case apierrors.IsNotFound(err):
		return fmt.Errorf("namespace %q does not exist", settings.Namespace)
	}
	return fmt.Errorf("could not start the node shell: %w", err)
}

// SweepNodeShells removes shells nothing is attached to any more.
//
// A privileged pod left running in someone's cluster is a hole this tool opened, so the
// cleanup does not depend on a session ending politely.
func (s *Service) SweepNodeShells(ctx context.Context, cluster *store.Cluster, namespace string, olderThan time.Duration, impersonate *ImpersonationConfig) (int, error) {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return 0, err
	}

	podType, err := LookupType("pods")
	if err != nil {
		return 0, err
	}

	pods, err := client.Resource(podType.GVR()).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "kubby.io/node-shell=true",
	})
	if err != nil {
		return 0, translateAPIError(err, podType)
	}

	removed := 0
	cutoff := time.Now().Add(-olderThan)
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.GetCreationTimestamp().After(cutoff) {
			continue
		}
		if err := s.StopNodeShell(ctx, cluster, namespace, pod.GetName(), impersonate); err == nil {
			removed++
		}
	}
	return removed, nil
}

func toAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func orShrug(message string) string {
	if strings.TrimSpace(message) == "" {
		return "the cluster gave no further detail"
	}
	return message
}

// nodeShellTimeout is a last resort. The kubelet normally says what is wrong long before
// this, and that message is what the reader gets.
const nodeShellTimeout = 45 * time.Second
