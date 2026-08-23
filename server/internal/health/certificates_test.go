package health

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var testNow = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

func certPEM(t *testing.T, notAfter time.Time) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "shop.example.com"},
		NotBefore:    testNow.Add(-24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func certFixture(t *testing.T, notAfter time.Time) fakeReader {
	t.Helper()

	ingress := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "shop", "namespace": "storefront"},
		"spec": map[string]any{"tls": []any{map[string]any{
			"secretName": "shop-tls",
			"hosts":      []any{"shop.example.com"},
		}}},
	}}
	secret := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "shop-tls", "namespace": "storefront"},
		"type":     "kubernetes.io/tls",
		"data":     map[string]any{"tls.crt": base64.StdEncoding.EncodeToString(certPEM(t, notAfter))},
	}}
	// An unreferenced TLS secret: Kubernetes rotates plenty of certificates on its own,
	// and reporting them would bury the ones a person has to renew.
	unused := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "webhook-tls", "namespace": "storefront"},
		"type":     "kubernetes.io/tls",
		"data": map[string]any{
			"tls.crt": base64.StdEncoding.EncodeToString(certPEM(t, testNow.Add(24*time.Hour))),
		},
	}}

	return fakeReader{objects: map[schema.GroupVersionResource][]unstructured.Unstructured{
		ingressesGVR: {ingress},
		secretsGVR:   {secret, unused},
	}}
}

func detectCerts(t *testing.T, notAfter time.Time) []Finding {
	t.Helper()

	d := &CertificateDetector{Now: func() time.Time { return testNow }}
	findings, err := d.Detect(context.Background(), certFixture(t, notAfter))
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	return findings
}

func TestCertificateWithinThirtyDaysWarns(t *testing.T) {
	findings := detectCerts(t, testNow.Add(20*24*time.Hour))

	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want only the ingress-referenced secret", findings)
	}
	if findings[0].Severity != SeverityWarning || findings[0].Reason != "CertificateExpiring" {
		t.Fatalf("finding = %+v", findings[0])
	}
	if findings[0].Name != "shop-tls" {
		t.Fatalf("name = %q, want the referenced secret", findings[0].Name)
	}
}

func TestCertificateWithinSevenDaysIsCritical(t *testing.T) {
	findings := detectCerts(t, testNow.Add(3*24*time.Hour))

	if len(findings) != 1 || findings[0].Severity != SeverityCritical {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestExpiredCertificateSaysSo(t *testing.T) {
	findings := detectCerts(t, testNow.Add(-24*time.Hour))

	if len(findings) != 1 || findings[0].Reason != "CertificateExpired" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Severity != SeverityCritical {
		t.Fatalf("severity = %q", findings[0].Severity)
	}
}

func TestHealthyCertificateProducesNothing(t *testing.T) {
	findings := detectCerts(t, testNow.Add(90*24*time.Hour))

	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none", findings)
	}
}

// A chain is only valid until its first certificate expires, and that is often the
// intermediate rather than the leaf.
func TestExpiryOfReportsTheEarliestInTheChain(t *testing.T) {
	leaf := certPEM(t, testNow.Add(90*24*time.Hour))
	intermediate := certPEM(t, testNow.Add(10*24*time.Hour))

	got, err := ExpiryOf(append(leaf, intermediate...))
	if err != nil {
		t.Fatalf("expiry: %v", err)
	}
	if got.After(testNow.Add(11 * 24 * time.Hour)) {
		t.Fatalf("expiry = %s, want the intermediate's", got)
	}
}
