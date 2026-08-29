package logsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// bucket builds one pod's aggregation result the way Elasticsearch returns it.
type bucket struct {
	pod, namespace, container, message string
	count                              int64
	slices                             int
	matched                            map[string]int64
	firstSeen, lastSeen                time.Time
}

func (b bucket) encode() map[string]any {
	slices := make([]any, b.slices)
	for i := range slices {
		slices[i] = map[string]any{"doc_count": 1}
	}

	rules := map[string]any{}
	for name, count := range b.matched {
		rules[name] = map[string]any{"doc_count": count}
	}

	return map[string]any{
		"key":        b.pod,
		"doc_count":  b.count,
		"namespace":  map[string]any{"buckets": []any{map[string]any{"key": b.namespace}}},
		"container":  map[string]any{"buckets": []any{map[string]any{"key": b.container}}},
		"first_seen": map[string]any{"value": float64(b.firstSeen.UnixMilli())},
		"last_seen":  map[string]any{"value": float64(b.lastSeen.UnixMilli())},
		"slices":     map[string]any{"buckets": slices},
		"rules":      map[string]any{"buckets": rules},
		"sample": map[string]any{"hits": map[string]any{
			"hits": []any{map[string]any{"_source": map[string]any{"log": b.message}}},
		}},
	}
}

func sweepAgainst(t *testing.T, buckets ...bucket) ([]Finding, map[string]any) {
	t.Helper()

	var sent map[string]any
	encoded := make([]any, 0, len(buckets))
	for _, b := range buckets {
		encoded = append(encoded, b.encode())
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"aggregations": map[string]any{"pods": map[string]any{"buckets": encoded}},
		})
	}))
	t.Cleanup(server.Close)

	client, err := New(Config{URL: server.URL, Index: "logs-*"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	findings, err := client.Sweep(context.Background(), DefaultRules(), 15*time.Minute)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	return findings, sent
}

func TestSweepTurnsABucketIntoAFinding(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)

	findings, _ := sweepAgainst(t, bucket{
		pod: "nx-fxrateengineapi-ff6b97dfd-8mlwz", namespace: "nx-apps",
		container: "nx-fxrateengineapi", count: 456672, slices: 3,
		matched:   map[string]int64{"SQL Server": 456672, "Application error": 456672},
		firstSeen: now.Add(-15 * time.Minute), lastSeen: now,
		message: `Login failed for user 'netixuser'. Detail:System.Data.SqlClient.SqlException (0x80131904): Cannot open database "NetTrexCommon" requested by the login.`,
	})

	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	f := findings[0]

	if f.Pod != "nx-fxrateengineapi-ff6b97dfd-8mlwz" || f.Namespace != "nx-apps" {
		t.Errorf("finding identifies %s/%s", f.Namespace, f.Pod)
	}
	// The specific rule, not the net that also matched: "Application error" is a worse
	// answer than "SQL Server" for the same lines.
	if f.Rule != "SQL Server" || f.Class != ClassAuth {
		t.Errorf("rule = %q class = %q, want SQL Server / auth", f.Rule, f.Class)
	}
	if f.Count != 456672 {
		t.Errorf("count = %d", f.Count)
	}
	// Failing in every slice of the window is an outage that is still going.
	if f.Severity != SeverityError {
		t.Errorf("severity = %q, want error", f.Severity)
	}
	// The identity of what failed, which is what sends the reader to the right place.
	for _, want := range []string{"database NetTrexCommon", "user netixuser"} {
		if !strings.Contains(f.Summary, want) {
			t.Errorf("summary %q does not mention %q", f.Summary, want)
		}
	}
	if f.FirstSeen.IsZero() || f.LastSeen.IsZero() {
		t.Errorf("finding has no window: %v … %v", f.FirstSeen, f.LastSeen)
	}
}

// A pod that stumbled once and a pod that has been failing for the whole window are
// both worth showing, and calling them the same thing is how a mark stops meaning
// anything.
func TestSeverityFollowsHowMuchOfTheWindowIsFailing(t *testing.T) {
	now := time.Now().UTC()
	base := bucket{
		pod: "p", namespace: "n", count: 3,
		matched: map[string]int64{"Connection refused": 3}, firstSeen: now, lastSeen: now,
		message: "Error: connect ECONNREFUSED 10.43.248.218:11111",
	}

	one := base
	one.slices = 1
	all := base
	all.slices = 3

	if got, _ := sweepAgainst(t, one); got[0].Severity != SeverityWarning {
		t.Errorf("a single slice gave %q, want warning", got[0].Severity)
	}
	if got, _ := sweepAgainst(t, all); got[0].Severity != SeverityError {
		t.Errorf("every slice gave %q, want error", got[0].Severity)
	}
}

