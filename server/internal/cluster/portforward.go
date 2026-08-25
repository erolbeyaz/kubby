package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/transport/spdy"

	"github.com/erolbeyaz/kubby/internal/store"
)

// ForwardTarget is what to reach.
type ForwardTarget struct {
	Namespace string
	// Pod is set directly, or resolved from a Service.
	Pod  string
	Port int
}

// ResolveForward turns a Service into a pod behind it, because port-forward reaches pods
// and a Service is a name for several of them.
func (s *Service) ResolveForward(ctx context.Context, cluster *store.Cluster, typeKey, namespace, name string, port int, impersonate *ImpersonationConfig) (*ForwardTarget, error) {
	if err := validateRef(namespace, name); err != nil {
		return nil, err
	}
	if typeKey == "pods" {
		return &ForwardTarget{Namespace: namespace, Pod: name, Port: port}, nil
	}

	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}

	serviceType, err := LookupType("services")
	if err != nil {
		return nil, err
	}
	service, err := client.Resource(serviceType.GVR()).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, translateAPIError(err, serviceType)
	}

	relations := servedPods(ctx, s, cluster, service, namespace, impersonate)
	for _, relation := range relations {
		if relation.Severity == "" {
			return &ForwardTarget{Namespace: namespace, Pod: relation.Name, Port: port}, nil
		}
	}
	return nil, fmt.Errorf("no ready pod is behind service %q", name)
}

// Forward opens a stream to a port inside a pod and copies bytes both ways.
//
// The tunnel runs server-side, so what the reader gets is a stream in their browser
// rather than a socket on their machine. That is a different thing from kubectl's
// port-forward and a better fit here: nothing has to be installed, and the connection is
// gone when the tab is.
func (s *Service) Forward(ctx context.Context, cluster *store.Cluster, target ForwardTarget, local io.ReadWriter, impersonate *ImpersonationConfig) error {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	url := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(target.Namespace).
		Name(target.Pod).
		SubResource("portforward").
		URL()

	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return fmt.Errorf("prepare tunnel: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", url)
	connection, _, err := dialer.Dial(portForwardProtocol)
	if err != nil {
		return fmt.Errorf("open tunnel to %s: %w", target.Pod, err)
	}
	defer func() { _ = connection.Close() }()

	headers := http.Header{}
	headers.Set(corev1PortHeader, strconv.Itoa(target.Port))
	headers.Set(corev1RequestIDHeader, "0")

	// The error stream first: the cluster refuses a closed port here, and reading data
	// before it would block on a connection that is never going to carry any.
	headers.Set(corev1StreamType, "error")
	errorStream, err := connection.CreateStream(headers)
	if err != nil {
		return fmt.Errorf("open tunnel: %w", err)
	}
	defer func() { _ = errorStream.Close() }()

	headers.Set(corev1StreamType, "data")
	dataStream, err := connection.CreateStream(headers)
	if err != nil {
		return fmt.Errorf("open tunnel: %w", err)
	}
	defer func() { _ = dataStream.Close() }()

	refused := make(chan error, 1)
	go func() {
		message, _ := io.ReadAll(errorStream)
		if len(message) > 0 {
			refused <- fmt.Errorf("port %d in %s: %s", target.Port, target.Pod, message)
			return
		}
		refused <- nil
	}()

	done := make(chan error, 2)
	go func() { _, err := io.Copy(dataStream, local); done <- err }()
	go func() { _, err := io.Copy(local, dataStream); done <- err }()

	select {
	case <-ctx.Done():
		return nil
	case err := <-refused:
		if err != nil {
			return err
		}
		return <-done
	case err := <-done:
		if err != nil && !isClosed(err) {
			return err
		}
		return nil
	}
}

func isClosed(err error) bool {
	if err == nil {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) || errors.Is(err, io.EOF)
}

const (
	portForwardProtocol   = "portforward.k8s.io"
	corev1PortHeader      = "port"
	corev1RequestIDHeader = "requestID"
	corev1StreamType      = "streamType"
)

// ForwardablePort is one port an object declares.
type ForwardablePort struct {
	Name      string
	Port      int
	Protocol  string
	Container string
}

// ForwardablePorts reads the ports an object declares, so the reader picks one from the
// object rather than recalling a number. A pod's ports are per container; a service's are
// its own, and it is the service port that is forwarded.
func (s *Service) ForwardablePorts(ctx context.Context, cluster *store.Cluster, typeKey, namespace, name string, impersonate *ImpersonationConfig) ([]ForwardablePort, error) {
	if err := validateRef(namespace, name); err != nil {
		return nil, err
	}

	resourceType, err := LookupType(typeKey)
	if err != nil {
		return nil, err
	}

	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}
	object, err := client.Resource(resourceType.GVR()).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, translateAPIError(err, resourceType)
	}

	switch typeKey {
	case "services":
		return servicePorts(object), nil
	case "pods":
		return containerPorts(object, "spec", "containers"), nil
	default:
		// Everything with a pod template — Deployment, StatefulSet, DaemonSet, Job —
		// declares its ports in the template rather than on itself.
		return containerPorts(object, "spec", "template", "spec", "containers"), nil
	}
}

func servicePorts(object *unstructured.Unstructured) []ForwardablePort {
	entries, _, _ := unstructured.NestedSlice(object.Object, "spec", "ports")

	out := make([]ForwardablePort, 0, len(entries))
	for _, entry := range entries {
		port, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		number, found, _ := unstructured.NestedInt64(port, "port")
		if !found {
			continue
		}
		name, _, _ := unstructured.NestedString(port, "name")
		protocol, _, _ := unstructured.NestedString(port, "protocol")
		out = append(out, ForwardablePort{Name: name, Port: int(number), Protocol: orTCP(protocol)})
	}
	return out
}

func containerPorts(object *unstructured.Unstructured, path ...string) []ForwardablePort {
	containers, _, _ := unstructured.NestedSlice(object.Object, path...)

	var out []ForwardablePort
	for _, entry := range containers {
		container, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		containerName, _, _ := unstructured.NestedString(container, "name")
		ports, _, _ := unstructured.NestedSlice(container, "ports")

		for _, portEntry := range ports {
			port, ok := portEntry.(map[string]any)
			if !ok {
				continue
			}
			number, found, _ := unstructured.NestedInt64(port, "containerPort")
			if !found {
				continue
			}
			name, _, _ := unstructured.NestedString(port, "name")
			protocol, _, _ := unstructured.NestedString(port, "protocol")
			out = append(out, ForwardablePort{
				Name: name, Port: int(number), Protocol: orTCP(protocol), Container: containerName,
			})
		}
	}
	return out
}

func orTCP(protocol string) string {
	if protocol == "" {
		return "TCP"
	}
	return protocol
}
