package cluster

import (
	"context"
	"errors"
	"fmt"

	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"

	"github.com/erolbeyaz/kubby/internal/store"
)

// Reasons a write is refused, in the order they are checked.
var (
	ErrGlobalReadOnly = errors.New("kubby is running read-only")
	ErrNotAllowed     = errors.New("you may not change objects in this cluster")
	ErrClusterDenied  = errors.New("the cluster refused this operation")
)

// Verb is what is being asked of the API server.
type Verb string

const (
	VerbCreate Verb = "create"
	VerbUpdate Verb = "update"
	VerbPatch  Verb = "patch"
	VerbDelete Verb = "delete"
)

// WriteRequest is one intended change, before anything has been sent.
type WriteRequest struct {
	Type      ResourceType
	Namespace string
	Name      string
	Verb      Verb
}

// Permission is what the caller's own role allows, decided before this package is reached.
type Permission struct {
	// GlobalReadOnly is the deployment-wide kill switch.
	GlobalReadOnly bool
	// MayWrite is the role's verdict (RBAC), not the cluster's.
	MayWrite bool
}

// GitOpsOwner names the controller that owns an object, when one does.
type GitOpsOwner struct {
	Controller string `json:"controller"`
	Instance   string `json:"instance,omitempty"`
	// SelfHeal means the controller will revert this change, usually within minutes.
	SelfHeal bool `json:"selfHeal"`
}

// WriteVerdict is why a write may or may not proceed, and what the caller should be told
// before it does.
type WriteVerdict struct {
	Allowed bool         `json:"allowed"`
	Reason  string       `json:"reason,omitempty"`
	Owner   *GitOpsOwner `json:"owner,omitempty"`
}

// CheckWrite runs every gate in order and returns why, not just whether.
//
// The order matters: the cheapest and most absolute refusals come first, so a locked
// cluster is never asked to authorise something it would not have accepted. Nothing here
// is optional and nothing may be skipped — a control that can be bypassed is not a
// control, and this tool holds cluster-wide credentials.
func (s *Service) CheckWrite(ctx context.Context, cluster *store.Cluster, req WriteRequest, perm Permission, impersonate *ImpersonationConfig) (*WriteVerdict, error) {
	if perm.GlobalReadOnly {
		return &WriteVerdict{Reason: ErrGlobalReadOnly.Error()}, nil
	}
	if !perm.MayWrite {
		return &WriteVerdict{Reason: ErrNotAllowed.Error()}, nil
	}
	// The per-cluster lock binds everyone, admins included (ADR-029/039): it exists for
	// the window where a cluster must not change, not to express who is senior.
	if cluster.ReadOnly {
		return &WriteVerdict{Reason: ErrReadOnlyCluster.Error()}, nil
	}

	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	// The credential Kubby holds may be narrower than the role suggests, and asking the
	// cluster is the only way to know before trying.
	review := &authzv1.SelfSubjectAccessReview{
		Spec: authzv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authzv1.ResourceAttributes{
				Namespace: req.Namespace,
				Verb:      string(req.Verb),
				Group:     req.Type.Group,
				Version:   req.Type.Version,
				Resource:  req.Type.Resource,
				Name:      req.Name,
			},
		},
	}
	result, err := client.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("check permission: %w", err)
	}
	if !result.Status.Allowed {
		reason := result.Status.Reason
		if reason == "" {
			reason = fmt.Sprintf("the cluster credential may not %s %s", req.Verb, req.Type.Resource)
		}
		return &WriteVerdict{Reason: fmt.Errorf("%w: %s", ErrClusterDenied, reason).Error()}, nil
	}

	return &WriteVerdict{Allowed: true}, nil
}

// GitOps annotations and labels the common controllers leave behind.
const (
	argoInstanceLabel      = "argocd.argoproj.io/instance"
	argoInstanceAnnotation = "argocd.argoproj.io/tracking-id"
	fluxNameLabel          = "kustomize.toolkit.fluxcd.io/name"
	fluxNamespaceLabel     = "kustomize.toolkit.fluxcd.io/namespace"
	argoSyncOptions        = "argocd.argoproj.io/sync-options"
)

// OwnerOf reports which controller manages an object, if any.
//
// Editing a GitOps-managed object is not forbidden — sometimes it is exactly what an
// incident needs — but doing it without knowing is how a change quietly disappears and
// nobody understands why (ADR-028).
func OwnerOf(obj *unstructured.Unstructured) *GitOpsOwner {
	labels := obj.GetLabels()
	annotations := obj.GetAnnotations()

	if instance := labels[argoInstanceLabel]; instance != "" {
		return &GitOpsOwner{Controller: "argocd", Instance: instance, SelfHeal: !argoSyncDisabled(annotations)}
	}
	if tracking := annotations[argoInstanceAnnotation]; tracking != "" {
		return &GitOpsOwner{Controller: "argocd", Instance: tracking, SelfHeal: !argoSyncDisabled(annotations)}
	}
	if name := labels[fluxNameLabel]; name != "" {
		instance := name
		if namespace := labels[fluxNamespaceLabel]; namespace != "" {
			instance = namespace + "/" + name
		}
		return &GitOpsOwner{Controller: "flux", Instance: instance, SelfHeal: true}
	}
	return nil
}

// argoSyncDisabled reports whether the object opts out of being reconciled back.
func argoSyncDisabled(annotations map[string]string) bool {
	return annotations[argoSyncOptions] == "Prune=false" ||
		annotations["argocd.argoproj.io/compare-options"] == "IgnoreExtraneous"
}
