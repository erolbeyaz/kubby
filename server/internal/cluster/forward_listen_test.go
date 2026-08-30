package cluster

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A forward is only useful if a whole HTTP exchange survives it: a request written in,
// a response read back, and the request body ended so the server answers at all.
func TestAForwardCarriesAWholeRequestAndResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = fmt.Fprintf(w, "saw %s %s with %q", r.Method, r.URL.Path, body)
	}))
	defer upstream.Close()

	forward := serveTo(t, upstream.Listener.Addr().String(), nil, nil)
	defer forward.Close()

	response, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/things", forward.Port),
		"text/plain", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("POST through the forward: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	payload, _ := io.ReadAll(response.Body)
	if got, want := string(payload), `saw POST /things with "hello"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Every connection gets a tunnel of its own, so one pod dropping a connection costs that
// request and no other.
func TestEachConnectionDialsItsOwnTunnel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()

	var dials atomic.Int64
	forward := serveTo(t, upstream.Listener.Addr().String(), func() { dials.Add(1) }, nil)
	defer forward.Close()

	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	for i := 0; i < 3; i++ {
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", forward.Port))
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}

	if got := dials.Load(); got != 3 {
		t.Errorf("%d tunnels for 3 connections", got)
	}
}

// A pod that cannot be reached closes the connection, and the browser reports only
// "reset". Without this the reason is nowhere at all.
func TestAFailedDialIsReported(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	reported := make(chan error, 1)
	forward := serve(context.Background(), listener,
		func(context.Context) (net.Conn, error) { return nil, fmt.Errorf("no such pod") },
		nil,
		func(err error) { reported <- err },
	)
	defer forward.Close()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write([]byte("GET / HTTP/1.0\r\n\r\n"))
	_ = conn.Close()

	select {
	case err := <-reported:
		if !strings.Contains(err.Error(), "no such pod") {
			t.Errorf("reported %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("a failed dial was never reported")
	}
}

// Traffic on a local port never reaches Kubby as an HTTP request, so without this the
// idle sweep would close a tunnel somebody is using.
func TestUseIsReportedSoAnActiveTunnelIsNotSweptAway(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer upstream.Close()

	touched := make(chan struct{}, 1)
	forward := serveTo(t, upstream.Listener.Addr().String(), nil, func() {
		select {
		case touched <- struct{}{}:
		default:
		}
	})
	defer forward.Close()

	response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", forward.Port))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()

	select {
	case <-touched:
	case <-time.After(3 * time.Second):
		t.Error("using the tunnel did not count as use")
	}
}

// Stopping has to free the port, or a reader who closed a forward cannot open the same
// one again.
func TestClosingFreesThePort(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer upstream.Close()

	forward := serveTo(t, upstream.Listener.Addr().String(), nil, nil)
	port := forward.Port
	forward.Close()
	// Twice, because a reader stopping a tunnel and the idle sweep reaching it are two
	// paths to the same call.
	forward.Close()

	deadline := time.Now().Add(3 * time.Second)
	for {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			_ = listener.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("port %d is still held: %v", port, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAPortThatIsAlreadyTakenIsRefusedByName(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()

	port := taken.Addr().(*net.TCPAddr).Port
	if _, err := listen("127.0.0.1", port, PortRange{}); err == nil {
		t.Fatalf("port %d was bound twice", port)
	} else if !strings.Contains(err.Error(), "not available") {
		t.Errorf("said %q", err)
	}
}

// The range exists for a deployment that publishes a fixed set of ports; binding outside
// it would hand out an address nothing forwards.
func TestARangeIsHonouredAndItsExhaustionSaidPlainly(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	port := first.Addr().(*net.TCPAddr).Port

	// A range of exactly one, already taken.
	if _, err := listen("127.0.0.1", 0, PortRange{From: port, To: port}); err == nil {
		t.Error("a full range still produced a listener")
	} else if !strings.Contains(err.Error(), "in use") {
		t.Errorf("said %q", err)
	}

	// A range of two with one free.
	listener, err := listen("127.0.0.1", 0, PortRange{From: port, To: port + 1})
	if err != nil {
		t.Fatalf("a range with a free port failed: %v", err)
	}
	defer func() { _ = listener.Close() }()
	if got := listener.Addr().(*net.TCPAddr).Port; got != port+1 {
		t.Errorf("bound %d, want %d", got, port+1)
	}
}

func TestParsePortRange(t *testing.T) {
	if got, err := ParsePortRange(""); err != nil || !got.Any() {
		t.Errorf("empty gave %+v, %v", got, err)
	}
	if got, err := ParsePortRange(" 30000-30049 "); err != nil || got.From != 30000 || got.To != 30049 {
		t.Errorf("range gave %+v, %v", got, err)
	}
	for _, bad := range []string{"30000", "abc-30049", "30049-30000", "0-10", "1-70000"} {
		if _, err := ParsePortRange(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestLocalForwardsCanBeTurnedOff(t *testing.T) {
	var svc *Service
	if _, err := svc.ListenForward(context.Background(), nil, ForwardTarget{}, nil, "  ", 0, PortRange{}, nil, nil); err == nil {
		t.Error("an empty bind address still opened a port")
	}
}

// serveTo pipes a local port to an address, which is what a pod tunnel does once the
// Kubernetes part is out of the way.
func serveTo(t *testing.T, address string, onDial, touch func()) *LocalForward {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return serve(context.Background(), listener, func(ctx context.Context) (net.Conn, error) {
		if onDial != nil {
			onDial()
		}
		return net.Dial("tcp", address)
	}, touch, nil)
}
