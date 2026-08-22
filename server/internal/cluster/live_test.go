package cluster_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/erolbeyaz/kubby/internal/cluster"
)

// Live tests need a real cluster. Point KUBBY_TEST_KUBECONFIG at one to enable them:
//
//	KUBBY_TEST_KUBECONFIG=/path/to/kubeconfig go test ./internal/cluster/
//
// A fake client would prove the code compiles, not that it can actually reach a
// Kubernetes API and classify what comes back.
func liveKubeconfig(t *testing.T) []byte {
	t.Helper()

	path := os.Getenv("KUBBY_TEST_KUBECONFIG")
	if path == "" {
		t.Skip("KUBBY_TEST_KUBECONFIG is not set; skipping live cluster tests")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read kubeconfig: %v", err)
	}
	return raw
}

func TestProbeReachesARealCluster(t *testing.T) {
	raw := liveKubeconfig(t)

	cfg, err := cluster.RESTConfig(raw, cluster.ConnectionOptions{
		QPS: 20, Burst: 40, Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}

	health := cluster.Probe(context.Background(), cfg, 15*time.Second)

	if health.Status != cluster.StatusValid {
		t.Fatalf("Status = %q (%s), want valid", health.Status, health.Detail)
	}
	if health.K8sVersion == "" {
		t.Error("no Kubernetes version was reported")
	}
	if health.NodeCount == nil || *health.NodeCount == 0 {
		t.Errorf("NodeCount = %v, want at least one node", health.NodeCount)
	}
	if len(health.Permissions) == 0 {
		t.Error("no permissions were reported; the access review did not run")
	}

	t.Logf("cluster: %s, %d nodes, metrics=%v, can: %v",
		health.K8sVersion, *health.NodeCount, health.MetricsAvailable, health.Permissions)
}

// A revoked or mistyped token must be reported as an invalid credential, not as an
// outage: the two need different responses from the user (ADR-018).
func TestProbeReportsABadTokenAsInvalidCredential(t *testing.T) {
	raw := liveKubeconfig(t)

	parsed, err := cluster.ParseKubeconfig(raw, cluster.AddressPolicy{AllowLoopback: true})
	if err != nil {
		t.Fatalf("ParseKubeconfig: %v", err)
	}
	ctx, err := parsed.Context("")
	if err != nil {
		t.Fatalf("Context: %v", err)
	}

	broken := replaceToken(t, raw, ctx.Name)

	cfg, err := cluster.RESTConfig(broken, cluster.ConnectionOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}

	health := cluster.Probe(context.Background(), cfg, 15*time.Second)
	if health.Status != cluster.StatusInvalid {
		t.Fatalf("Status = %q (%s), want invalid", health.Status, health.Detail)
	}
	if health.Detail == "" {
		t.Error("no explanation was given for the rejected credential")
	}
	t.Logf("detail: %s", health.Detail)
}

// An address that nothing answers on must be reported as unreachable, leaving the
// credential's own status untouched.
func TestProbeReportsAnUnreachableEndpoint(t *testing.T) {
	raw := []byte(`
apiVersion: v1
kind: Config
clusters:
- {name: c, cluster: {server: "https://127.0.0.1:1", insecure-skip-tls-verify: true}}
users:
- {name: u, user: {token: irrelevant}}
contexts:
- {name: ctx, context: {cluster: c, user: u}}
current-context: ctx
`)

	cfg, err := cluster.RESTConfig(raw, cluster.ConnectionOptions{Timeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}

	health := cluster.Probe(context.Background(), cfg, 5*time.Second)
	if health.Status != cluster.StatusUnreachable {
		t.Fatalf("Status = %q (%s), want unreachable", health.Status, health.Detail)
	}
}

// A stored kubeconfig must not be able to run a command even if it somehow carries an
// exec block: the paste path rejects it, and so does the connection path.
func TestRESTConfigRefusesExecEvenWhenStored(t *testing.T) {
	raw := []byte(`
apiVersion: v1
kind: Config
clusters:
- {name: c, cluster: {server: "https://127.0.0.1:6550", insecure-skip-tls-verify: true}}
users:
- name: u
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: /bin/sh
      args: ["-c", "touch /tmp/pwned"]
contexts:
- {name: ctx, context: {cluster: c, user: u}}
current-context: ctx
`)

	if _, err := cluster.RESTConfig(raw, cluster.ConnectionOptions{}); err == nil {
		t.Fatal("RESTConfig accepted an exec-based kubeconfig")
	}
}

// The extra CA bundle must be added to the cluster's own CA, not substituted for it:
// replacing it would break every connection that already worked.
func TestExtraCABundleIsAddedNotSubstituted(t *testing.T) {
	raw := liveKubeconfig(t)

	// A pool holding an unrelated CA, standing in for a corporate root.
	unrelated := x509.NewCertPool()
	if !unrelated.AppendCertsFromPEM(selfSignedPEM(t)) {
		t.Fatal("could not build the test CA pool")
	}

	cfg, err := cluster.RESTConfig(raw, cluster.ConnectionOptions{
		Timeout:  10 * time.Second,
		ExtraCAs: unrelated,
	})
	if err != nil {
		t.Fatalf("RESTConfig: %v", err)
	}

	// The cluster is signed by its own CA, which is only trusted if it survived the
	// merge with the extra bundle.
	health := cluster.Probe(context.Background(), cfg, 15*time.Second)
	if health.Status != cluster.StatusValid {
		t.Fatalf("Status = %q (%s): the kubeconfig CA was lost when the extra bundle was applied",
			health.Status, health.Detail)
	}
}

// selfSignedPEM produces a throwaway CA certificate for the trust-merge test.
func selfSignedPEM(t *testing.T) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kubby-test-unrelated-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
