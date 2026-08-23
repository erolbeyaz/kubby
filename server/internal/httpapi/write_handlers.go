package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/erolbeyaz/kubby/internal/audit"
	"github.com/erolbeyaz/kubby/internal/cluster"
	"github.com/erolbeyaz/kubby/internal/rbac"
	"github.com/erolbeyaz/kubby/internal/store"
)

// writeContext is what every mutating handler needs before it can act.
type writeContext struct {
	cluster  *store.Cluster
	resource cluster.ResourceType
	verdict  *cluster.WriteVerdict
}

// authoriseWrite runs the whole pre-flight chain and answers the request itself when the
// answer is no.
//
// Nothing that writes may skip this. A control that can be bypassed by adding a handler
// is not a control, and this tool holds credentials that can empty a cluster.
func (h *resourceHandlers) authoriseWrite(w http.ResponseWriter, r *http.Request, resourceType cluster.ResourceType, namespace, name string, verb cluster.Verb) (*writeContext, bool) {
	c, ok := h.resolveCluster(w, r)
	if !ok {
		return nil, false
	}

	_, user := principal(r)
	verdict, err := h.svc.CheckWrite(r.Context(), c,
		cluster.WriteRequest{Type: resourceType, Namespace: namespace, Name: name, Verb: verb},
		cluster.Permission{
			GlobalReadOnly: h.readOnly,
			MayWrite:       rbac.Role(user.Role).Can(rbac.PermClusterWrite),
		},
		impersonationFor(r, c),
	)
	if err != nil {
		writeResourceError(w, r, err)
		return nil, false
	}
	if !verdict.Allowed {
		// 403 and the reason: "forbidden" alone leaves the reader guessing which of four
		// gates stopped them.
		writeError(w, r, http.StatusForbidden, verdict.Reason)
		return nil, false
	}
	return &writeContext{cluster: c, resource: resourceType, verdict: verdict}, true
}

// ---------------------------------------------------------------- apply

type applyBody struct {
	Manifest string `json:"manifest"`
	DryRun   bool   `json:"dryRun"`
}

// applyObject validates or writes a manifest.
//
// A dry run is a read of what would happen, so it is offered to anyone who may write and
// costs the cluster nothing; the real write follows the same path with the flag off.
func (h *resourceHandlers) applyObject(w http.ResponseWriter, r *http.Request) {
	var body applyBody
	if !decodeJSON(w, r, &body) {
		return
	}

	documents, err := splitManifest(body.Manifest)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if len(documents) == 0 {
		writeError(w, r, http.StatusBadRequest, "the manifest is empty")
		return
	}

	typeKey := strings.Trim(chi.URLParam(r, "*"), "/")
	results := make([]map[string]any, 0, len(documents))

	for _, object := range documents {
		// A pasted manifest names its own kind; a route key only exists when the write
		// came from an object already on screen.
		resourceType, err := typeOf(typeKey, object)
		if err != nil {
			results = append(results, map[string]any{
				"kind": object.GetKind(), "name": object.GetName(), "error": err.Error(),
			})
			continue
		}

		ctx, ok := h.authoriseWrite(w, r, resourceType, object.GetNamespace(), object.GetName(), cluster.VerbUpdate)
		if !ok {
			return
		}

		result, err := h.svc.Apply(r.Context(), ctx.cluster, cluster.ApplyRequest{
			Type:   ctx.resource,
			Object: object,
			DryRun: body.DryRun,
		}, impersonationFor(r, ctx.cluster))

		entry := map[string]any{
			"kind":      object.GetKind(),
			"name":      object.GetName(),
			"namespace": object.GetNamespace(),
		}
		if err != nil {
			// One rejected document does not invalidate the others' results: saying which
			// one failed is the whole value of applying a multi-document manifest here.
			entry["error"] = err.Error()
			results = append(results, entry)
			if !body.DryRun {
				h.recordWrite(r, ctx, audit.ActionObjectApplied, object.GetNamespace(), object.GetName(), audit.ResultError, nil)
			}
			continue
		}

		entry["diff"] = result.Diff
		entry["unchanged"] = result.Unchanged
		if owner := cluster.OwnerOf(object); owner != nil {
			entry["owner"] = owner
		}
		results = append(results, entry)

		if !body.DryRun {
			h.recordWrite(r, ctx, audit.ActionObjectApplied, object.GetNamespace(), object.GetName(),
				audit.ResultSuccess, cluster.OwnerOf(object))
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results, "dryRun": body.DryRun})
}

