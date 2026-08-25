package httpapi_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

type streamChange struct {
	Type      string `json:"type"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
	Row       *struct {
		Name   string            `json:"name"`
		Fields map[string]string `json:"fields"`
	} `json:"row"`
}

// readChanges opens the stream and returns what arrives within the deadline.
func readChanges(t *testing.T, h *harness, path string, want int, timeout time.Duration) []streamChange {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.server.URL+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream returned %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content type = %q, want an event stream", got)
	}

	var out []streamChange
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() && len(out) < want {
		line := scanner.Text()
		payload, found := strings.CutPrefix(line, "data: ")
		if !found {
			continue
		}

		var change streamChange
		if err := json.Unmarshal([]byte(payload), &change); err != nil {
			t.Fatalf("stream sent something that is not a change: %s", payload)
		}
		out = append(out, change)
	}
	return out
}

// A client that receives a projection on first load and a raw object on every update
// would need two readers for the same thing, and the second would drift (ADR-004).
func TestStreamSendsProjectedRows(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "stream-rows")

	changes := readChanges(t, h,
		"/api/v1/clusters/"+id+"/stream/pods?namespace=payments", 3, 20*time.Second)

	if len(changes) == 0 {
		t.Fatal("the stream sent nothing")
	}
	for _, change := range changes {
		if change.Type == "reset" {
			continue
		}
		if change.Row == nil {
			t.Fatalf("change %+v carries no row", change)
		}
		// The same fields the list projects, so one reader serves both.
		if change.Row.Fields["status"] == "" {
			t.Errorf("row %q has no status; the stream is not projecting", change.Row.Name)
		}
	}
}

// A streamed row carries the same measurements the list attaches.
//
// Without this the figures blink out: a watch event replaces the row with one that has no
// CPU or memory, and the column shows nothing until the next full refetch — which is what
// made the numbers appear to come and go.
func TestStreamedRowsKeepTheirUsageFigures(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "stream-usage")

	// Only meaningful where something is measuring; a cluster without metrics-server
	// legitimately has nothing to attach.
	list := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/pods?namespace=payments", nil)
	body := decode[listBody](t, list)
	_ = list.Body.Close()

	measured := false
	for _, row := range body.Rows {
		if cpu := row.Fields["cpu"]; cpu != "" && cpu != "—" {
			measured = true
			break
		}
	}
	if !measured {
		t.Skip("this cluster reports no pod usage, so there is nothing to keep")
	}

	changes := readChanges(t, h,
		"/api/v1/clusters/"+id+"/stream/pods?namespace=payments", 3, 30*time.Second)

	for _, change := range changes {
		if change.Type == "reset" || change.Row == nil {
			continue
		}
		if _, ok := change.Row.Fields["cpu"]; !ok {
			t.Fatalf("streamed row %q dropped its cpu field", change.Row.Name)
		}
		if _, ok := change.Row.Fields["memory"]; !ok {
			t.Fatalf("streamed row %q dropped its memory field", change.Row.Name)
		}
	}
}

func TestStreamRefusesAnUnknownKind(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "stream-unknown")

	resp := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/stream/nonsense", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("returned %d, want 404", resp.StatusCode)
	}
}

// Following a pod to what created it is the first question asked of a misbehaving one,
// and doing it by hand is two lookups in opposite directions.
func TestRelationsWalkTheOwnerChain(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "relations")

	pods := h.do(http.MethodGet, "/api/v1/clusters/"+id+"/resources/pods?namespace=payments", nil)
	rows := decode[listBody](t, pods).Rows
	_ = pods.Body.Close()

	var pod string
	for _, row := range rows {
		if strings.HasPrefix(row.Name, "payments-api-") {
			pod = row.Name
			break
		}
	}
	if pod == "" {
		t.Skip("no deployment-owned pod in the payments namespace")
	}

	resp := h.do(http.MethodGet,
		"/api/v1/clusters/"+id+"/relations/pods?namespace=payments&name="+pod, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("relations returned %d %s", resp.StatusCode, readBody(resp))
	}
	body := decode[struct {
		Relations []struct {
			Direction string `json:"direction"`
			Kind      string `json:"kind"`
			Name      string `json:"name"`
		} `json:"relations"`
	}](t, resp)

	kinds := map[string]bool{}
	for _, relation := range body.Relations {
		if relation.Direction == "owner" {
			kinds[relation.Kind] = true
		}
	}
	// Both steps, not only the first: a pod's ReplicaSet is rarely the thing to look at.
	if !kinds["ReplicaSet"] || !kinds["Deployment"] {
		t.Fatalf("owner chain stopped early: %+v", body.Relations)
	}
}

// A Service whose selector matches nothing is a common and quiet outage.
func TestRelationsReportAServiceThatSelectsNothing(t *testing.T) {
	h := signedInAdmin(t)
	id := registerCluster(t, h, "relations-service")

	created := h.do(http.MethodPost, "/api/v1/clusters/"+id+"/apply", map[string]any{
		"dryRun": false,
		"manifest": `apiVersion: v1
kind: Service
metadata:
  name: kubby-orphan-service
  namespace: payments
spec:
  selector:
    app: nothing-has-this-label
  ports:
    - port: 80
`,
	})
	if created.StatusCode != http.StatusOK {
		t.Fatalf("create service: %d %s", created.StatusCode, readBody(created))
	}
	_ = created.Body.Close()

	t.Cleanup(func() {
		resp := h.do(http.MethodDelete,
			"/api/v1/clusters/"+id+"/object/services?namespace=payments&name=kubby-orphan-service", nil)
		_ = resp.Body.Close()
	})

	resp := h.do(http.MethodGet,
		"/api/v1/clusters/"+id+"/relations/services?namespace=payments&name=kubby-orphan-service", nil)
	defer func() { _ = resp.Body.Close() }()

	body := decode[struct {
		Relations []struct {
			Direction string `json:"direction"`
			Detail    string `json:"detail"`
			Severity  string `json:"severity"`
		} `json:"relations"`
	}](t, resp)

	var warned bool
	for _, relation := range body.Relations {
		if relation.Direction == "serves" && relation.Severity == "error" {
			warned = true
			if !strings.Contains(relation.Detail, "matches nothing") {
				t.Errorf("the warning does not say what is wrong: %q", relation.Detail)
			}
		}
	}
	if !warned {
		t.Fatalf("a service selecting nothing was reported as fine: %+v", body.Relations)
	}
}
