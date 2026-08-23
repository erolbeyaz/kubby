package cluster

import (
	"context"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/erolbeyaz/kubby/internal/k8s"
	"github.com/erolbeyaz/kubby/internal/store"
)

// LogRequest describes which log to read.
type LogRequest struct {
	Namespace string
	Pod       string
	// Container is optional: an empty value opens the application container rather than
	// whichever one the API happens to list first (ADR-030).
	Container string
	Follow    bool
	// Previous reads the log of the container instance that died, which is the only
	// place a crash loop's cause is written down.
	Previous     bool
	TailLines    int64
	SinceSeconds int64
	Timestamps   bool
}

// PodContainers lists a pod's containers, classified so a log view can default to the
// application container and group the injected ones after it.
func (s *Service) PodContainers(ctx context.Context, cluster *store.Cluster, namespace, pod string, sidecars []string, impersonate *ImpersonationConfig) ([]k8s.Container, error) {
	spec, err := s.podSpec(ctx, cluster, namespace, pod, impersonate)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(spec.Spec.Containers))
	for _, container := range spec.Spec.Containers {
		names = append(names, container.Name)
	}
	inits := make([]string, 0, len(spec.Spec.InitContainers))
	for _, container := range spec.Spec.InitContainers {
		inits = append(inits, container.Name)
	}
	return k8s.NewClassifier(sidecars).Ordered(names, inits), nil
}

// OpenLog returns a stream of the pod's log. The caller closes it.
func (s *Service) OpenLog(ctx context.Context, cluster *store.Cluster, req LogRequest, sidecars []string, impersonate *ImpersonationConfig) (io.ReadCloser, string, error) {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, "", err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("create client: %w", err)
	}

	container := req.Container
	if container == "" {
		container, err = s.defaultContainer(ctx, cluster, req.Namespace, req.Pod, sidecars, impersonate)
		if err != nil {
			return nil, "", err
		}
	}

	options := &corev1.PodLogOptions{
		Container:  container,
		Follow:     req.Follow,
		Previous:   req.Previous,
		Timestamps: req.Timestamps,
	}
	if req.TailLines > 0 {
		options.TailLines = &req.TailLines
	}
	if req.SinceSeconds > 0 {
		options.SinceSeconds = &req.SinceSeconds
	}

	stream, err := client.CoreV1().Pods(req.Namespace).GetLogs(req.Pod, options).Stream(ctx)
	if err != nil {
		return nil, container, translateAPIError(err, ResourceType{Kind: "Pod", Resource: "pods"})
	}
	return stream, container, nil
}

func (s *Service) defaultContainer(ctx context.Context, cluster *store.Cluster, namespace, pod string, sidecars []string, impersonate *ImpersonationConfig) (string, error) {
	spec, err := s.podSpec(ctx, cluster, namespace, pod, impersonate)
	if err != nil {
		return "", err
	}

	names := make([]string, 0, len(spec.Spec.Containers))
	for _, container := range spec.Spec.Containers {
		names = append(names, container.Name)
	}
	return k8s.NewClassifier(sidecars).DefaultContainer(names), nil
}

func (s *Service) podSpec(ctx context.Context, cluster *store.Cluster, namespace, name string, impersonate *ImpersonationConfig) (*corev1.Pod, error) {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, translateAPIError(err, ResourceType{Kind: "Pod", Resource: "pods"})
	}
	return pod, nil
}

// DefaultLogTail is how much history a log view opens with. Enough to see what happened,
// short enough that opening a log is not a download.
const DefaultLogTail int64 = 500

// LogStreamTimeout bounds a non-following read. A follow has no deadline of its own; it
// ends when the client goes away.
const LogStreamTimeout = 30 * time.Second
