package cluster

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/erolbeyaz/kubby/internal/store"
)

// LocalForward is a real TCP port on Kubby's own host, piped to a pod.
//
// It exists because a proxy under a path prefix cannot serve an arbitrary application.
// An app referring to its own files absolutely — `/assets/app.js` — has those requests
// resolved against the origin root, which under a prefix is Kubby rather than the pod;
// and a single-page app builds most of its URLs in JavaScript at runtime, where no
// server-side rewrite can reach them. Giving the app a port of its own gives it an
// origin of its own at its own root, and every one of those problems disappears rather
// than being worked around.
type LocalForward struct {
	// Port is what was actually bound, which is not what was asked for when 0 was.
	Port int

	listener net.Listener
	stop     context.CancelFunc
	closeOne sync.Once
}

// Close stops accepting and ends the tunnel.
func (f *LocalForward) Close() {
	if f == nil {
		return
	}
	f.closeOne.Do(func() {
		if f.stop != nil {
			f.stop()
		}
		if f.listener != nil {
			_ = f.listener.Close()
		}
	})
}

// PortRange is where a local forward may bind.
//
// A range exists for the deployment that runs Kubby in a container: a port nobody
// published is a port the browser cannot reach, and Docker and Kubernetes both want to
// know which ports to publish before anything asks for one.
type PortRange struct {
	From int
	To   int
}

// ParsePortRange reads "30000-30049", or an empty string meaning any free port.
func ParsePortRange(raw string) (PortRange, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return PortRange{}, nil
	}

	from, to, found := strings.Cut(trimmed, "-")
	if !found {
		return PortRange{}, fmt.Errorf("a port range looks like 30000-30049, got %q", raw)
	}

	low, err := strconv.Atoi(strings.TrimSpace(from))
	if err != nil {
		return PortRange{}, fmt.Errorf("%q is not a port number", from)
	}
	high, err := strconv.Atoi(strings.TrimSpace(to))
	if err != nil {
		return PortRange{}, fmt.Errorf("%q is not a port number", to)
	}
	if low < 1 || high > 65535 || low > high {
		return PortRange{}, fmt.Errorf("the port range must be inside 1-65535 and ascending, got %q", raw)
	}
	return PortRange{From: low, To: high}, nil
}

// Any reports whether any free port will do.
func (r PortRange) Any() bool { return r.From == 0 && r.To == 0 }

// ListenForward opens a local port and pipes everything on it to the pod.
//
// The listener outlives the request that opened it; it is closed by the reader, by the
// registry's idle sweep, or by the process shutting down.
func (s *Service) ListenForward(
	ctx context.Context,
	c *store.Cluster,
	target ForwardTarget,
	impersonate *ImpersonationConfig,
	bind string,
	wanted int,
	within PortRange,
	touch func(),
	// onDialError reports a tunnel that could not be opened. Without it a pod that
	// cannot be reached closes the connection and the browser says only "reset", which
	// names neither the pod nor the reason.
	onDialError func(error),
) (*LocalForward, error) {
	if strings.TrimSpace(bind) == "" {
		return nil, fmt.Errorf("local forwards are disabled")
	}

	listener, err := listen(bind, wanted, within)
	if err != nil {
		return nil, err
	}

	dial := func(ctx context.Context) (net.Conn, error) {
		conn, err := s.DialForward(ctx, c, target, impersonate)
		if err != nil {
			return nil, fmt.Errorf("dial %s/%s:%d: %w", target.Namespace, target.Pod, target.Port, err)
		}
		return conn, nil
	}
	return serve(ctx, listener, dial, touch, onDialError), nil
}

// serve accepts on the listener and pipes each connection to a freshly dialled tunnel.
//
// Split from ListenForward so the piping can be tested without a cluster: what is worth
// proving here is that bytes cross in both directions and that a request completes, and
// neither of those needs Kubernetes to be involved.
func serve(
	ctx context.Context,
	listener net.Listener,
	dial func(context.Context) (net.Conn, error),
	touch func(),
	onDialError func(error),
) *LocalForward {
	tunnelCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	forward := &LocalForward{
		Port:     listener.Addr().(*net.TCPAddr).Port,
		listener: listener,
		stop:     stop,
	}

	go func() {
		<-tunnelCtx.Done()
		_ = listener.Close()
	}()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			if touch != nil {
				touch()
			}
			go func() {
				defer func() { _ = conn.Close() }()
				// One tunnel per connection rather than one shared for the listener: a
				// pod that drops a connection should cost that request and no other.
				upstream, err := dial(tunnelCtx)
				if err != nil {
					if onDialError != nil {
						onDialError(err)
					}
					return
				}
				defer func() { _ = upstream.Close() }()
				pipe(conn, upstream)
			}()
		}
	}()

	return forward
}

// listen binds the port that was asked for, or hunts for a free one in the range.
func listen(bind string, wanted int, within PortRange) (net.Listener, error) {
	if wanted > 0 {
		listener, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(wanted)))
		if err != nil {
			return nil, fmt.Errorf("port %d on %s is not available: %w", wanted, bind, err)
		}
		return listener, nil
	}

	if within.Any() {
		listener, err := net.Listen("tcp", net.JoinHostPort(bind, "0"))
		if err != nil {
			return nil, fmt.Errorf("no local port could be opened on %s: %w", bind, err)
		}
		return listener, nil
	}

	for port := within.From; port <= within.To; port++ {
		if listener, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port))); err == nil {
			return listener, nil
		}
	}
	return nil, fmt.Errorf("every port in %d-%d is in use", within.From, within.To)
}

// pipe copies in both directions until either end stops.
func pipe(client, upstream net.Conn) {
	done := make(chan struct{}, 2)

	go func() {
		_, _ = io.Copy(upstream, client)
		// Half-close so the pod sees the end of the request rather than waiting on a
		// body that will never arrive.
		if closer, ok := upstream.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		if closer, ok := client.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		}
		done <- struct{}{}
	}()

	<-done
	<-done
}
