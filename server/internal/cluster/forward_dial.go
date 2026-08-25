package cluster

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/transport/spdy"

	"github.com/erolbeyaz/kubby/internal/store"
)

// DialForward opens one tunnelled connection to a port inside a pod.
//
// It is a net.Conn so an ordinary http.Transport can dial through it: a pod's own web UI
// is then reachable from the reader's browser without anything being forwarded to their
// machine, which a browser could not receive anyway.
func (s *Service) DialForward(ctx context.Context, cluster *store.Cluster, target ForwardTarget, impersonate *ImpersonationConfig) (net.Conn, error) {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	url := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(target.Namespace).
		Name(target.Pod).
		SubResource("portforward").
		URL()

	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("prepare tunnel: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", url)
	connection, _, err := dialer.Dial(portForwardProtocol)
	if err != nil {
		return nil, fmt.Errorf("open tunnel to %s: %w", target.Pod, err)
	}

	headers := http.Header{}
	headers.Set(corev1PortHeader, strconv.Itoa(target.Port))
	headers.Set(corev1RequestIDHeader, "0")

	// The error stream first: the cluster refuses a closed port here, and reading data
	// before it would block on a connection that is never going to carry any.
	headers.Set(corev1StreamType, "error")
	errorStream, err := connection.CreateStream(headers)
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("open tunnel: %w", err)
	}

	headers.Set(corev1StreamType, "data")
	dataStream, err := connection.CreateStream(headers)
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("open tunnel: %w", err)
	}

	conn := &forwardConn{
		stream:  dataStream,
		release: connection.Close,
		name:    fmt.Sprintf("%s/%s:%d", target.Namespace, target.Pod, target.Port),
	}

	// A refusal arrives on its own stream after the dial has already succeeded, so it can
	// only be surfaced by ending the connection the caller is holding.
	go func() {
		message, _ := io.ReadAll(errorStream)
		if len(message) > 0 {
			_ = conn.Close()
		}
	}()

	return conn, nil
}

// forwardConn presents a SPDY stream as a connection. Deadlines are accepted and ignored:
// the stream keeps no timer of its own, and the caller's context governs the lifetime.
type forwardConn struct {
	stream  io.ReadWriteCloser
	release func() error
	name    string
	closed  bool
}

func (c *forwardConn) Read(p []byte) (int, error)  { return c.stream.Read(p) }
func (c *forwardConn) Write(p []byte) (int, error) { return c.stream.Write(p) }

func (c *forwardConn) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	_ = c.stream.Close()
	return c.release()
}

func (c *forwardConn) LocalAddr() net.Addr              { return forwardAddr(c.name) }
func (c *forwardConn) RemoteAddr() net.Addr             { return forwardAddr(c.name) }
func (c *forwardConn) SetDeadline(time.Time) error      { return nil }
func (c *forwardConn) SetReadDeadline(time.Time) error  { return nil }
func (c *forwardConn) SetWriteDeadline(time.Time) error { return nil }

type forwardAddr string

func (a forwardAddr) Network() string { return "kubby-forward" }
func (a forwardAddr) String() string  { return string(a) }
