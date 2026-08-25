package cluster

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/erolbeyaz/kubby/internal/store"
)

// HelmRelease is one installed chart.
type HelmRelease struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Revision   int    `json:"revision"`
	Status     string `json:"status"`
	Chart      string `json:"chart,omitempty"`
	Version    string `json:"chartVersion,omitempty"`
	AppVersion string `json:"appVersion,omitempty"`
	Updated    string `json:"updated,omitempty"`
	// Description is Helm's own note for the revision — "Install complete", "Upgrade
	// complete", or the reason a rollback happened.
	Description string `json:"description,omitempty"`
}

// HelmReleaseDetail adds what is only worth fetching for one release.
type HelmReleaseDetail struct {
	HelmRelease
	// Values is what the release was installed with, which is the question people
	// actually open a release to answer.
	Values map[string]any `json:"values,omitempty"`
	Notes  string         `json:"notes,omitempty"`
	// History is every revision still retained, newest first.
	History []HelmRelease `json:"history,omitempty"`
}

// ListHelmReleases reads the releases installed in a cluster.
//
// Read from the API server rather than by running the helm binary. Kubby reads Kubernetes
// through client-go everywhere else and this is no different: shelling out would mean a
// second credential path, a second set of failure modes, and an answer that depends on
// which helm happens to be on PATH.
//
// Helm keeps a release as a Secret per revision, labelled `owner=helm`. Everything the
// list needs is in the labels; the chart's own name and version need the payload, which
// is base64 twice and then gzipped.
func (s *Service) ListHelmReleases(ctx context.Context, cluster *store.Cluster, namespace string, impersonate *ImpersonationConfig) ([]HelmRelease, error) {
	secrets, err := s.helmSecrets(ctx, cluster, namespace, "", impersonate)
	if err != nil {
		return nil, err
	}

	// Only the newest revision of each release: older ones are the history, and listing
	// them all would show a chart upgraded ten times as ten installations.
	newest := map[string]HelmRelease{}
	for i := range secrets {
		release := releaseFromSecret(&secrets[i])
		if release.Name == "" {
			continue
		}
		key := release.Namespace + "/" + release.Name
		if existing, seen := newest[key]; !seen || release.Revision > existing.Revision {
			newest[key] = release
		}
	}

	out := make([]HelmRelease, 0, len(newest))
	for _, release := range newest {
		out = append(out, release)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// HelmReleaseDetails reads one release, its values and its history.
func (s *Service) HelmReleaseDetails(ctx context.Context, cluster *store.Cluster, namespace, name string, impersonate *ImpersonationConfig) (*HelmReleaseDetail, error) {
	secrets, err := s.helmSecrets(ctx, cluster, namespace, name, impersonate)
	if err != nil {
		return nil, err
	}
	if len(secrets) == 0 {
		return nil, fmt.Errorf("no Helm release named %q in %s", name, namespace)
	}

	var (
		out     HelmReleaseDetail
		history []HelmRelease
	)

	for i := range secrets {
		release := releaseFromSecret(&secrets[i])
		if release.Name != name {
			continue
		}
		history = append(history, release)

		if release.Revision < out.Revision {
			continue
		}

		payload, err := decodeRelease(&secrets[i])
		if err != nil {
			// The labels still describe the release; only the values and notes are lost.
			out.HelmRelease = release
			continue
		}
		out.HelmRelease = release
		out.Values, _ = payload["config"].(map[string]any)
		if info, ok := payload["info"].(map[string]any); ok {
			out.Notes, _ = info["notes"].(string)
		}
	}

	sort.Slice(history, func(i, j int) bool { return history[i].Revision > history[j].Revision })
	out.History = history
	return &out, nil
}

func (s *Service) helmSecrets(ctx context.Context, cluster *store.Cluster, namespace, name string, impersonate *ImpersonationConfig) ([]unstructured.Unstructured, error) {
	client, err := s.dynamicFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}

	secretType, err := LookupType("secrets")
	if err != nil {
		return nil, err
	}

	selector := "owner=helm"
	if name != "" {
		selector += ",name=" + name
	}

	list, err := client.Resource(secretType.GVR()).Namespace(namespace).
		List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, translateAPIError(err, secretType)
	}
	return list.Items, nil
}

// releaseFromSecret reads what the labels carry, which is everything the list shows
// except the chart's own name and version.
func releaseFromSecret(secret *unstructured.Unstructured) HelmRelease {
	labels := secret.GetLabels()

	release := HelmRelease{
		Name:      labels["name"],
		Namespace: secret.GetNamespace(),
		Status:    labels["status"],
	}
	release.Revision, _ = strconv.Atoi(labels["version"])

	if modified := labels["modifiedAt"]; modified != "" {
		if seconds, err := strconv.ParseInt(modified, 10, 64); err == nil {
			// UTC and RFC 3339, converted for display only in the browser (ADR-026).
			release.Updated = time.Unix(seconds, 0).UTC().Format(time.RFC3339)
		}
	}

	// The chart's identity is only in the payload, so a decode failure costs the chart
	// name rather than the whole row.
	if payload, err := decodeRelease(secret); err == nil {
		if chart, ok := payload["chart"].(map[string]any); ok {
			if metadata, ok := chart["metadata"].(map[string]any); ok {
				release.Chart, _ = metadata["name"].(string)
				release.Version, _ = metadata["version"].(string)
				release.AppVersion, _ = metadata["appVersion"].(string)
			}
		}
		if info, ok := payload["info"].(map[string]any); ok {
			release.Description, _ = info["description"].(string)
		}
	}
	return release
}

// decodeRelease unwraps Helm's storage format: the Secret's value is base64 (which the
// API client has already undone), then base64 again, then gzip, then JSON.
func decodeRelease(secret *unstructured.Unstructured) (map[string]any, error) {
	encoded, found, err := unstructured.NestedString(secret.Object, "data", "release")
	if err != nil || !found {
		return nil, fmt.Errorf("the release secret carries no payload")
	}

	outer, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode the release: %w", err)
	}
	inner, err := base64.StdEncoding.DecodeString(string(outer))
	if err != nil {
		// Helm 2 stored it base64 once. Falling back rather than failing means an old
		// release still shows its chart.
		inner = outer
	}

	reader, err := gzip.NewReader(bytes.NewReader(inner))
	if err != nil {
		// Not every release is compressed.
		return unmarshalRelease(inner)
	}
	defer func() { _ = reader.Close() }()

	// Bounded: this is attacker-influenced input in the sense that anyone who can write a
	// Secret in the namespace controls it, and an unbounded gzip is a way to exhaust
	// memory from inside the cluster.
	plain, err := io.ReadAll(io.LimitReader(reader, maxReleaseBytes))
	if err != nil {
		return nil, fmt.Errorf("decompress the release: %w", err)
	}
	return unmarshalRelease(plain)
}

func unmarshalRelease(raw []byte) (map[string]any, error) {
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("the release payload is not JSON: %w", err)
	}
	return out, nil
}

// A rendered manifest for a large chart is big; anything past this is not a release.
const maxReleaseBytes = 32 << 20
