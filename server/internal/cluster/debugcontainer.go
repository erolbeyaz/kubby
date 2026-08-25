package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/erolbeyaz/kubby/internal/store"
)

// StartDebugContainer attaches a container with a shell to a running pod.
//
// A distroless image has no shell, and that is deliberate — the answer is not to rebuild
// the image but to bring one alongside it. The container shares the pod's namespaces, so
// its processes, network and (with targetContainer) its filesystem are all reachable from
// the prompt (ADR-013 #4).
//
// It cannot be removed: an ephemeral container lives until the pod does. That is a
// property of the Kubernetes API, not a shortcut here, and the reader is told so before
// one is started.
func (s *Service) StartDebugContainer(ctx context.Context, cluster *store.Cluster, namespace, pod, targetContainer, image string, impersonate *ImpersonationConfig) (string, error) {
	if strings.TrimSpace(image) == "" {
		return "", fmt.Errorf("no debug image is configured")
	}

	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return "", err
	}
	podType, err := LookupType("pods")
	if err != nil {
		return "", err
	}

	// A name per attempt: a second debug container is a second name, and reusing one the
	// pod already carries is rejected by the API server.
	name := fmt.Sprintf("kubby-debug-%d", time.Now().UTC().Unix())

	container := map[string]any{
		"name":                     name,
		"image":                    image,
		"command":                  toAny([]string{"/bin/sh"}),
		"stdin":                    true,
		"tty":                      true,
		"terminationMessagePolicy": "File",
		"imagePullPolicy":          "IfNotPresent",
	}
	if targetContainer != "" {
		// Without this the prompt sees the pod's processes but not the container's files,
		// which is half of why anyone opens one.
		container["targetContainerName"] = targetContainer
	}

	current, err := client.Resource(podType.GVR()).Namespace(namespace).
		Get(ctx, pod, metav1.GetOptions{}, "ephemeralcontainers")
	if err != nil {
		return "", translateDebugError(err)
	}

	existing, _, _ := unstructured.NestedSlice(current.Object, "spec", "ephemeralContainers")
	if err := unstructured.SetNestedSlice(current.Object, append(existing, container),
		"spec", "ephemeralContainers"); err != nil {
		return "", fmt.Errorf("build the debug container: %w", err)
	}

	if _, err := client.Resource(podType.GVR()).Namespace(namespace).
		Update(ctx, current, metav1.UpdateOptions{}, "ephemeralcontainers"); err != nil {
		return "", translateDebugError(err)
	}

	if err := s.waitForDebugContainer(ctx, client, podType, namespace, pod, name); err != nil {
		return "", err
	}
	return name, nil
}

// waitForDebugContainer holds until the kubelet has actually started it, so the session
// that follows does not open against a container that does not exist yet.
func (s *Service) waitForDebugContainer(ctx context.Context, client dynamic.Interface, podType ResourceType, namespace, pod, name string) error {
	deadline := time.Now().Add(debugContainerTimeout)

	for time.Now().Before(deadline) {
		current, err := client.Resource(podType.GVR()).Namespace(namespace).Get(ctx, pod, metav1.GetOptions{})
		if err != nil {
			return translateDebugError(err)
		}

		statuses, _, _ := unstructured.NestedSlice(current.Object, "status", "ephemeralContainerStatuses")
		for _, entry := range statuses {
			status, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			if statusName, _, _ := unstructured.NestedString(status, "name"); statusName != name {
				continue
			}
			if _, running, _ := unstructured.NestedMap(status, "state", "running"); running {
				return nil
			}
			// The kubelet's own words beat a timeout: a registry it cannot reach says so
			// here within seconds.
			if reason, _, _ := unstructured.NestedString(status, "state", "waiting", "reason"); reason != "" {
				if failedPull(reason) {
					message, _, _ := unstructured.NestedString(status, "state", "waiting", "message")
					return fmt.Errorf("the debug container did not start (%s): %s", reason, orShrug(message))
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("the debug container did not start within %s", debugContainerTimeout)
}

func failedPull(reason string) bool {
	switch reason {
	case "ErrImagePull", "ImagePullBackOff", "CreateContainerConfigError", "CreateContainerError":
		return true
	}
	return false
}

func translateDebugError(err error) error {
	switch {
	case apierrors.IsForbidden(err):
		return fmt.Errorf("%w: the cluster credential may not attach a debug container to this pod",
			ErrClusterDenied)
	case apierrors.IsNotFound(err):
		return fmt.Errorf("this pod is gone")
	case apierrors.IsMethodNotSupported(err) || apierrors.IsBadRequest(err):
		return fmt.Errorf("this cluster does not accept ephemeral containers")
	}
	return fmt.Errorf("could not attach the debug container: %w", err)
}

// Kept separate from the node shell's: attaching to an existing pod skips scheduling and
// should be quick, so waiting as long would only delay the reader's error message.
const debugContainerTimeout = 30 * time.Second
