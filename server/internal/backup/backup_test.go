package backup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erolbeyaz/kubby/internal/crypto"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
	"github.com/erolbeyaz/kubby/internal/store/storetest"
)

const passphrase = "a-long-enough-passphrase"

func keyring(t *testing.T, b byte) *crypto.Keyring {
	t.Helper()

	k, err := crypto.NewKeyring(1, bytes.Repeat([]byte{b}, 32))
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	return k
}

// The case the whole tool exists for: an archive from one installation, restored into a
// fresh one with a different key, and the credential still opens.
//
// If the archive were sealed with the instance key this would be impossible, which is why
// it is sealed with the passphrase instead.
func TestAnArchiveRestoresIntoAnInstallationWithADifferentKey(t *testing.T) {
	ctx := context.Background()

	source := storetest.Isolated(t, "../../migrations")
	sourceKey := keyring(t, 7)

	sealed, err := sourceKey.Seal([]byte("apiVersion: v1\nkind: Config\nclusters: []\n"), nil)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	cluster, err := source.Clusters().Create(ctx, store.NewCluster{
		Name: "prod-eu", Environment: "prod", AuthSource: "kubeconfig",
		APIServerURL: "https://api.example.com", ContextName: "prod",
		KubeconfigEnc: sealed,
	})
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}

	// Re-sealed bound to the row, the way the real registration path does it.
	bound, err := sourceKey.Seal([]byte("apiVersion: v1\nkind: Config\nclusters: []\n"),
		[]byte("cluster:"+cluster.ID.String()))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := source.Clusters().ReplaceCredential(ctx, cluster.ID, "prod", bound,
		"https://api.example.com", false); err != nil {
		t.Fatalf("replace credential: %v", err)
	}

	user, err := source.Users().Create(ctx, "someone@example.com", "Someone", "$argon2id$fake", rbac.RoleUser)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := source.Clusters().SetGrant(ctx, user.ID, cluster.ID, user.ID, store.AccessRead); err != nil {
		t.Fatalf("grant: %v", err)
	}

	path := filepath.Join(t.TempDir(), "kubby.bak")
	exported, err := New(source, sourceKey).Export(ctx, path, passphrase)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if exported.Clusters != 1 || exported.Users != 1 || exported.Grants != 1 {
		t.Fatalf("exported %+v", exported)
	}

	// A different installation, with a key that has nothing to do with the first.
	target := storetest.Isolated(t, "../../migrations")
	targetKey := keyring(t, 42)

	restored, err := New(target, targetKey).Restore(ctx, path, passphrase, false)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Clusters != 1 || restored.Users != 1 || restored.Grants != 1 {
		t.Fatalf("restored %+v", restored)
	}

	// And the credential opens with the new installation's key, which is the only proof
	// that matters.
	_, resealed, err := target.Clusters().Credential(ctx, cluster.ID)
	if err != nil {
		t.Fatalf("read the restored credential: %v", err)
	}
	plain, err := targetKey.Open(resealed, []byte("cluster:"+cluster.ID.String()))
	if err != nil {
		t.Fatalf("the restored credential cannot be opened by the installation holding it: %v", err)
	}
	if !strings.Contains(string(plain), "kind: Config") {
		t.Fatalf("the restored credential is not the kubeconfig: %.60s", plain)
	}
}

