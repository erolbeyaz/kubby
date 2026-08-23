package cluster

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"github.com/erolbeyaz/kubby/internal/store"
)

// ErrConflict means the object changed since it was read.
var ErrConflict = errors.New("this object changed while you were editing it")

// ApplyRequest is one object the caller wants written.
type ApplyRequest struct {
	Type   ResourceType
	Object *unstructured.Unstructured
	// DryRun asks the API server to validate and report without persisting.
	DryRun bool
}

// ApplyResult is what a write produced, or would have produced.
type ApplyResult struct {
	// Diff is the change the server says it would make, line by line.
	Diff []DiffLine `json:"diff"`
	// Object is what the server returned. On a dry run it is what it would have stored.
	Object *unstructured.Unstructured `json:"-"`
	// Unchanged is true when the server's result equals what is already stored.
	Unchanged bool `json:"unchanged"`
}

// Apply sends an object, optionally as a dry run.
//
// Every write goes through a dry run first (ADR-011). The point is not validation — the
// real write validates too — but that the person pressing the button sees the change the
// server will actually make, which is rarely exactly the change they typed: defaults are
// filled in, webhooks rewrite fields, and immutable values are rejected.
func (s *Service) Apply(ctx context.Context, cluster *store.Cluster, req ApplyRequest, impersonate *ImpersonationConfig) (*ApplyResult, error) {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}

	namespace := req.Object.GetNamespace()
	name := req.Object.GetName()
	if name == "" {
		return nil, fmt.Errorf("%w: the manifest has no metadata.name", ErrResourceNotFound)
	}

	resource := client.Resource(req.Type.GVR()).Namespace(namespace)

	current, err := resource.Get(ctx, name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, translateAPIError(err, req.Type)
	}

	options := metav1.UpdateOptions{}
	createOptions := metav1.CreateOptions{}
	if req.DryRun {
		options.DryRun = []string{metav1.DryRunAll}
		createOptions.DryRun = []string{metav1.DryRunAll}
	}

	var result *unstructured.Unstructured
	if current == nil {
		result, err = resource.Create(ctx, req.Object, createOptions)
	} else {
		// Carrying the stored resourceVersion is what makes the server reject a write
		// that would silently overwrite someone else's (ADR-011).
		if req.Object.GetResourceVersion() == "" {
			req.Object.SetResourceVersion(current.GetResourceVersion())
		}
		result, err = resource.Update(ctx, req.Object, options)
	}
	if err != nil {
		if apierrors.IsConflict(err) {
			return nil, fmt.Errorf("%w: reload it and reapply your change", ErrConflict)
		}
		return nil, translateWriteError(err, req.Type, name)
	}

	diff := Diff(current, result)
	return &ApplyResult{Diff: diff, Object: result, Unchanged: len(diff) == 0}, nil
}

// DeleteRequest is one object to remove.
type DeleteRequest struct {
	Type      ResourceType
	Namespace string
	Name      string
	// Propagation is Background by default: it is the behaviour someone who does not know
	// the difference should get, and the one kubectl uses.
	Propagation metav1.DeletionPropagation
}

// Delete removes one object.
func (s *Service) Delete(ctx context.Context, cluster *store.Cluster, req DeleteRequest, impersonate *ImpersonationConfig) error {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return err
	}

	propagation := req.Propagation
	if propagation == "" {
		propagation = metav1.DeletePropagationBackground
	}

	err = client.Resource(req.Type.GVR()).Namespace(req.Namespace).
		Delete(ctx, req.Name, metav1.DeleteOptions{PropagationPolicy: &propagation})
	if err != nil {
		return translateWriteError(err, req.Type, req.Name)
	}
	return nil
}

// Patch applies a merge patch, for the small structured changes — scale, suspend, a
// restart annotation — that do not need the whole object sent back.
func (s *Service) Patch(ctx context.Context, cluster *store.Cluster, req WriteRequest, patch []byte, impersonate *ImpersonationConfig) (*unstructured.Unstructured, error) {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}

	result, err := client.Resource(req.Type.GVR()).Namespace(req.Namespace).
		Patch(ctx, req.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return nil, translateWriteError(err, req.Type, req.Name)
	}
	return result, nil
}

func (s *Service) dynamicFor(ctx context.Context, cluster *store.Cluster, impersonate *ImpersonationConfig) (dynamic.Interface, error) {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}
	return client, nil
}
