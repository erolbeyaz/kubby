package cluster_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/store"
)

// A credential that stops working must be discovered before someone opens the cluster,
// not at the moment they need it (ADR-018).
func TestMonitorDetectsAndRecordsABrokenCredential(t *testing.T) {
	svc, db, owner := serviceHarness(t)
	raw := liveKubeconfig(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, cluster.CreateInput{
		Name: "monitored-" + uuid.NewString()[:8], Environment: "test",
		Kubeconfig: raw, CreatedBy: owner,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM clusters WHERE id = $1`, created.ID)
	})
	if created.CredentialStatus != store.CredentialValid {
		t.Fatalf("cluster did not start valid: %s", created.StatusDetail)
	}

	// Break the stored credential the way an expiry or revocation would.
	broken := replaceToken(t, raw, "")
	if err := svc.ReplaceCredential(ctx, created.ID, broken, ""); err != nil {
		t.Fatalf("ReplaceCredential: %v", err)
	}
	if _, err := db.Pool().Exec(ctx,
		`UPDATE clusters SET credential_status = 'valid', status_detail = '' WHERE id = $1`,
		created.ID); err != nil {
		t.Fatalf("reset status: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	monitor := cluster.NewMonitor(svc, db, audit.New(db.Audit(), logger), logger, 50*time.Millisecond)

	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		monitor.Run(runCtx)
	}()

	deadline := time.After(3 * time.Second)
	for {
		reloaded, err := db.Clusters().ByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		if reloaded.CredentialStatus == store.CredentialInvalid {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("the monitor never noticed the broken credential (status %q)", reloaded.CredentialStatus)
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the monitor did not stop when its context was cancelled")
	}

	t.Run("the change is recorded in the audit trail", func(t *testing.T) {
		events, err := db.Audit().List(ctx, store.AuditFilter{Action: audit.ActionClusterCredentialInvalid})
		if err != nil {
			t.Fatalf("List audit: %v", err)
		}
		for _, e := range events {
			if e.ClusterID != nil && *e.ClusterID == created.ID {
				return
			}
		}
		t.Error("no audit event was written when the credential became invalid")
	})
}

// A disabled interval must not spin: the monitor should return immediately.
func TestMonitorIsDisabledByAZeroInterval(t *testing.T) {
	svc, db, _ := serviceHarness(t)

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	monitor := cluster.NewMonitor(svc, db, audit.New(db.Audit(), logger), logger, 0)

	done := make(chan struct{})
	go func() {
		defer close(done)
		monitor.Run(context.Background())
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the monitor kept running with a zero interval")
	}
}