// A restore run against a live installation by mistake must be a no-op, not the last
// thing that happens to it.
func TestRestoringOverExistingDataChangesNothing(t *testing.T) {
	ctx := context.Background()

	db := storetest.Isolated(t, "../../migrations")
	key := keyring(t, 7)

	sealed, _ := key.Seal([]byte("original"), nil)
	cluster, err := db.Clusters().Create(ctx, store.NewCluster{
		Name: "prod", Environment: "prod", AuthSource: "kubeconfig",
		APIServerURL: "https://api.example.com", ContextName: "prod", KubeconfigEnc: sealed,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	bound, _ := key.Seal([]byte("original"), []byte("cluster:"+cluster.ID.String()))
	_ = db.Clusters().ReplaceCredential(ctx, cluster.ID, "prod", bound, "https://api.example.com", false)

	path := filepath.Join(t.TempDir(), "kubby.bak")
	service := New(db, key)
	if _, err := service.Export(ctx, path, passphrase); err != nil {
		t.Fatalf("export: %v", err)
	}

	// Change something, then restore the archive over it.
	locked := true
	if err := db.Clusters().UpdateSettings(ctx, cluster.ID, store.ClusterSettings{ReadOnly: &locked}); err != nil {
		t.Fatalf("update: %v", err)
	}

	summary, err := service.Restore(ctx, path, passphrase, false)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if summary.Clusters != 0 || summary.Skipped == 0 {
		t.Errorf("an existing cluster was rewritten: %+v", summary)
	}

	after, err := db.Clusters().ByID(ctx, cluster.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !after.ReadOnly {
		t.Fatal("the restore undid a change made after the export")
	}
}

func TestADryRunWritesNothing(t *testing.T) {
	ctx := context.Background()

	source := storetest.Isolated(t, "../../migrations")
	key := keyring(t, 7)

	sealed, _ := key.Seal([]byte("x"), nil)
	c, _ := source.Clusters().Create(ctx, store.NewCluster{
		Name: "prod", Environment: "prod", AuthSource: "kubeconfig",
		APIServerURL: "https://api.example.com", ContextName: "prod", KubeconfigEnc: sealed,
	})
	bound, _ := key.Seal([]byte("x"), []byte("cluster:"+c.ID.String()))
	_ = source.Clusters().ReplaceCredential(ctx, c.ID, "prod", bound, "https://api.example.com", false)

	path := filepath.Join(t.TempDir(), "kubby.bak")
	if _, err := New(source, key).Export(ctx, path, passphrase); err != nil {
		t.Fatalf("export: %v", err)
	}

	target := storetest.Isolated(t, "../../migrations")
	summary, err := New(target, keyring(t, 42)).Restore(ctx, path, passphrase, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if summary.Clusters != 1 {
		t.Errorf("the dry run should report what it would do: %+v", summary)
	}

	clusters, err := target.Clusters().List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(clusters) != 0 {
		t.Fatalf("the dry run wrote %d clusters", len(clusters))
	}
}

// The passphrase is the only thing between this file and every cluster in it.
func TestAWrongPassphraseOpensNothing(t *testing.T) {
	ctx := context.Background()
	db := storetest.Isolated(t, "../../migrations")

	path := filepath.Join(t.TempDir(), "kubby.bak")
	if _, err := New(db, keyring(t, 7)).Export(ctx, path, passphrase); err != nil {
		t.Fatalf("export: %v", err)
	}

	_, err := New(db, keyring(t, 7)).Restore(ctx, path, "the-wrong-passphrase", true)
	if err == nil {
		t.Fatal("a wrong passphrase opened the archive")
	}
	// Deliberately indistinguishable from a tampered file: saying which would help
	// somebody guessing.
	if !strings.Contains(err.Error(), "wrong passphrase") {
		t.Errorf("unhelpful error: %v", err)
	}
}

// A byte changed anywhere must fail the open rather than produce altered contents.
func TestATamperedArchiveIsRefused(t *testing.T) {
	ctx := context.Background()
	db := storetest.Isolated(t, "../../migrations")

	path := filepath.Join(t.TempDir(), "kubby.bak")
	if _, err := New(db, keyring(t, 7)).Export(ctx, path, passphrase); err != nil {
		t.Fatalf("export: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// One byte in the header, which is authenticated as additional data, and one in the
	// ciphertext. Both have to fail.
	for _, at := range []int{len(magic) + 2, len(body) - 5} {
		altered := append([]byte(nil), body...)
		altered[at] ^= 0xff
		if err := os.WriteFile(path, altered, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		if _, err := New(db, keyring(t, 7)).Restore(ctx, path, passphrase, true); err == nil {
			t.Fatalf("a byte changed at %d was accepted", at)
		}
	}
}

func TestAShortPassphraseIsNotAccepted(t *testing.T) {
	if MinPassphrase < 12 {
		t.Errorf("the minimum passphrase is %d; this file protects every cluster credential",
			MinPassphrase)
	}
}
