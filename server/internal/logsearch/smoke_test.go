package logsearch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/erolbeyaz/kubby/internal/logsearch"
)

// The smoke test seeds a store of its own and reads it back.
//
// It runs twice, against an index whose message field is `text` and one where it is
// `keyword`, because that difference is what made the first version of this feature
// return nothing at all from a store full of matching lines — with no error on either
// side. A stub cannot catch that; only Elasticsearch deciding for itself can.
func TestSmokeSweepAgainstBothFieldMappings(t *testing.T) {
	address := liveStore(t)

	for _, mapping := range []string{"text", "keyword"} {
		t.Run(mapping, func(t *testing.T) {
			index := seedSmokeIndex(t, address, mapping)

			client, err := logsearch.New(logsearch.Config{URL: address, Index: index})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			findings, err := client.Sweep(context.Background(), logsearch.DefaultRules(),
				logsearch.SweepOptions{Window: 15 * time.Minute, MinCount: 3})
			if err != nil {
				t.Fatalf("Sweep: %v", err)
			}

			byPod := map[string]logsearch.Finding{}
			for _, f := range findings {
				byPod[f.Pod] = f
			}
			for _, f := range findings {
				t.Logf("  %-16s %-22s %-8s %3d  %s", f.Pod, f.Rule, f.Severity, f.Count, f.Summary)
			}

			// Failing through every slice of the window, and named by the rule that
			// explains it rather than by the generic net that also matched.
			sql, ok := byPod["sql-pod"]
			if !ok {
				t.Fatalf("the pod that cannot open its database was not found; got %v", keys(byPod))
			}
			if sql.Rule != "SQL Server" || sql.Class != logsearch.ClassAuth {
				t.Errorf("sql-pod = %q / %q", sql.Rule, sql.Class)
			}
			if sql.Severity != logsearch.SeverityError {
				t.Errorf("sql-pod severity = %q, want error", sql.Severity)
			}
			for _, want := range []string{"database Orders", "user svc-orders"} {
				if !strings.Contains(sql.Summary, want) {
					t.Errorf("sql-pod summary %q does not mention %q", sql.Summary, want)
				}
			}

			// A whole stack trace in one document keeps only the line that says what.
			java, ok := byPod["java-pod"]
			if !ok {
				t.Fatal("the pod whose connection pool is empty was not found")
			}
			if strings.Contains(java.Sample, "at java.lang.Thread.run") {
				t.Errorf("java-pod sample carries frames: %q", java.Sample)
			}

			if refused := byPod["node-pod"]; !strings.Contains(refused.Summary, "10.43.248.218:11111") {
				t.Errorf("node-pod summary = %q", refused.Summary)
			}

			// The two that must not appear: one healthy, one that stumbled once.
			for _, quiet := range []string{"healthy-pod", "flaky-pod"} {
				if found, ok := byPod[quiet]; ok {
					t.Errorf("%s was reported: %q", quiet, found.Sample)
				}
			}
		})
	}
}

func keys(m map[string]logsearch.Finding) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// seedSmokeIndex writes a small cluster's worth of log lines and returns the index name.
func seedSmokeIndex(t *testing.T, address, messageType string) string {
	t.Helper()

	index := "kubby-smoke-logs-" + messageType
	drop(t, address, index)

	// An explicit mapping, because the whole point is which one is in force.
	create(t, address, index, fmt.Sprintf(`{"mappings":{"properties":{
		"@timestamp":{"type":"date"},
		"log":{"type":%q},
		"kubernetes":{"properties":{
			"pod_name":{"type":"keyword"},
			"namespace_name":{"type":"keyword"},
			"container_name":{"type":"keyword"}}}}}}`, messageType))
	t.Cleanup(func() { drop(t, address, index) })

	now := time.Now().UTC()
	// Spread across the window's three slices, so "failing continuously" is a fact
	// about the data rather than an assumption in the test.
	slices := []time.Duration{-1 * time.Minute, -6 * time.Minute, -11 * time.Minute}

	var body bytes.Buffer
	add := func(pod, message string, at time.Time) {
		fmt.Fprintf(&body, "%s\n", `{"index":{}}`)
		doc, _ := json.Marshal(map[string]any{
			"@timestamp": at.Format(time.RFC3339Nano),
			"log":        message,
			"kubernetes": map[string]any{
				"pod_name": pod, "namespace_name": "smoke", "container_name": pod,
			},
		})
		body.Write(doc)
		body.WriteByte('\n')
	}

	for _, offset := range slices {
		at := now.Add(offset)
		add("sql-pod", `Login failed for user 'svc-orders'. System.Data.SqlClient.SqlException (0x80131904): Cannot open database "Orders" requested by the login.`, at)
		add("java-pod", "org.hibernate.exception.JDBCConnectionException: Unable to acquire JDBC Connection\n    at com.zaxxer.hikari.pool.HikariPool.createPoolEntry(HikariPool.java:430)\n    at java.lang.Thread.run(Thread.java:750)", at)
		add("node-pod", "Error: connect ECONNREFUSED 10.43.248.218:11111", at)
		add("healthy-pod", "GET /api/v1/orders/4821 200 in 14ms", at)
	}
	// Below the threshold on purpose: one failure that was retried is not an outage.
	add("flaky-pod", "Error: connect ECONNREFUSED 10.43.248.218:11111", now.Add(-2*time.Minute))

	post(t, address, "/"+index+"/_bulk?refresh=wait_for", body.String(), "application/x-ndjson")
	return index
}

func create(t *testing.T, address, index, body string) {
	t.Helper()
	post(t, address, "/"+index, body, "application/json")
}

func drop(t *testing.T, address, index string) {
	t.Helper()
	request, _ := http.NewRequest(http.MethodDelete, address+"/"+index, nil)
	response, err := http.DefaultClient.Do(request)
	if err == nil {
		_ = response.Body.Close()
	}
}

func post(t *testing.T, address, path, body, contentType string) {
	t.Helper()

	method := http.MethodPost
	if !strings.Contains(path, "_bulk") {
		method = http.MethodPut
	}
	request, err := http.NewRequest(method, address+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	request.Header.Set("Content-Type", contentType)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode >= 300 {
		var out bytes.Buffer
		_, _ = out.ReadFrom(response.Body)
		t.Fatalf("%s %s: %d %s", method, path, response.StatusCode, out.String())
	}
}
