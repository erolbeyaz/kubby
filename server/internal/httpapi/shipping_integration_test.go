package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// receiver stands in for a SIEM: it accepts newline-delimited JSON and remembers it.
type receiver struct {
	mu    sync.Mutex
	lines []string
	fail  bool
}

func (r *receiver) start(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		shouldFail := r.fail
		r.mu.Unlock()

		if shouldFail {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("the sink is down"))
			return
		}

		body := readBodyOf(req)
		r.mu.Lock()
		for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
			if line != "" {
				r.lines = append(r.lines, line)
			}
		}
		r.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server
}

func (r *receiver) waitFor(t *testing.T, action string) map[string]any {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		lines := append([]string(nil), r.lines...)
		r.mu.Unlock()

		for _, line := range lines {
			var event map[string]any
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				continue
			}
			if event["action"] == action {
				return event
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	t.Fatalf("%q never reached the sink; it received %d lines", action, len(r.lines))
	return nil
}

func (r *receiver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.lines)
}

func readBodyOf(req *http.Request) string {
	defer func() { _ = req.Body.Close() }()

	body, _ := io.ReadAll(req.Body)
	return string(body)
}

// The setting existed since phase 6 and did nothing: there was no code that sent
// anything. This is the test that says it now does.
func TestAuditEventsReachTheConfiguredSink(t *testing.T) {
	sink := &receiver{}
	server := sink.start(t)

	h := signedInAdmin(t)

	saved := h.do(http.MethodPut, "/api/v1/settings/audit-sink", map[string]any{
		"enabled": true, "kind": "http", "url": server.URL,
	})
	if saved.StatusCode != http.StatusOK {
		t.Fatalf("save the sink: %d %s", saved.StatusCode, readBody(saved))
	}
	_ = saved.Body.Close()

	// Any audited act will do; a cluster registration is one with plenty of detail.
	id := registerCluster(t, h, "shipping-probe")

	event := sink.waitFor(t, "cluster.created")

	if event["result"] != "success" {
		t.Errorf("the shipped event says result=%v", event["result"])
	}
	if event["actor"] != "admin@example.com" {
		t.Errorf("the shipped event names actor %v", event["actor"])
	}
	if event["clusterId"] != id {
		t.Errorf("the shipped event names cluster %v, want %s", event["clusterId"], id)
	}
	if event["@timestamp"] == nil {
		t.Error("the shipped event has no timestamp")
	}
	if event["requestId"] == nil {
		t.Error("the shipped event has no request id, so it cannot be tied to a log line")
	}
}

// A sink that refuses everything must not stop Kubby recording, answering or working.
// The audit trail is the database and the log stream; the sink is a copy of it.
func TestABrokenSinkDoesNotStopAnything(t *testing.T) {
	sink := &receiver{fail: true}
	server := sink.start(t)

	h := signedInAdmin(t)

	saved := h.do(http.MethodPut, "/api/v1/settings/audit-sink", map[string]any{
		"enabled": true, "kind": "http", "url": server.URL,
	})
	if saved.StatusCode != http.StatusOK {
		t.Fatalf("save the sink: %d %s", saved.StatusCode, readBody(saved))
	}
	_ = saved.Body.Close()

	// Everything still works while the sink is refusing.
	id := registerCluster(t, h, "broken-sink-probe")

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a failing sink broke an ordinary request: %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// And the record is in the database regardless.
	audits := h.do(http.MethodGet, "/api/v1/audit", nil)
	body := readBody(audits)
	_ = audits.Body.Close()

	if !strings.Contains(body, "cluster.created") {
		t.Fatal("the event was not recorded while the sink was failing")
	}
}

// Saving the setting reconfigures the running shipper. Requiring a restart would mean an
// admin changing where the audit trail goes has no way to know it took effect.
func TestChangingTheSinkTakesEffectWithoutARestart(t *testing.T) {
	first := &receiver{}
	firstServer := first.start(t)
	second := &receiver{}
	secondServer := second.start(t)

	h := signedInAdmin(t)

	saved := h.do(http.MethodPut, "/api/v1/settings/audit-sink", map[string]any{
		"enabled": true, "kind": "http", "url": firstServer.URL,
	})
	_ = saved.Body.Close()

	registerCluster(t, h, "sink-one")
	first.waitFor(t, "cluster.created")

	// Point it somewhere else.
	moved := h.do(http.MethodPut, "/api/v1/settings/audit-sink", map[string]any{
		"enabled": true, "kind": "http", "url": secondServer.URL,
	})
	if moved.StatusCode != http.StatusOK {
		t.Fatalf("move the sink: %d %s", moved.StatusCode, readBody(moved))
	}
	_ = moved.Body.Close()

	before := first.count()
	registerCluster(t, h, "sink-two")
	second.waitFor(t, "cluster.created")

	if after := first.count(); after > before+1 {
		t.Errorf("the old sink kept receiving after the setting changed (%d → %d)", before, after)
	}
}

// A kind with no sender behind it must be refused at the point of saving, or the screen
// says shipping is on while nothing leaves the process.
func TestASinkWithNoSenderIsRefused(t *testing.T) {
	h := signedInAdmin(t)

	resp := h.do(http.MethodPut, "/api/v1/settings/audit-sink", map[string]any{
		"enabled": true, "kind": "syslog", "url": "http://localhost:514",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("a sink type nothing can send to was accepted")
	}
}
