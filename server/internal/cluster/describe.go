package cluster

import (
	"context"
	"fmt"

	"k8s.io/kubectl/pkg/describe"

	"github.com/erolbeyaz/kubby/internal/store"
)

// Describe renders the same text `kubectl describe` prints.
//
// This uses kubectl's own describers rather than a formatter of our own. A describe
// output people already know how to read is worth more than a prettier one they have to
// learn, and every release of Kubernetes that adds a field adds it here too.
func (s *Service) Describe(ctx context.Context, cluster *store.Cluster, resourceType ResourceType, namespace, name string, impersonate *ImpersonationConfig) (string, error) {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return "", err
	}

	getter := &restClientGetter{config: cfg}
	mapper, err := getter.ToRESTMapper()
	if err != nil {
		return "", fmt.Errorf("build rest mapper: %w", err)
	}

	gvk := resourceType.GVR().GroupVersion().WithKind(resourceType.Kind)
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return "", fmt.Errorf("%w: %s is not served by this cluster", ErrKindUnavailable, resourceType.Kind)
	}

	describer, err := describe.Describer(getter, mapping)
	if err != nil {
		return "", fmt.Errorf("%w: %s cannot be described", ErrKindUnavailable, resourceType.Kind)
	}

	out, err := describer.Describe(namespace, name, describe.DescriberSettings{ShowEvents: true})
	if err != nil {
		return "", translateAPIError(err, resourceType)
	}
	return out, nil
}
