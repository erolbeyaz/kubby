package cluster_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/crypto"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

func serviceHarness(t *testing.T) (*cluster.Service, *store.DB, uuid.UUID) {
	t.Helper()

	dsn := os.Getenv("KUBBY_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("KUBBY_TEST_DB_DSN is not set; skipping cluster service tests")
	}

	db, err := store.OpenDSN(context.Background(), dsn, 5)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(db.Close)

	keyring, err := crypto.NewKeyring(1, bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	svc := cluster.NewService(db, keyring, cluster.Settings{
		DefaultQPS: 20, DefaultBurst: 40, Timeout: 10 * time.Second, AllowLoopback: true,
	})

	hash, _ := hashForTest(t)
	owner, err := db.Users().Create(context.Background(),
		"cluster-owner-"+uuid.NewString()[:8]+"@example.com", "Owner", hash, rbac.RoleAdmin)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM users WHERE id = $1`, owner.ID)
	})
	return svc, db, owner.ID
}

func TestServiceValidatesBeforeStoringAnything(t *testing.T) {
	svc, db, _ := serviceHarness(t)
	raw := liveKubeconfig(t)
	ctx := context.Background()

	before := countClusters(t, db)

	result, err := svc.Validate(ctx, raw, "")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if len(result.Contexts) == 0 {
		t.Fatal("no contexts were reported")
	}
	if result.Probe == nil {
		t.Fatal("the selected context was not probed; the user would save blind")
	}
	if result.Probe.Status != cluster.StatusValid {
		t.Errorf("probe status = %q (%s)", result.Probe.Status, result.Probe.Detail)
	}
	if len(result.Probe.Permissions) == 0 {
		t.Error("no permissions were reported before saving")
	}

	if after := countClusters(t, db); after != before {
		t.Errorf("validation wrote %d cluster rows; it must store nothing", after-before)
	}
}

func TestServiceStoresKubeconfigEncrypted(t *testing.T) {
	svc, db, owner := serviceHarness(t)
	raw := liveKubeconfig(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, cluster.CreateInput{
		Name:        "enc-check-" + uuid.NewString()[:8],
		Environment: "test",
		Kubeconfig:  raw,
		CreatedBy:   owner,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM clusters WHERE id = $1`, created.ID)
	})

	// The probe ran as part of creation, so the cluster already knows what it is.
	if created.CredentialStatus != store.CredentialValid {
		t.Errorf("CredentialStatus = %q (%s), want valid", created.CredentialStatus, created.StatusDetail)
	}
	if created.K8sVersion == "" || created.NodeCount == nil {
		t.Errorf("health was not recorded: version=%q nodes=%v", created.K8sVersion, created.NodeCount)
	}

	t.Run("no plaintext reaches the database", func(t *testing.T) {
		var stored []byte
		err := db.Pool().QueryRow(ctx,
			`SELECT kubeconfig_enc FROM cluster_credentials WHERE cluster_id = $1`, created.ID).Scan(&stored)
		if err != nil {
			t.Fatalf("read credential: %v", err)
		}

		if bytes.Contains(stored, []byte("apiVersion")) || bytes.Contains(stored, []byte("kind: Config")) {
			t.Fatal("the stored credential contains kubeconfig plaintext")
		}
		for _, marker := range [][]byte{[]byte("token:"), []byte("certificate-authority-data")} {
			if bytes.Contains(stored, marker) {
				t.Fatalf("the stored credential leaks %q", marker)
			}
		}
	})

	t.Run("the credential still works after a round trip", func(t *testing.T) {
		cfg, err := svc.RESTConfigFor(ctx, created, nil)
		if err != nil {
			t.Fatalf("RESTConfigFor: %v", err)
		}
		if health := cluster.Probe(ctx, cfg, 10*time.Second); health.Status != cluster.StatusValid {
			t.Errorf("decrypted credential does not work: %s (%s)", health.Status, health.Detail)
		}
	})
}

// A ciphertext is bound to its row, so moving it onto another cluster must fail rather
// than silently granting access with someone else's credential.
func TestStoredCredentialIsBoundToItsCluster(t *testing.T) {
	svc, db, owner := serviceHarness(t)
	raw := liveKubeconfig(t)
	ctx := context.Background()

	first, err := svc.Create(ctx, cluster.CreateInput{
		Name: "bind-a-" + uuid.NewString()[:8], Environment: "test", Kubeconfig: raw, CreatedBy: owner,
	})
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := svc.Create(ctx, cluster.CreateInput{
		Name: "bind-b-" + uuid.NewString()[:8], Environment: "test", Kubeconfig: raw, CreatedBy: owner,
	})
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(),
			`DELETE FROM clusters WHERE id = ANY($1)`, []uuid.UUID{first.ID, second.ID})
	})

	// Copy the first cluster's ciphertext onto the second row.
	_, err = db.Pool().Exec(ctx, `
		UPDATE cluster_credentials
		SET kubeconfig_enc = (SELECT kubeconfig_enc FROM cluster_credentials WHERE cluster_id = $1)
		WHERE cluster_id = $2`, first.ID, second.ID)
	if err != nil {
		t.Fatalf("transplant credential: %v", err)
	}

	if _, err := svc.RESTConfigFor(ctx, second, nil); err == nil {
		t.Fatal("a credential transplanted from another cluster decrypted successfully")
	}
}

func countClusters(t *testing.T, db *store.DB) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(context.Background(), `SELECT count(*) FROM clusters`).Scan(&n); err != nil {
		t.Fatalf("count clusters: %v", err)
	}
	return n
}
