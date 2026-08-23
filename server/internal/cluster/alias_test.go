package cluster_test

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/erolbeyaz/kubby/internal/store"
)

// storeCluster keeps the test signatures short.
type storeCluster = store.Cluster

func unstructuredNested(obj map[string]any, path ...string) (any, bool, error) {
	return unstructured.NestedFieldNoCopy(obj, path...)
}
