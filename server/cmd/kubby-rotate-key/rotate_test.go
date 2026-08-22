package main

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

// Key rotation is a disaster-recovery path: if it does not actually work, discovering
// that during an incident is far too late. This exercises it against a real database.
func TestRotationRewrapsStoredSecretsAndKeepsThemReadable(t *testing.T) {
	dsn := os.Getenv("KUBBY_TEST_DB_DSN")
	kubeconfigPath := os.Getenv("KUBBY_TEST_KUBECONFIG")
	if dsn == "" || kubeconfigPath == "" {
		t.Skip("KUBBY_TEST_DB_DSN and KUBBY_TEST_KUBECONFIG are required for rotation tests")
	}

	kubeconfig, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		t.Fatalf("read kubeconfig: %v", err)
	}

	ctx := context.Background()

	// An isolated schema is not optional here: this command rewraps every row it finds,
	// so running it against the shared database would re-encrypt real credentials with
	// a throwaway test key and destroy them.
	schema := "rotate_" + uuid.NewString()[:8]
	db, err := store.OpenDSN(ctx, dsn+" search_path="+schema, 5)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		db.Close()
	})
	applySchema(t, db, schema)

	oldKey := bytes.Repeat([]byte{1}, 32)
	newKey := bytes.Repeat([]byte{2}, 32)

	oldRing, err := crypto.NewKeyring(1, oldKey)
	if err != nil {
		t.Fatalf("old keyring: %v", err)
	}

	// Seed a cluster whose credential is sealed under the old key.
	owner := seedOwner(t, db)
	svc := cluster.NewService(db, oldRing, cluster.Settings{
		DefaultQPS: 20, DefaultBurst: 40, Timeout: 10 * time.Second, AllowLoopback: true,
	})
	created, err := svc.Create(ctx, cluster.CreateInput{
		Name: "rotate-" + uuid.NewString()[:8], Environment: "test",
		Kubeconfig: kubeconfig, CreatedBy: owner,
	})
	if err != nil {
		t.Fatalf("Create cluster: %v", err)
	}
	before := storedBlob(t, db, created.ID)

	// Rotate: the new key is active, the old one kept for decryption only.
	newRing, err := crypto.NewKeyring(2, newKey)
	if err != nil {
		t.Fatalf("new keyring: %v", err)
	}
	if err := newRing.AddRetiredKey(1, oldKey); err != nil {
		t.Fatalf("AddRetiredKey: %v", err)
	}

	if !newRing.NeedsRewrap(before) {
		t.Fatal("a record sealed under the retired key is not reported as needing rewrap")
	}

	rewrapped, err := rewrapClusterCredentials(ctx, db, newRing, false)
	if err != nil {
		t.Fatalf("rewrapClusterCredentials: %v", err)
	}
	if rewrapped == 0 {
		t.Fatal("nothing was rewrapped")
	}

	after := storedBlob(t, db, created.ID)
	if bytes.Equal(before, after) {
		t.Fatal("the stored ciphertext did not change")
	}
	if newRing.NeedsRewrap(after) {
		t.Error("the rewrapped record still reports the retired key version")
	}

	t.Run("the rewrapped credential still works", func(t *testing.T) {
		// A keyring holding only the new key: this is the state after the old key is
		// discarded, which is the whole point of rotating.
		onlyNew, err := crypto.NewKeyring(2, newKey)
		if err != nil {
			t.Fatalf("keyring: %v", err)
		}
		rotatedSvc := cluster.NewService(db, onlyNew, cluster.Settings{
			DefaultQPS: 20, DefaultBurst: 40, Timeout: 10 * time.Second, AllowLoopback: true,
		})

		reloaded, err := db.Clusters().ByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("ByID: %v", err)
		}
		cfg, err := rotatedSvc.RESTConfigFor(ctx, reloaded, nil)
		if err != nil {
			t.Fatalf("RESTConfigFor after rotation: %v", err)
		}
		if health := cluster.Probe(ctx, cfg, 10*time.Second); health.Status != cluster.StatusValid {
			t.Errorf("the cluster is unusable after rotation: %s (%s)", health.Status, health.Detail)
		}
	})

	t.Run("a second run has nothing left to do", func(t *testing.T) {
		again, err := rewrapClusterCredentials(ctx, db, newRing, false)
		if err != nil {
			t.Fatalf("second pass: %v", err)
		}
		if again != 0 {
			t.Errorf("second pass rewrapped %d records; rotation is not idempotent", again)
		}
	})

	t.Run("a dry run writes nothing", func(t *testing.T) {
		thirdKey := bytes.Repeat([]byte{3}, 32)
		thirdRing, err := crypto.NewKeyring(3, thirdKey)
		if err != nil {
			t.Fatalf("keyring: %v", err)
		}
		if err := thirdRing.AddRetiredKey(2, newKey); err != nil {
			t.Fatalf("AddRetiredKey: %v", err)
		}

		snapshot := storedBlob(t, db, created.ID)
		count, err := rewrapClusterCredentials(ctx, db, thirdRing, true)
		if err != nil {
			t.Fatalf("dry run: %v", err)
		}
		if count == 0 {
			t.Error("the dry run reported nothing to do")
		}
		if !bytes.Equal(snapshot, storedBlob(t, db, created.ID)) {
			t.Error("the dry run modified stored data")
		}
	})
}

func seedOwner(t *testing.T, db *store.DB) uuid.UUID {
	t.Helper()

	// The hash is never verified here; only the foreign key matters.
	user, err := db.Users().Create(context.Background(),
		"rotate-owner-"+uuid.NewString()[:8]+"@example.com", "Owner",
		"$argon2id$v=19$m=65536,t=3,p=4$c29tZXNhbHR2YWx1ZXg$C1EPYbRYgAG1TeXfHKmL5j0dLpBsBJI3T0KcMdcFH2M",
		rbac.RoleAdmin)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	return user.ID
}

func storedBlob(t *testing.T, db *store.DB, clusterID uuid.UUID) []byte {
	t.Helper()

	var blob []byte
	err := db.Pool().QueryRow(context.Background(),
		`SELECT kubeconfig_enc FROM cluster_credentials WHERE cluster_id = $1`, clusterID).Scan(&blob)
	if err != nil {
		t.Fatalf("read stored blob: %v", err)
	}
	return blob
}
