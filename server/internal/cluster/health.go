package cluster

import (
	"context"
	"errors"
	"fmt"
	"time"

	authzv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// CredentialStatus mirrors the values stored on a cluster row.
const (
	StatusValid       = "valid"
	StatusInvalid     = "invalid"
	StatusUnreachable = "unreachable"
)

// Health is what a probe learned about a cluster.
type Health struct {
	Status           string
	Detail           string
	K8sVersion       string
	NodeCount        *int
	MetricsAvailable bool
	// Permissions summarises what the credential may do, shown before the user commits
	// to saving it (ADR-018).
	Permissions []string
}

// Probe checks a cluster and classifies the outcome.
//
// The distinction that matters: 401 means the credential itself is no longer good and
// the cluster must be flagged for re-authentication, while a network failure means the
// cluster is merely unreachable and the credential may still be fine. Collapsing the
// two into one error is what makes an expired token look like an outage.
func Probe(ctx context.Context, cfg *rest.Config, timeout time.Duration) Health {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := Clientset(cfg)
	if err != nil {
		return Health{Status: StatusUnreachable, Detail: err.Error()}
	}

	version, err := client.Discovery().ServerVersion()
	if err != nil {
		return classifyFailure(err)
	}

	health := Health{
		Status:     StatusValid,
		K8sVersion: version.GitVersion,
	}

	nodes, err := client.CoreV1().Nodes().List(probeCtx, metav1.ListOptions{Limit: 500})
	switch {
	case err == nil:
		count := len(nodes.Items)
		health.NodeCount = &count
	case apierrors.IsForbidden(err):
		// A credential scoped to a namespace is legitimate; it just cannot count nodes.
		health.Detail = "the credential cannot list nodes"
	default:
		return classifyFailure(err)
	}

	health.MetricsAvailable = metricsAvailable(probeCtx, client)
	health.Permissions = summarisePermissions(probeCtx, client)
	return health
}

// classifyFailure separates a rejected credential from an unreachable endpoint.
func classifyFailure(err error) Health {
	switch {
	case apierrors.IsUnauthorized(err):
		return Health{
			Status: StatusInvalid,
			Detail: "the credential was rejected; the token may have expired or been revoked",
		}
	case apierrors.IsForbidden(err):
		return Health{
			Status: StatusInvalid,
			Detail: "the credential is not permitted to read basic cluster information",
		}
	default:
		return Health{Status: StatusUnreachable, Detail: unwrapMessage(err)}
	}
}

// metricsAvailable reports whether metrics.k8s.io is served, so the UI can degrade
// gracefully instead of showing an error where usage figures belong (ADR-007).
func metricsAvailable(ctx context.Context, client kubernetes.Interface) bool {
	groups, err := client.Discovery().ServerGroups()
	if err != nil {
		return false
	}
	for _, group := range groups.Groups {
		if group.Name == "metrics.k8s.io" {
			return true
		}
	}
	return false
}

// verbsToCheck are the capabilities worth showing before a credential is saved.
var verbsToCheck = []struct {
	label    string
	group    string
	resource string
	verb     string
}{
	{"list pods", "", "pods", "list"},
	{"read pod logs", "", "pods/log", "get"},
	{"exec into pods", "", "pods/exec", "create"},
	{"list nodes", "", "nodes", "list"},
	{"list secrets", "", "secrets", "list"},
	{"edit deployments", "apps", "deployments", "update"},
	{"delete pods", "", "pods", "delete"},
	{"impersonate users", "", "users", "impersonate"},
}

// summarisePermissions asks the API server what this credential may do, so the user
// sees the real capability rather than assuming cluster-admin.
func summarisePermissions(ctx context.Context, client kubernetes.Interface) []string {
	var allowed []string

	for _, check := range verbsToCheck {
		review := &authzv1.SelfSubjectAccessReview{
			Spec: authzv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authzv1.ResourceAttributes{
					Group:    check.group,
					Resource: check.resource,
					Verb:     check.verb,
				},
			},
		}
		result, err := client.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
		if err != nil {
			// The review API itself may be unavailable; report nothing rather than
			// claiming a capability that was never confirmed.
			return allowed
		}
		if result.Status.Allowed {
			allowed = append(allowed, check.label)
		}
	}
	return allowed
}

func unwrapMessage(err error) string {
	var statusErr *apierrors.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.ErrStatus.Message
	}
	return fmt.Sprint(err)
}
