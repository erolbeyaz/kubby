package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"

	"github.com/erolbeyaz/kubby/internal/store"
)

// restartAnnotation is what kubectl writes to trigger a rollout, and reusing it means a
// restart from Kubby and one from kubectl are the same event to everything downstream.
const restartAnnotation = "kubectl.kubernetes.io/restartedAt"

// What a workload ran before Kubby last scaled it, and what it was scaled to.
//
// On the object rather than in Kubby, because the number has to survive a restart, a
// different reader, and Kubby not being there at all — and because it is then visible in
// the YAML rather than being a fact only this tool knows.
const (
	scaledFromAnnotation = "kubby.io/scaled-from"
	scaledToAnnotation   = "kubby.io/scaled-to"
)

// Scale sets a workload's replica count.
func (s *Service) Scale(ctx context.Context, cluster *store.Cluster, req WriteRequest, replicas int32, impersonate *ImpersonationConfig) error {
	if replicas < 0 {
		return fmt.Errorf("%w: replicas cannot be negative", ErrResourceNotFound)
	}

	// What it was is written onto the object before it is changed, so bringing a set of
	// workloads back up does not mean remembering what each one ran. Kubernetes keeps no
	// such record, and a drill that scales twenty deployments to zero has no other way
	// to restore twenty different numbers. Failing to read the current count is not fatal:
	// the worst it costs is that record.
	previous, err := s.currentReplicas(ctx, cluster, req, impersonate)
	if err != nil {
		previous = 0
	}

	_, err = s.Patch(ctx, cluster, req, scalePatch(previous, replicas), impersonate)
	return err
}

// scalePatch sets the count and records what it was. A previous of zero records nothing:
// a second scale-to-zero must not overwrite the count the first one saved.
func scalePatch(previous int64, replicas int32) []byte {
	annotations := map[string]string{scaledToAnnotation: strconv.Itoa(int(replicas))}
	if previous > 0 {
		annotations[scaledFromAnnotation] = strconv.FormatInt(previous, 10)
	}
	patch, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{"annotations": annotations},
		"spec":     map[string]any{"replicas": replicas},
	})
	return patch
}

// RestoreScale puts a workload back to what it ran before Kubby scaled it.
//
// Reports what it restored to, because "back to normal" is a number the reader wants to
// see rather than take on trust.
func (s *Service) RestoreScale(ctx context.Context, cluster *store.Cluster, req WriteRequest, impersonate *ImpersonationConfig) (int32, error) {
	object, err := s.Get(ctx, cluster, req.Type, req.Namespace, req.Name, impersonate)
	if err != nil {
		return 0, err
	}

	replicas, err := recordedCount(object.GetAnnotations(), req.Namespace+"/"+req.Name)
	if err != nil {
		return 0, err
	}

	patch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
	if _, err := s.Patch(ctx, cluster, req, patch, impersonate); err != nil {
		return 0, err
	}
	return replicas, nil
}

// recordedCount reads back what scalePatch wrote.
func recordedCount(annotations map[string]string, what string) (int32, error) {
	previous, ok := annotations[scaledFromAnnotation]
	if !ok {
		return 0, fmt.Errorf("%w: nothing recorded to restore %s to — it has not been scaled from here",
			ErrRequestRejected, what)
	}

	replicas, err := strconv.Atoi(previous)
	if err != nil || replicas < 0 {
		return 0, fmt.Errorf("%w: %s on %s holds %q, which is not a replica count",
			ErrRequestRejected, scaledFromAnnotation, what, previous)
	}
	return int32(replicas), nil
}

// currentReplicas reads what a workload runs now. A failure is not fatal to a scale: the
// worst it costs is the record of where it came from.
func (s *Service) currentReplicas(ctx context.Context, cluster *store.Cluster, req WriteRequest, impersonate *ImpersonationConfig) (int64, error) {
	object, err := s.Get(ctx, cluster, req.Type, req.Namespace, req.Name, impersonate)
	if err != nil {
		return 0, err
	}
	replicas, found, err := unstructured.NestedInt64(object.Object, "spec", "replicas")
	if err != nil || !found {
		return 0, fmt.Errorf("no replica count on %s/%s", req.Namespace, req.Name)
	}
	return replicas, nil
}