func TestSweepPullsTheAddressOutOfARefusedConnection(t *testing.T) {
	now := time.Now().UTC()
	findings, _ := sweepAgainst(t, bucket{
		pod: "fi-n8n-worker-845d5696df-9rtdf", namespace: "data", count: 23, slices: 3,
		matched: map[string]int64{"Connection refused": 23}, firstSeen: now, lastSeen: now,
		message: "Error: connect ECONNREFUSED 10.43.248.218:11111",
	})

	if got := findings[0].Summary; got != "address 10.43.248.218:11111" {
		t.Errorf("summary = %q", got)
	}
	if findings[0].Class != ClassUnreachable {
		t.Errorf("class = %q, want unreachable", findings[0].Class)
	}
}

// A Java stack trace arrives as one document with thirty lines in it, twenty-nine of
// which are frames. The frames say where, not what.
func TestSweepKeepsOnlyTheFirstLineOfAStackTrace(t *testing.T) {
	now := time.Now().UTC()
	trace := "org.hibernate.exception.JDBCConnectionException: Unable to acquire JDBC Connection\n" +
		"    at org.springframework.orm.hibernate5.HibernateTransactionManager.doBegin(HibernateTransactionManager.java:542)\n" +
		"    at java.lang.Thread.run(Thread.java:750)"

	findings, _ := sweepAgainst(t, bucket{
		pod: "emktgonder-1", namespace: "kasbox", count: 4, slices: 2,
		matched: map[string]int64{"JDBC connection pool": 4}, firstSeen: now, lastSeen: now,
		message: trace,
	})

	if strings.Contains(findings[0].Sample, "at java.lang.Thread.run") {
		t.Errorf("the sample carries stack frames: %q", findings[0].Sample)
	}
	if !strings.Contains(findings[0].Sample, "Unable to acquire JDBC Connection") {
		t.Errorf("the sample lost the message: %q", findings[0].Sample)
	}
}

func TestSweepRedactsTheLineItKeeps(t *testing.T) {
	now := time.Now().UTC()
	findings, _ := sweepAgainst(t, bucket{
		pod: "p", namespace: "n", count: 9, slices: 1,
		matched: map[string]int64{"Application error": 9}, firstSeen: now, lastSeen: now,
		message: "FATAL: connect failed Server=db;User Id=app;Password=hunter2;Encrypt=true",
	})

	if strings.Contains(findings[0].Sample, "hunter2") {
		t.Errorf("the sample carries a password: %q", findings[0].Sample)
	}
}

// One request for the whole cluster, whatever the pod count: the grouping happens where
// the data already is.
func TestSweepAsksOneQuestionAndGroupsItByPod(t *testing.T) {
	now := time.Now().UTC()
	_, sent := sweepAgainst(t, bucket{
		pod: "p", namespace: "n", count: 1, slices: 1,
		matched: map[string]int64{"Application error": 1}, firstSeen: now, lastSeen: now,
	})

	encoded, _ := json.Marshal(sent)
	body := string(encoded)

	if !strings.Contains(body, `"field":"kubernetes.pod_name"`) {
		t.Errorf("the query does not group by pod: %s", body)
	}
	if !strings.Contains(body, `"gte":"now-15m"`) {
		t.Errorf("the query does not carry the window: %s", body)
	}
	// size 0: the lines themselves are never fetched.
	if !strings.Contains(body, `"size":0`) {
		t.Errorf("the query asks for documents: %s", body)
	}
	if !strings.Contains(body, "date_histogram") {
		t.Errorf("the query cannot tell a continuing failure from a stumble: %s", body)
	}
}

func TestSweepWithNoRulesAsksNothing(t *testing.T) {
	client, _ := New(Config{URL: "http://127.0.0.1:1", Index: "logs-*"})

	findings, err := client.Sweep(context.Background(), nil, time.Minute)
	if err != nil || findings != nil {
		t.Errorf("Sweep with no rules returned %v, %v", findings, err)
	}
}

func TestFieldNamesFallBackToFluentBitsSpelling(t *testing.T) {
	fields := Fields{Message: "message"}.withDefaults()

	if fields.Message != "message" {
		t.Errorf("an explicit message field was overwritten: %q", fields.Message)
	}
	if fields.Pod != "kubernetes.pod_name" || fields.Timestamp != "@timestamp" {
		t.Errorf("defaults did not fill in: %+v", fields)
	}
}

// A retry that succeeded on the second attempt logged one failure. Reporting that is
// how a list fills with marks nobody reads.
func TestASingleStumbleIsNotAFinding(t *testing.T) {
	now := time.Now().UTC()
	base := bucket{
		pod: "p", namespace: "n", slices: 1,
		firstSeen: now, lastSeen: now,
		message: "Error: connect ECONNREFUSED 10.43.248.218:11111",
	}

	once := base
	once.count, once.matched = 1, map[string]int64{"Connection refused": 1}
	if got, _ := sweepAgainst(t, once); len(got) != 0 {
		t.Errorf("one line produced %d findings", len(got))
	}

	repeated := base
	repeated.count, repeated.matched = 3, map[string]int64{"Connection refused": 3}
	if got, _ := sweepAgainst(t, repeated); len(got) != 1 {
		t.Errorf("a repeated failure produced %d findings, want 1", len(got))
	}
}
