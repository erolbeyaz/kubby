package cluster

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// restClientGetter satisfies what kubectl's describers ask for, built from a REST config
// we already hold rather than from a kubeconfig on disk.
//
// Kubby never has a kubeconfig on disk: credentials are stored encrypted and turned into
// a REST config in memory. The parts of the interface that only exist to read files are
// therefore not implemented, and say so instead of returning something misleading.
type restClientGetter struct {
	config *rest.Config
}

func (g *restClientGetter) ToRESTConfig() (*rest.Config, error) { return g.config, nil }

func (g *restClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	client, err := discovery.NewDiscoveryClientForConfig(g.config)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(client), nil
}

func (g *restClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	client, err := g.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	return restmapper.NewDeferredDiscoveryRESTMapper(client), nil
}

func (g *restClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	// Nothing in the describe path calls this. Returning an empty loader would look like
	// a valid empty kubeconfig, which is worse than an obvious failure.
	return errorClientConfig{}
}

type errorClientConfig struct{}

var errNoKubeconfigOnDisk = fmt.Errorf("kubby holds credentials in memory; there is no kubeconfig to load")

func (errorClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return clientcmdapi.Config{}, errNoKubeconfigOnDisk
}
func (errorClientConfig) ClientConfig() (*rest.Config, error) { return nil, errNoKubeconfigOnDisk }
func (errorClientConfig) Namespace() (string, bool, error)    { return "", false, errNoKubeconfigOnDisk }
func (errorClientConfig) ConfigAccess() clientcmd.ConfigAccess {
	return clientcmd.NewDefaultPathOptions()
}