// Restart rolls a workload by stamping its pod template, which is what kubectl does.
func (s *Service) Restart(ctx context.Context, cluster *store.Cluster, req WriteRequest, now time.Time, impersonate *ImpersonationConfig) error {
	stamp := now.UTC().Format(time.RFC3339)
	patch := []byte(fmt.Sprintf(
		`{"spec":{"template":{"metadata":{"annotations":{%q:%q}}}}}`, restartAnnotation, stamp))

	_, err := s.Patch(ctx, cluster, req, patch, impersonate)
	return err
}

// SetSuspended pauses or resumes a CronJob.
func (s *Service) SetSuspended(ctx context.Context, cluster *store.Cluster, req WriteRequest, suspended bool, impersonate *ImpersonationConfig) error {
	patch := []byte(fmt.Sprintf(`{"spec":{"suspend":%t}}`, suspended))
	_, err := s.Patch(ctx, cluster, req, patch, impersonate)
	return err
}

// TriggerCronJob runs a CronJob now by creating a Job from its template, the way
// `kubectl create job --from=cronjob/x` does.
func (s *Service) TriggerCronJob(ctx context.Context, cluster *store.Cluster, namespace, name string, now time.Time, impersonate *ImpersonationConfig) (string, error) {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return "", err
	}

	cronType, err := LookupType("batch/cronjobs")
	if err != nil {
		return "", err
	}
	cron, err := client.Resource(cronType.GVR()).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", translateAPIError(err, cronType)
	}

	template, found, err := unstructured.NestedMap(cron.Object, "spec", "jobTemplate", "spec")
	if err != nil || !found {
		return "", fmt.Errorf("%s %q has no job template", cronType.Kind, name)
	}

	// A manual run is named after the minute it started, which is how it stays
	// distinguishable from the scheduled runs around it.
	jobName := fmt.Sprintf("%s-manual-%s", name, now.UTC().Format("20060102150405"))
	job := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      jobName,
			"namespace": namespace,
			"annotations": map[string]any{
				"cronjob.kubernetes.io/instantiate": "manual",
				"kubby.io/triggered-from":           name,
			},
		},
		"spec": template,
	}}

	jobType, err := LookupType("batch/jobs")
	if err != nil {
		return "", err
	}
	if _, err := client.Resource(jobType.GVR()).Namespace(namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return "", translateWriteError(err, jobType, jobName)
	}
	return jobName, nil
}

// SetUnschedulable cordons or uncordons a node.
func (s *Service) SetUnschedulable(ctx context.Context, cluster *store.Cluster, name string, unschedulable bool, impersonate *ImpersonationConfig) error {
	nodeType, err := LookupType("nodes")
	if err != nil {
		return err
	}

	patch := []byte(fmt.Sprintf(`{"spec":{"unschedulable":%t}}`, unschedulable))
	_, err = s.Patch(ctx, cluster, WriteRequest{Type: nodeType, Name: name}, patch, impersonate)
	return err
}

// EvictResult is one pod's outcome during a drain.
type EvictResult struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Evicted   bool   `json:"evicted"`
	Reason    string `json:"reason,omitempty"`
}

// Evict asks the API server to remove a pod, honouring PodDisruptionBudgets.
//
// Eviction is not deletion: the point is that the cluster gets to say no. A budget that
// refuses is the system working, so the refusal is reported as the reason rather than as
// a failure of the tool.
func (s *Service) Evict(ctx context.Context, cluster *store.Cluster, namespace, name string, impersonate *ImpersonationConfig) error {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("create client: %w", err)
	}

	eviction := &policyv1.Eviction{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	if err := client.PolicyV1().Evictions(namespace).Evict(ctx, eviction); err != nil {
		if apierrors.IsTooManyRequests(err) {
			return fmt.Errorf("a PodDisruptionBudget is holding this pod: %s", err.Error())
		}
		return translateWriteError(err, ResourceType{Kind: "Pod", Resource: "pods"}, name)
	}
	return nil
}

