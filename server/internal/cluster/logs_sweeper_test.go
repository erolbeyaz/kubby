package cluster_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/logsearch"
	"github.com/erolbeyaz/kubby/internal/store"
)

// sweeperHarness registers a cluster pointed at a log source and returns a sweeper for it.
func sweeperHarness(t *testing.T, logsURL, index string) (*cluster.LogSweeper, *storeCluster) {
	t.Helper()

	svc, db, owner := serviceHarness(t)
	created, err := svc.Create(context.Background(), cluster.CreateInput{
		Name: "logs-" + uuid.NewString()[:8], Environment: "test",
		Kubeconfig: liveKubeconfig(t), CreatedBy: owner,
	})
	if err != nil {
		t.Fatalf("Create cluster: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM clusters WHERE id = $1`, created.ID)
	})

	if err := db.Clusters().UpdateSettings(context.Background(), created.ID, store.ClusterSettings{
		LogsURL: &logsURL, LogsIndex: &index,
	}); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	analysis := func(context.Context) (logsearch.Fields, []logsearch.Rule, logsearch.SweepOptions, error) {
		return logsearch.DefaultFields(), logsearch.DefaultRules(),
			logsearch.SweepOptions{Window: 15 * time.Minute}, nil
	}
	return cluster.NewLogSweeper(svc, db, logger, time.Minute, analysis), created
}

// sweepOnce runs the sweeper's loop long enough for its first pass and stops it.
func sweepOnce(t *testing.T, sweeper *cluster.LogSweeper) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		sweeper.Run(ctx)
	}()

	// Run sweeps before it waits on the ticker, so the first pass has happened by the
	// time cancellation is seen.
	time.Sleep(2 * time.Second)
	cancel()
	<-done
}

// The mistake this exists to prevent: a log store nobody can reach rendering as a
// cluster with nothing wrong in it. It was made once already with Prometheus (ADR-111),
// and the shape of it here is identical.
func TestAnUnreachableLogStoreIsUnknownAndNeverClean(t *testing.T) {
	if os.Getenv("KUBBY_TEST_KUBECONFIG") == "" {
		t.Skip("KUBBY_TEST_KUBECONFIG is not set")
	}
	// A port with nothing behind it.
	sweeper, c := sweeperHarness(t, "http://127.0.0.1:1", "logs-*")
	sweepOnce(t, sweeper)

	found := sweeper.Findings(c.ID.String())
	if found.State != cluster.LogsStateUnknown {
		t.Errorf("state = %q, want unknown", found.State)
	}
	if found.Detail == "" {
		t.Error("an unreachable store gave no reason")
	}
	if len(found.Findings) != 0 {
		t.Errorf("an unreachable store produced %d findings", len(found.Findings))
	}
}

// No source configured is an ordinary state and must be told apart from a broken one:
// one of them is somebody's to fix.
func TestAClusterWithNoLogSourceIsOffNotUnknown(t *testing.T) {
	if os.Getenv("KUBBY_TEST_KUBECONFIG") == "" {
		t.Skip("KUBBY_TEST_KUBECONFIG is not set")
	}
	sweeper, c := sweeperHarness(t, "", "")
	sweepOnce(t, sweeper)

	if got := sweeper.Findings(c.ID.String()).State; got != cluster.LogsStateOff {
		t.Errorf("state = %q, want off", got)
	}
}

// A cluster the sweeper has not reached yet is unknown, not empty: "we have not looked"
// and "we looked and found nothing" are different answers.
func TestAnUnsweptClusterIsUnknown(t *testing.T) {
	if os.Getenv("KUBBY_TEST_KUBECONFIG") == "" {
		t.Skip("KUBBY_TEST_KUBECONFIG is not set")
	}
	sweeper, _ := sweeperHarness(t, "", "")

	found := sweeper.Findings(uuid.NewString())
	if found.State != cluster.LogsStateUnknown {
		t.Errorf("an unswept cluster reported %q", found.State)
	}
}

