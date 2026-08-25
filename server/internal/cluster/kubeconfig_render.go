package cluster

import (
	"context"
	"fmt"
	"os"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/erolbeyaz/kubby/internal/store"
)

// RenderKubeconfig writes out a kubeconfig holding exactly one cluster.
//
// It is derived from the same rest.Config every other request uses, so the terminal
// inherits the connection Kubby already made: the extra CA bundle, the proxy, and the
// impersonated identity. Deriving it separately would let the terminal reach a cluster
// on terms nothing else in this tool agreed to.
//
// One context, no others. There is nothing here to switch to, which is what keeps a
// `kubectl config use-context` in that terminal from reaching a different cluster.
func (s *Service) RenderKubeconfig(ctx context.Context, cluster *store.Cluster, impersonate *ImpersonationConfig) ([]byte, string, error) {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, "", err
	}

	name := kubeconfigName(cluster.Name)

	target := clientcmdapi.NewCluster()
	target.Server = cfg.Host
	target.InsecureSkipTLSVerify = cfg.Insecure
	target.CertificateAuthorityData, err = bytesOrFile(cfg.CAData, cfg.CAFile)
	if err != nil {
		return nil, "", err
	}
	if cfg.ServerName != "" {
		target.TLSServerName = cfg.ServerName
	}
	if cfg.Proxy != nil {
		// The rest.Config carries the proxy as a function, which a file cannot hold. The
		// process inherits HTTPS_PROXY from Kubby's own environment instead.
		target.ProxyURL = os.Getenv("HTTPS_PROXY")
	}

	user := clientcmdapi.NewAuthInfo()
	user.Token = cfg.BearerToken
	if cfg.BearerTokenFile != "" && user.Token == "" {
		token, err := os.ReadFile(cfg.BearerTokenFile)
		if err != nil {
			return nil, "", fmt.Errorf("read the cluster token: %w", err)
		}
		user.Token = string(token)
	}
	user.ClientCertificateData, err = bytesOrFile(cfg.CertData, cfg.CertFile)
	if err != nil {
		return nil, "", err
	}
	user.ClientKeyData, err = bytesOrFile(cfg.KeyData, cfg.KeyFile)
	if err != nil {
		return nil, "", err
	}
	user.Username = cfg.Username
	user.Password = cfg.Password

	// Impersonation belongs in the file rather than in an argument: an argument can be
	// left off, and the identity the cluster records must not be the reader's to choose.
	if cfg.Impersonate.UserName != "" {
		user.Impersonate = cfg.Impersonate.UserName
		user.ImpersonateGroups = cfg.Impersonate.Groups
	}

	context := clientcmdapi.NewContext()
	context.Cluster = name
	context.AuthInfo = name

	config := clientcmdapi.NewConfig()
	config.Clusters[name] = target
	config.AuthInfos[name] = user
	config.Contexts[name] = context
	config.CurrentContext = name

	rendered, err := clientcmd.Write(*config)
	if err != nil {
		return nil, "", fmt.Errorf("write the kubeconfig: %w", err)
	}
	return rendered, name, nil
}

// bytesOrFile prefers the bytes already in memory and falls back to the path they would
// otherwise have been read from.
func bytesOrFile(data []byte, path string) ([]byte, error) {
	if len(data) > 0 {
		return data, nil
	}
	if path == "" {
		return nil, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return contents, nil
}

// kubeconfigName keeps the cluster's own name where a reader will see it — in the prompt
// of `kubectl config current-context` — while staying a legal identifier.
func kubeconfigName(name string) string {
	safe := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			safe = append(safe, r)
		default:
			safe = append(safe, '-')
		}
	}
	if len(safe) == 0 {
		return "cluster"
	}
	return string(safe)
}
