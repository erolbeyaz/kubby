// Package cluster owns kubeconfig handling and Kubernetes client construction.
package cluster

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

var (
	ErrInvalidKubeconfig = errors.New("kubeconfig is not valid")
	ErrExecPluginUsed    = errors.New("exec-plugin based authentication is not supported")
	ErrNoContexts        = errors.New("kubeconfig defines no usable context")
	ErrUnknownContext    = errors.New("kubeconfig has no such context")
)

// maxKubeconfigBytes bounds what will be parsed. Real kubeconfigs are a few kilobytes.
const maxKubeconfigBytes = 256 * 1024

// ContextInfo describes one context found in a pasted kubeconfig, so the user can pick
// which one to keep before anything is stored.
type ContextInfo struct {
	Name                    string
	ClusterName             string
	UserName                string
	Server                  string
	Namespace               string
	AuthMethod              string
	InsecureSkipTLSVerify   bool
	HasCertificateAuthority bool

	// Blocked marks a context that may never be used: its address is one Kubby
	// refuses to contact at all, such as a cloud metadata endpoint.
	Blocked bool
	// Problem explains why the context is blocked, or why it may fail to connect. A
	// context whose host does not resolve is still listed — it may simply be
	// unreachable from here — so the user sees it rather than wondering where it went.
	Problem string
}

// Usable reports whether this context may be saved and connected to.
func (c ContextInfo) Usable() bool { return !c.Blocked }

// ParsedKubeconfig is the result of validating pasted text, before any connection is
// attempted and before anything is written down.
type ParsedKubeconfig struct {
	Contexts       []ContextInfo
	CurrentContext string
	raw            []byte
	config         *clientcmdapi.Config
}

// ParseKubeconfig validates pasted kubeconfig text.
//
// Authentication is restricted to embedded bearer tokens and client certificates
// (ADR-017): an exec block makes client-go run an external command on the server, which
// would turn "paste a kubeconfig" into arbitrary code execution.
func ParseKubeconfig(raw []byte, policy AddressPolicy) (*ParsedKubeconfig, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: it is empty", ErrInvalidKubeconfig)
	}
	if len(raw) > maxKubeconfigBytes {
		return nil, fmt.Errorf("%w: larger than %d bytes", ErrInvalidKubeconfig, maxKubeconfigBytes)
	}

	config, err := clientcmd.Load(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKubeconfig, err)
	}
	if len(config.Contexts) == 0 {
		return nil, ErrNoContexts
	}

	// Reject exec before anything else touches the config, so a malicious entry cannot
	// be reached even by a later code path.
	for name, user := range config.AuthInfos {
		if user.Exec != nil {
			return nil, fmt.Errorf("%w: user %q uses an exec plugin (command %q)",
				ErrExecPluginUsed, name, user.Exec.Command)
		}
		if user.AuthProvider != nil {
			return nil, fmt.Errorf("%w: user %q uses auth provider %q, which is not supported",
				ErrExecPluginUsed, name, user.AuthProvider.Name)
		}
	}

	parsed := &ParsedKubeconfig{
		CurrentContext: config.CurrentContext,
		raw:            raw,
		config:         config,
	}

	names := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		info, err := describeContext(config, name, policy)
		if err != nil {
			return nil, err
		}
		parsed.Contexts = append(parsed.Contexts, *info)
	}

	usable := 0
	for _, c := range parsed.Contexts {
		if c.Usable() {
			usable++
		}
	}
	if usable == 0 {
		reasons := make([]string, 0, len(parsed.Contexts))
		for _, c := range parsed.Contexts {
			reasons = append(reasons, fmt.Sprintf("%s: %s", c.Name, c.Problem))
		}
		return nil, fmt.Errorf("%w: %s", ErrNoContexts, strings.Join(reasons, "; "))
	}
	return parsed, nil
}

func describeContext(config *clientcmdapi.Config, name string, policy AddressPolicy) (*ContextInfo, error) {
	ctx := config.Contexts[name]
	if ctx == nil {
		return nil, ErrUnknownContext
	}

	cluster := config.Clusters[ctx.Cluster]
	if cluster == nil {
		return nil, fmt.Errorf("%w: context %q references unknown cluster %q",
			ErrInvalidKubeconfig, name, ctx.Cluster)
	}

	info := &ContextInfo{
		Name:                    name,
		ClusterName:             ctx.Cluster,
		UserName:                ctx.AuthInfo,
		Server:                  cluster.Server,
		Namespace:               ctx.Namespace,
		InsecureSkipTLSVerify:   cluster.InsecureSkipTLSVerify,
		HasCertificateAuthority: len(cluster.CertificateAuthorityData) > 0 || cluster.CertificateAuthority != "",
		AuthMethod:              "none",
	}

	// A blocked address is a hard refusal; a name that simply does not resolve is
	// reported but left selectable, since resolution can depend on where Kubby runs.
	if _, err := policy.validateServerURL(cluster.Server); err != nil {
		info.Problem = err.Error()
		info.Blocked = errors.Is(err, ErrBlockedAddress) && !strings.Contains(err.Error(), "cannot resolve")
	}

	if user := config.AuthInfos[ctx.AuthInfo]; user != nil {
		switch {
		case user.Token != "" || user.TokenFile != "":
			info.AuthMethod = "token"
		case len(user.ClientCertificateData) > 0 || user.ClientCertificate != "":
			info.AuthMethod = "client-certificate"
		case user.Username != "":
			info.AuthMethod = "basic"
		}
	}
	return info, nil
}

// Context returns one context by name, defaulting to the current context. A blocked
// context is refused here so no caller can select its way past the address policy.
func (p *ParsedKubeconfig) Context(name string) (*ContextInfo, error) {
	if name == "" {
		name = p.CurrentContext
	}
	for i := range p.Contexts {
		if p.Contexts[i].Name != name {
			continue
		}
		if p.Contexts[i].Blocked {
			return nil, fmt.Errorf("%w: %s", ErrBlockedAddress, p.Contexts[i].Problem)
		}
		return &p.Contexts[i], nil
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownContext, name)
}

// Raw returns the original bytes, for encryption at rest. It is never sent to a client.
func (p *ParsedKubeconfig) Raw() []byte { return p.raw }
