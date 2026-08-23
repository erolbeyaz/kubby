package health

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	secretsGVR   = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	ingressesGVR = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}
)

// Certificate expiry thresholds. Thirty days is enough notice to renew through a change
// process; seven means someone should be doing it now.
const (
	DefaultCertWarning  = 30 * 24 * time.Hour
	DefaultCertCritical = 7 * 24 * time.Hour
)

// CertificateDetector reports TLS certificates that are running out.
//
// An expired certificate is an outage that announces itself only at the moment it starts,
// which is exactly the class of problem this panel exists to move earlier.
type CertificateDetector struct {
	Namespaces []string
	Warning    time.Duration
	Critical   time.Duration
	Now        func() time.Time
}

func (d *CertificateDetector) Name() string { return "certificate" }

func (d *CertificateDetector) Detect(ctx context.Context, r Reader) ([]Finding, error) {
	wanted, err := d.ingressSecrets(ctx, r)
	if err != nil {
		return nil, err
	}
	if len(wanted) == 0 {
		return nil, nil
	}

	var findings []Finding
	for _, namespace := range namespacesOr(d.Namespaces) {
		secrets, err := r.List(ctx, secretsGVR, namespace)
		if err != nil {
			return nil, err
		}
		for i := range secrets {
			key := secrets[i].GetNamespace() + "/" + secrets[i].GetName()
			hosts, referenced := wanted[key]
			if !referenced {
				continue
			}
			if finding, found := d.inspect(&secrets[i], hosts); found {
				findings = append(findings, finding)
			}
		}
	}
	return findings, nil
}

// ingressSecrets collects the secrets Ingresses actually serve. Reading every TLS secret
// in the cluster would report certificates nothing uses, and service-account and
// webhook certificates that Kubernetes rotates on its own.
func (d *CertificateDetector) ingressSecrets(ctx context.Context, r Reader) (map[string][]string, error) {
	wanted := map[string][]string{}

	for _, namespace := range namespacesOr(d.Namespaces) {
		ingresses, err := r.List(ctx, ingressesGVR, namespace)
		if err != nil {
			return nil, err
		}
		for i := range ingresses {
			for _, entry := range mapsAt(&ingresses[i], "spec", "tls") {
				name, _ := entry["secretName"].(string)
				if name == "" {
					continue
				}
				key := ingresses[i].GetNamespace() + "/" + name
				wanted[key] = append(wanted[key], hostsOf(entry)...)
			}
		}
	}
	return wanted, nil
}

func (d *CertificateDetector) inspect(secret *unstructured.Unstructured, hosts []string) (Finding, bool) {
	encoded, _, _ := unstructured.NestedString(secret.Object, "data", "tls.crt")
	if encoded == "" {
		return Finding{}, false
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Finding{}, false
	}
	expiry, err := earliestExpiry(raw)
	if err != nil {
		return Finding{}, false
	}

	severity, remaining, found := d.severityFor(expiry)
	if !found {
		return Finding{}, false
	}

	return Finding{
		Category:  CategoryCert,
		Severity:  severity,
		Kind:      "Secret",
		Namespace: secret.GetNamespace(),
		Name:      secret.GetName(),
		Reason:    reasonFor(remaining),
		Detail:    certDetail(remaining, expiry, hosts),
		LastSeen:  expiry.UTC().Format(time.RFC3339),
		TypeKey:   "secrets",
	}, true
}

func (d *CertificateDetector) severityFor(expiry time.Time) (severity string, remaining time.Duration, found bool) {
	remaining = expiry.Sub(d.now())

	switch {
	case remaining <= d.critical():
		return SeverityCritical, remaining, true
	case remaining <= d.warning():
		return SeverityWarning, remaining, true
	}
	return "", 0, false
}

func reasonFor(remaining time.Duration) string {
	if remaining <= 0 {
		return "CertificateExpired"
	}
	return "CertificateExpiring"
}

func certDetail(remaining time.Duration, expiry time.Time, hosts []string) string {
	served := ""
	if len(hosts) > 0 {
		served = fmt.Sprintf(" It serves %s.", joinHosts(hosts))
	}
	if remaining <= 0 {
		return fmt.Sprintf("The certificate expired on %s.%s", expiry.UTC().Format(time.RFC3339), served)
	}
	return fmt.Sprintf("The certificate expires in %d days, on %s.%s",
		int(remaining.Hours()/24), expiry.UTC().Format(time.RFC3339), served)
}

// earliestExpiry returns the soonest NotAfter in a PEM bundle: a chain is only valid
// until its first certificate expires, and that is often the intermediate rather than
// the leaf.
func earliestExpiry(raw []byte) (time.Time, error) {
	var earliest time.Time

	for block, rest := pem.Decode(raw); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != "CERTIFICATE" {
			continue
		}
		parsed, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		if earliest.IsZero() || parsed.NotAfter.Before(earliest) {
			earliest = parsed.NotAfter
		}
	}
	if earliest.IsZero() {
		return time.Time{}, fmt.Errorf("no certificate in bundle")
	}
	return earliest, nil
}

// ExpiryOf reports the soonest expiry in a PEM bundle, for certificates that do not live
// in the cluster — the kubeconfig's client certificate and its cluster CA.
func ExpiryOf(pemBytes []byte) (time.Time, error) { return earliestExpiry(pemBytes) }

func hostsOf(entry map[string]any) []string {
	raw, _ := entry["hosts"].([]any)

	out := make([]string, 0, len(raw))
	for _, host := range raw {
		if value, ok := host.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func joinHosts(hosts []string) string {
	switch len(hosts) {
	case 1:
		return hosts[0]
	case 2:
		return hosts[0] + " and " + hosts[1]
	}
	return fmt.Sprintf("%s and %d more", hosts[0], len(hosts)-1)
}

func (d *CertificateDetector) warning() time.Duration {
	if d.Warning <= 0 {
		return DefaultCertWarning
	}
	return d.Warning
}

func (d *CertificateDetector) critical() time.Duration {
	if d.Critical <= 0 {
		return DefaultCertCritical
	}
	return d.Critical
}

func (d *CertificateDetector) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}
