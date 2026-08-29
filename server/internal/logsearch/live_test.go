package logsearch_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/erolbeyaz/kubby/internal/logsearch"
)

// Live tests need a real Elasticsearch. `make compose-up` with the elastic profile
// starts one; point this at it to enable them:
//
//	KUBBY_TEST_ELASTICSEARCH=http://localhost:9200 go test ./internal/logsearch/
//
// A stub can be made to answer anything. Elasticsearch decides for itself what a data
// stream is, what `_resolve/index` returns for a wildcard and how `track_total_hits`
// behaves — and those are exactly the three things this package depends on.
func liveStore(t *testing.T) string {
	t.Helper()

	address := os.Getenv("KUBBY_TEST_ELASTICSEARCH")
	if address == "" {
		t.Skip("KUBBY_TEST_ELASTICSEARCH is not set; skipping live log store tests")
	}
	return address
}

func TestProbeAgainstARealStore(t *testing.T) {
	address := liveStore(t)

	client, err := logsearch.New(logsearch.Config{URL: address, Index: "logs-kubbydev-app-*"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	probe, err := client.Probe(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	if probe.Version == "" {
		t.Error("the store answered without naming its version")
	}
	if probe.Indices == 0 {
		t.Fatal("the pattern resolved to nothing; seed a document first")
	}
	if probe.Documents == 0 {
		t.Error("the window holds no documents")
	}

	// The point of showing the document: the field names are the thing the operator has
	// to read off it.
	rendered, _ := json.Marshal(probe.Sample)
	for _, field := range []string{"kubernetes", "pod_name", "log"} {
		if !strings.Contains(string(rendered), field) {
			t.Errorf("the sample does not carry %q: %s", field, rendered)
		}
	}
}

// A pattern that matches nothing is the failure that looks like success, and only
// `_resolve/index` can tell it apart from a quiet cluster.
func TestProbeSeparatesATypoFromAQuietCluster(t *testing.T) {
	address := liveStore(t)

	client, err := logsearch.New(logsearch.Config{URL: address, Index: "logs-no-such-cluster-*"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	probe, err := client.Probe(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Indices != 0 {
		t.Errorf("a pattern matching nothing resolved to %d indices", probe.Indices)
	}
}

// The whole point, against a real store: one request, every pod that is complaining,
// grouped and summarised. A stub can be made to answer anything; Elasticsearch decides
// for itself how a filters aggregation nests inside a terms aggregation.
func TestSweepAgainstARealStore(t *testing.T) {
	address := liveStore(t)

	client, err := logsearch.New(logsearch.Config{URL: address, Index: "logs-kubbydev-app-*"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	started := time.Now()
	findings, err := client.Sweep(context.Background(), logsearch.DefaultRules(), 15*time.Minute)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	t.Logf("%d findings in %s", len(findings), time.Since(started).Round(time.Millisecond))

	if len(findings) == 0 {
		t.Fatal("nothing was found; seed the store first")
	}

	byPod := map[string]logsearch.Finding{}
	for _, f := range findings {
		byPod[f.Pod] = f
		t.Logf("  %-45s %-22s %-12s %6d  %s", f.Pod, f.Rule, f.Severity, f.Count, f.Summary)
	}

	// The control: a pod whose logs say nothing is wrong must not appear. A detector
	// that lights up every row has told the reader nothing.
	for pod := range byPod {
		if strings.HasPrefix(pod, "fi-customer-api") {
			t.Errorf("a healthy pod was reported: %s — %q", pod, byPod[pod].Sample)
		}
	}

	var sql, refused bool
	for _, f := range findings {
		if f.Rule == "SQL Server" && strings.Contains(f.Summary, "database NetTrexCommon") {
			sql = true
		}
		if f.Rule == "Connection refused" && strings.Contains(f.Summary, "10.43.248.218:11111") {
			refused = true
		}
	}
	if !sql {
		t.Error("the SQL Server rule did not identify the database it could not open")
	}
	if !refused {
		t.Error("the connection-refused rule did not identify the address")
	}
}