// Revision is one entry in a workload's rollout history.
type Revision struct {
	Number   int64             `json:"number"`
	Created  string            `json:"created"`
	Images   []string          `json:"images"`
	Cause    string            `json:"cause,omitempty"`
	Current  bool              `json:"current"`
	Selector map[string]string `json:"-"`
}

var replicaSetGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}

// RolloutHistory lists a Deployment's revisions, newest first.
//
// Kubernetes keeps no history object: the revisions are the ReplicaSets the Deployment
// left behind, each stamped with the revision it belonged to.
func (s *Service) RolloutHistory(ctx context.Context, cluster *store.Cluster, namespace, name string, impersonate *ImpersonationConfig) ([]Revision, error) {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}

	deployType, err := LookupType("apps/deployments")
	if err != nil {
		return nil, err
	}
	deployment, err := client.Resource(deployType.GVR()).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, translateAPIError(err, deployType)
	}
	currentRevision := deployment.GetAnnotations()["deployment.kubernetes.io/revision"]

	sets, err := client.Resource(replicaSetGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, translateAPIError(err, ResourceType{Kind: "ReplicaSet", Resource: "replicasets"})
	}

	var revisions []Revision
	for i := range sets.Items {
		set := &sets.Items[i]
		if ownerOfName(set) != name {
			continue
		}
		annotations := set.GetAnnotations()
		number, err := strconv.ParseInt(annotations["deployment.kubernetes.io/revision"], 10, 64)
		if err != nil {
			continue
		}
		revisions = append(revisions, Revision{
			Number:  number,
			Created: set.GetCreationTimestamp().UTC().Format(time.RFC3339),
			Images:  imagesOf(set),
			Cause:   annotations["kubernetes.io/change-cause"],
			Current: annotations["deployment.kubernetes.io/revision"] == currentRevision,
		})
	}
	sort.Slice(revisions, func(i, j int) bool { return revisions[i].Number > revisions[j].Number })
	return revisions, nil
}

// Rollback returns a Deployment to an earlier revision by reapplying that revision's pod
// template, which is what `kubectl rollout undo` does.
func (s *Service) Rollback(ctx context.Context, cluster *store.Cluster, namespace, name string, revision int64, impersonate *ImpersonationConfig) error {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return err
	}

	sets, err := client.Resource(replicaSetGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return translateAPIError(err, ResourceType{Kind: "ReplicaSet", Resource: "replicasets"})
	}

	var wanted *unstructured.Unstructured
	for i := range sets.Items {
		set := &sets.Items[i]
		if ownerOfName(set) != name {
			continue
		}
		if set.GetAnnotations()["deployment.kubernetes.io/revision"] == strconv.FormatInt(revision, 10) {
			wanted = set
			break
		}
	}
	if wanted == nil {
		return fmt.Errorf("%w: revision %d of %q", ErrResourceNotFound, revision, name)
	}

	template, found, err := unstructured.NestedMap(wanted.Object, "spec", "template")
	if err != nil || !found {
		return fmt.Errorf("revision %d has no pod template to return to", revision)
	}
	// The ReplicaSet's template carries a hash label the Deployment adds per revision;
	// sending it back would pin the new rollout to the old revision's identity.
	unstructured.RemoveNestedField(template, "metadata", "labels", "pod-template-hash")

	patch, err := json.Marshal(map[string]any{"spec": map[string]any{"template": template}})
	if err != nil {
		return err
	}

	deployType, err := LookupType("apps/deployments")
	if err != nil {
		return err
	}
	_, err = s.Patch(ctx, cluster, WriteRequest{Type: deployType, Namespace: namespace, Name: name}, patch, impersonate)
	return err
}

func ownerOfName(obj *unstructured.Unstructured) string {
	for _, owner := range obj.GetOwnerReferences() {
		if owner.Controller != nil && *owner.Controller {
			return owner.Name
		}
	}
	return ""
}

func imagesOf(obj *unstructured.Unstructured) []string {
	containers, _, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")

	images := make([]string, 0, len(containers))
	for _, raw := range containers {
		container, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if image, ok := container["image"].(string); ok {
			images = append(images, image)
		}
	}
	return images
}