func TestSweepJoinsFindingsToPodsAgainstARealStore(t *testing.T) {
	address := os.Getenv("KUBBY_TEST_ELASTICSEARCH")
	if address == "" || os.Getenv("KUBBY_TEST_KUBECONFIG") == "" {
		t.Skip("KUBBY_TEST_ELASTICSEARCH and KUBBY_TEST_KUBECONFIG are needed")
	}

	sweeper, c := sweeperHarness(t, address, "logs-kubbydev-app-*")
	sweepOnce(t, sweeper)

	found := sweeper.Findings(c.ID.String())
	if found.State != cluster.LogsStateOK {
		t.Fatalf("state = %q (%s), want ok", found.State, found.Detail)
	}
	if len(found.Findings) == 0 {
		t.Fatal("nothing was found; seed the store first")
	}

	// The lookup a list row does: namespace and pod name, no scan.
	first := found.Findings[0]
	if got := found.For(first.Namespace, first.Pod); got == nil || got.Pod != first.Pod {
		t.Errorf("For(%s/%s) did not find the finding it just reported", first.Namespace, first.Pod)
	}
	if found.For("nx-apps", "a-pod-that-does-not-exist") != nil {
		t.Error("a pod with no finding was given one")
	}
}

// The roll-up: a deployment whose pods are failing is where the reader goes, and three
// identical marks on three replicas is the same news three times.
func TestFindingsRollUpFromPodsToTheirWorkloads(t *testing.T) {
	address := os.Getenv("KUBBY_TEST_ELASTICSEARCH")
	if address == "" || os.Getenv("KUBBY_TEST_KUBECONFIG") == "" {
		t.Skip("KUBBY_TEST_ELASTICSEARCH and KUBBY_TEST_KUBECONFIG are needed")
	}

	sweeper, c := sweeperHarness(t, address, "logs-kubbydev-app-*")
	sweepOnce(t, sweeper)

	found := sweeper.Findings(c.ID.String())
	if found.State != cluster.LogsStateOK {
		t.Fatalf("state = %q (%s)", found.State, found.Detail)
	}

	// The three replicas of fi-n8n-worker all report the same refused connection.
	pods := make([]cluster.Row, 0, 3)
	for _, f := range found.Findings {
		if f.Namespace == "data" {
			pods = append(pods, cluster.Row{Namespace: f.Namespace, Name: f.Pod})
		}
	}
	if len(pods) < 2 {
		t.Skipf("only %d pods in the data namespace are reporting; nothing to roll up", len(pods))
	}

	rows := []cluster.Row{{Namespace: "data", Name: "fi-n8n-worker"}}
	sweeper.Attach(c.ID.String(), "Deployment", rows)

	rolled := rows[0].LogFinding
	if rolled == nil {
		t.Fatal("the deployment carries no finding, though its pods do")
	}
	if rolled.Pods != len(pods) {
		t.Errorf("rolled up %d pods, want %d", rolled.Pods, len(pods))
	}

	// Every pod's lines counted once, not one pod's shown for all of them.
	var total int64
	for _, f := range found.Findings {
		if f.Namespace == "data" {
			total += f.Count
		}
	}
	if rolled.Count != total {
		t.Errorf("rolled-up count = %d, want %d", rolled.Count, total)
	}
}

// A pod's own row carries its own finding, not the workload's aggregate.
func TestAttachGivesAPodItsOwnFinding(t *testing.T) {
	address := os.Getenv("KUBBY_TEST_ELASTICSEARCH")
	if address == "" || os.Getenv("KUBBY_TEST_KUBECONFIG") == "" {
		t.Skip("KUBBY_TEST_ELASTICSEARCH and KUBBY_TEST_KUBECONFIG are needed")
	}

	sweeper, c := sweeperHarness(t, address, "logs-kubbydev-app-*")
	sweepOnce(t, sweeper)

	found := sweeper.Findings(c.ID.String())
	if len(found.Findings) == 0 {
		t.Fatal("nothing was found; seed the store first")
	}
	first := found.Findings[0]

	rows := []cluster.Row{
		{Namespace: first.Namespace, Name: first.Pod},
		{Namespace: first.Namespace, Name: "a-pod-that-does-not-exist"},
	}
	sweeper.Attach(c.ID.String(), "Pod", rows)

	if rows[0].LogFinding == nil || rows[0].LogFinding.Pod != first.Pod {
		t.Errorf("the pod did not get its own finding")
	}
	if rows[0].LogFinding.Pods != 0 {
		t.Errorf("a pod's own finding claims to cover %d pods", rows[0].LogFinding.Pods)
	}
	if rows[1].LogFinding != nil {
		t.Error("a pod with no finding was given one")
	}
}
