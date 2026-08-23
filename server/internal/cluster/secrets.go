package cluster

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/erolbeyaz/kubby/internal/store"
)

var secretGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

// SecretKey describes one entry in a secret without disclosing it.
type SecretKey struct {
	Key string `json:"key"`
	// Bytes is the decoded length. It says whether a value is there and roughly what
	// shape it is, which is most of what a reader needs, without being the value.
	Bytes int `json:"bytes"`
}

// SecretKeys lists a secret's keys and their sizes. Values are never included: the
// default for a secret is masked, and disclosure is a separate, audited request (ADR-057).
func (s *Service) SecretKeys(ctx context.Context, cluster *store.Cluster, namespace, name string, impersonate *ImpersonationConfig) ([]SecretKey, error) {
	data, err := s.secretData(ctx, cluster, namespace, name, impersonate)
	if err != nil {
		return nil, err
	}

	keys := make([]SecretKey, 0, len(data))
	for key, encoded := range data {
		decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
		if decodeErr != nil {
			keys = append(keys, SecretKey{Key: key})
			continue
		}
		keys = append(keys, SecretKey{Key: key, Bytes: len(decoded)})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Key < keys[j].Key })
	return keys, nil
}

// RevealSecret returns one key's decoded value.
//
// One key at a time, deliberately: there is no "show everything". Each disclosure is its
// own decision and its own audit record, and a caller that wants five values leaves five
// records rather than one.
func (s *Service) RevealSecret(ctx context.Context, cluster *store.Cluster, namespace, name, key string, impersonate *ImpersonationConfig) (string, error) {
	if key == "" {
		return "", fmt.Errorf("%w: key is required", ErrResourceNotFound)
	}

	data, err := s.secretData(ctx, cluster, namespace, name, impersonate)
	if err != nil {
		return "", err
	}

	encoded, found := data[key]
	if !found {
		return "", fmt.Errorf("%w: no key %q in this secret", ErrResourceNotFound, key)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		// A value that is not valid base64 is returned as stored rather than as an
		// error: the point of looking is to see what is actually there.
		return encoded, nil
	}
	return string(decoded), nil
}

func (s *Service) secretData(ctx context.Context, cluster *store.Cluster, namespace, name string, impersonate *ImpersonationConfig) (map[string]string, error) {
	cfg, err := s.RESTConfigFor(ctx, cluster, impersonate)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	object, err := client.Resource(secretGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, translateAPIError(err, ResourceType{Kind: "Secret", Resource: "secrets"})
	}

	raw, _ := object.Object["data"].(map[string]any)
	data := make(map[string]string, len(raw))
	for key, value := range raw {
		if encoded, ok := value.(string); ok {
			data[key] = encoded
		}
	}
	return data, nil
}