// splitManifest reads one or many YAML documents.
func splitManifest(manifest string) ([]*unstructured.Unstructured, error) {
	var out []*unstructured.Unstructured

	for _, chunk := range strings.Split(manifest, "\n---") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(chunk), "---"))
		if trimmed == "" {
			continue
		}

		var parsed map[string]any
		if err := yaml.Unmarshal([]byte(trimmed), &parsed); err != nil {
			return nil, errors.New("the manifest is not valid YAML: " + err.Error())
		}
		if len(parsed) == 0 {
			continue
		}

		object := &unstructured.Unstructured{Object: parsed}
		if object.GetKind() == "" || object.GetAPIVersion() == "" {
			return nil, errors.New("every document needs an apiVersion and a kind")
		}
		out = append(out, object)
	}
	return out, nil
}

// ---------------------------------------------------------------- delete

func (h *resourceHandlers) deleteObject(w http.ResponseWriter, r *http.Request) {
	typeKey := strings.Trim(chi.URLParam(r, "*"), "/")
	namespace := r.URL.Query().Get("namespace")
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, r, http.StatusBadRequest, "name is required")
		return
	}

	resourceType, err := cluster.LookupType(typeKey)
	if err != nil {
		writeError(w, r, http.StatusNotFound, "unknown resource type")
		return
	}

	ctx, ok := h.authoriseWrite(w, r, resourceType, namespace, name, cluster.VerbDelete)
	if !ok {
		return
	}

	err = h.svc.Delete(r.Context(), ctx.cluster, cluster.DeleteRequest{
		Type:        ctx.resource,
		Namespace:   namespace,
		Name:        name,
		Propagation: propagationFrom(r.URL.Query().Get("propagation")),
	}, impersonationFor(r, ctx.cluster))

	if err != nil {
		h.recordWrite(r, ctx, audit.ActionObjectDeleted, namespace, name, audit.ResultError, nil)
		writeResourceError(w, r, err)
		return
	}

	h.recordWrite(r, ctx, audit.ActionObjectDeleted, namespace, name, audit.ResultSuccess, nil)
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// propagationFrom defaults to Background: it is what kubectl does, and the right answer
// for someone who does not know the difference.
func propagationFrom(raw string) metav1.DeletionPropagation {
	switch raw {
	case "Foreground":
		return metav1.DeletePropagationForeground
	case "Orphan":
		return metav1.DeletePropagationOrphan
	default:
		return metav1.DeletePropagationBackground
	}
}

func (h *resourceHandlers) recordWrite(r *http.Request, ctx *writeContext, action, namespace, name, result string, owner *cluster.GitOpsOwner) {
	if h.audit == nil {
		return
	}
	_, user := principal(r)

	details := map[string]any{}
	if owner != nil {
		// A change to a GitOps-managed object may be reverted minutes later, and the
		// record has to say so or the audit trail contradicts the cluster (ADR-028).
		details["gitops_managed"] = true
		details["gitops_controller"] = owner.Controller
		details["gitops_instance"] = owner.Instance
		details["gitops_self_heal"] = owner.SelfHeal
	}

	h.audit.Record(r.Context(), audit.Event{
		Action:       action,
		Result:       result,
		ActorID:      &user.ID,
		ActorEmail:   user.Email,
		ClusterID:    &ctx.cluster.ID,
		Namespace:    namespace,
		ResourceKind: ctx.resource.Kind,
		ResourceName: name,
		Details:      details,
	})
}

// typeOf resolves which kind is being written: the route says so when the object is
// already on screen, and the manifest says so when it was pasted.
func typeOf(typeKey string, object *unstructured.Unstructured) (cluster.ResourceType, error) {
	if typeKey != "" {
		return cluster.LookupType(typeKey)
	}
	return cluster.LookupKind(object.GroupVersionKind())
}
