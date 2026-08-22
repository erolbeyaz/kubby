package cluster

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ConnectionOptions shape how Kubby talks to one cluster.
type ConnectionOptions struct {
	// ContextName selects which context in the kubeconfig to use.
	ContextName string
	// QPS and Burst bound how hard a single cluster's API server can be hit, so one
	// busy screen cannot degrade the cluster for everyone else.
	QPS   float32
	Burst int
	// Timeout applies to individual requests, not to watches or log streams.
	Timeout time.Duration
	// ProxyURL routes traffic through an HTTP proxy when the API is only reachable
	// that way.
	ProxyURL string
	// ExtraCAs is appended to the system trust store for this connection.
	ExtraCAs *x509.CertPool
	// Impersonate makes the request carry a Kubernetes identity of its own, so the
	// cluster's audit log records who acted rather than which service account.
	Impersonate *ImpersonationConfig
}

// ImpersonationConfig maps a Kubby user onto a Kubernetes identity (ADR-005).
type ImpersonationConfig struct {
	Username string
	Groups   []string
}

// RESTConfig builds a client-go configuration from a stored kubeconfig.
//
// Exec-based authentication is stripped rather than trusted: even though the paste path
// rejects it, a stored config must not be able to run a command if it ever gets here.
func RESTConfig(kubeconfig []byte, opts ConnectionOptions) (*rest.Config, error) {
	config, err := clientcmd.Load(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKubeconfig, err)
	}
	for name, user := range config.AuthInfos {
		if user.Exec != nil || user.AuthProvider != nil {
			return nil, fmt.Errorf("%w: user %q", ErrExecPluginUsed, name)
		}
	}

	overrides := &clientcmd.ConfigOverrides{}
	if opts.ContextName != "" {
		overrides.CurrentContext = opts.ContextName
	}

	restCfg, err := clientcmd.NewDefaultClientConfig(*config, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build client config: %w", err)
	}

	applyOptions(restCfg, opts)
	return restCfg, nil
}

// InClusterRESTConfig builds a configuration from the pod's own service account.
// Only meaningful when Kubby runs inside Kubernetes (ADR-022).
func InClusterRESTConfig(opts ConnectionOptions) (*rest.Config, error) {
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("build in-cluster config: %w", err)
	}
	applyOptions(restCfg, opts)
	return restCfg, nil
}

// RunningInCluster reports whether a service account is mounted, which is what makes
// the in-cluster option available at all.
func RunningInCluster() bool {
	_, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token")
	return err == nil
}

func applyOptions(cfg *rest.Config, opts ConnectionOptions) {
	if opts.QPS > 0 {
		cfg.QPS = opts.QPS
	}
	if opts.Burst > 0 {
		cfg.Burst = opts.Burst
	}
	if opts.Timeout > 0 {
		cfg.Timeout = opts.Timeout
	}

	if opts.Impersonate != nil && opts.Impersonate.Username != "" {
		cfg.Impersonate = rest.ImpersonationConfig{
			UserName: opts.Impersonate.Username,
			Groups:   opts.Impersonate.Groups,
		}
	}

	if opts.ExtraCAs != nil && !cfg.Insecure {
		applyExtraCAs(cfg, opts.ExtraCAs)
	}

	if opts.ProxyURL != "" {
		if parsed, err := url.Parse(opts.ProxyURL); err == nil {
			cfg.Proxy = http.ProxyURL(parsed)
		}
	}
}

// applyExtraCAs makes the connection trust the extra bundle *in addition to* the CA the
// kubeconfig carries.
//
// Replacing RootCAs outright would drop the cluster's own CA and break exactly the
// connections that already worked, so the kubeconfig's CAData is folded into the same
// pool before it is installed.
func applyExtraCAs(cfg *rest.Config, extra *x509.CertPool) {
	pool := extra.Clone()

	if len(cfg.CAData) > 0 {
		if !pool.AppendCertsFromPEM(cfg.CAData) {
			// The kubeconfig CA is unusable; leave the config untouched rather than
			// silently connecting with a narrower trust store than intended.
			return
		}
	} else if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil || !pool.AppendCertsFromPEM(pem) {
			return
		}
	}

	// client-go builds its TLS config from CAData/CAFile, so both are cleared once
	// their contents live in the combined pool.
	cfg.CAData = nil
	cfg.CAFile = ""

	base := cfg.WrapTransport
	cfg.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		if transport, ok := rt.(*http.Transport); ok {
			if transport.TLSClientConfig == nil {
				transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			}
			transport.TLSClientConfig.RootCAs = pool
		}
		if base != nil {
			return base(rt)
		}
		return rt
	}
}

// Clientset builds a typed Kubernetes client.
func Clientset(cfg *rest.Config) (*kubernetes.Clientset, error) {
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return client, nil
}

// ServerFromKubeconfig reports the API server address a context points at, used to
// record where a cluster lives without decrypting the credential again.
func ServerFromKubeconfig(config *clientcmdapi.Config, contextName string) string {
	if contextName == "" {
		contextName = config.CurrentContext
	}
	ctx := config.Contexts[contextName]
	if ctx == nil {
		return ""
	}
	if cluster := config.Clusters[ctx.Cluster]; cluster != nil {
		return cluster.Server
	}
	return ""
}
