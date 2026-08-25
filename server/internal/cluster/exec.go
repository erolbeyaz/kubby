package cluster

import (
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/erolbeyaz/kubby/internal/k8s"
	"github.com/erolbeyaz/kubby/internal/store"
)

// ExecRequest is one interactive session.
type ExecRequest struct {
	Namespace string
	Pod       string
	// Container is optional: an empty value opens the application container rather than
	// whichever one the API lists first (ADR-030).
	Container string
	// Command is an argument list, never a shell string. Joining arguments into a string
	// and letting a shell split them again is how a pod name becomes an injection.
	Command []string
}

// ExecStreams are the ends the caller wires to the browser.
type ExecStreams struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Resize carries terminal sizes; nil means the far side is not a terminal.
	Resize <-chan remotecommand.TerminalSize
}

// DefaultShellCommand tries a real shell and falls back to the one every image has.
//
// A distroless image has neither, and the honest answer there is an ephemeral debug
// container rather than a shell that does not exist.
var DefaultShellCommand = []string{
	"/bin/sh", "-c",
	`command -v bash >/dev/null 2>&1 && exec bash || exec sh`,
}

// Exec opens an interactive session inside a container.
//
// The connection runs server-side: the browser talks to Kubby and Kubby talks to the API
// server's exec subresource. Nothing is needed on the reader's machine — no kubectl, no
// SSH, no WSL — because the reader's machine is not in the path at all (ADR-064).
func (s *Service) Exec(ctx context.Context, cluster *store.Cluster, req ExecRequest, sidecars []string, streams ExecStreams, impersonate *ImpersonationConfig) error {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	container := req.Container
	if container == "" {
		container, err = s.defaultContainer(ctx, cluster, req.Namespace, req.Pod, sidecars, impersonate)
		if err != nil {
			return err
		}
	}

	command := req.Command
	if len(command) == 0 {
		command = DefaultShellCommand
	}

	request := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(req.Pod).
		Namespace(req.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     streams.Stdin != nil,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(cfg, "POST", request.URL())
	if err != nil {
		return fmt.Errorf("open exec: %w", err)
	}

	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             streams.Stdin,
		Stdout:            streams.Stdout,
		Stderr:            streams.Stderr,
		Tty:               true,
		TerminalSizeQueue: sizeQueue{resize: streams.Resize},
	})
	if err != nil {
		return translateExecError(err, req.Pod, container)
	}
	return nil
}

// ExecContainers lists what a session may attach to, classified so the picker can default
// to the application container.
func (s *Service) ExecContainers(ctx context.Context, cluster *store.Cluster, namespace, pod string, sidecars []string, impersonate *ImpersonationConfig) ([]k8s.Container, error) {
	return s.PodContainers(ctx, cluster, namespace, pod, sidecars, impersonate)
}

// sizeQueue turns a channel of sizes into what client-go asks for. A nil channel means
// the queue blocks forever, which is what a non-terminal session wants.
type sizeQueue struct {
	resize <-chan remotecommand.TerminalSize
}

func (q sizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-q.resize
	if !ok {
		return nil
	}
	return &size
}

// translateExecError names the refusal. "exec failed" is true of all of them.
func translateExecError(err error, pod, container string) error {
	text := err.Error()

	switch {
	case strings.Contains(text, "executable file not found"), strings.Contains(text, "no such file or directory"):
		return fmt.Errorf("no shell in container %q of %q: a distroless image has none, "+
			"and a debug container is the way in", container, pod)
	case strings.Contains(text, "container not found"):
		return fmt.Errorf("container %q is not running in %q", container, pod)
	case strings.Contains(strings.ToLower(text), "forbidden"):
		return fmt.Errorf("%w: the cluster credential may not exec into pods", ErrClusterDenied)
	}
	return fmt.Errorf("exec ended: %w", err)
}
