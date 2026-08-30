package cluster

import (
	"encoding/json"
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func decodePatch(t *testing.T, patch []byte) (map[string]string, int64) {
	t.Helper()
	var decoded struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			Replicas int64 `json:"replicas"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(patch, &decoded); err != nil {
		t.Fatalf("patch is not valid JSON (%s): %v", patch, err)
	}
	return decoded.Metadata.Annotations, decoded.Spec.Replicas
}

func TestScalePatchRecordsWhatItIsReplacing(t *testing.T) {
	annotations, replicas := decodePatch(t, scalePatch(3, 0))

	if replicas != 0 {
		t.Errorf("replicas = %d, want 0", replicas)
	}
	if got := annotations[scaledFromAnnotation]; got != "3" {
		t.Errorf("%s = %q, want \"3\"", scaledFromAnnotation, got)
	}
	if got := annotations[scaledToAnnotation]; got != "0" {
		t.Errorf("%s = %q, want \"0\"", scaledToAnnotation, got)
	}
}

// The failure this guards against loses a whole drill: scale twenty deployments to zero,
// scale them to zero again by accident, and every recorded count is now zero. Restore
// then brings the cluster back with nothing running.
func TestScalePatchDoesNotForgetTheOriginalCountOnASecondScaleToZero(t *testing.T) {
	annotations, _ := decodePatch(t, scalePatch(0, 0))

	if _, recorded := annotations[scaledFromAnnotation]; recorded {
		t.Errorf("%s was overwritten with a zero: %v", scaledFromAnnotation, annotations)
	}
}

func TestRecordedCount(t *testing.T) {
	cases := []struct {
		name        string
		annotations map[string]string
		want        int32
		wantErr     bool
	}{
		{"scaled from here", map[string]string{scaledFromAnnotation: "5"}, 5, false},
		{"never scaled from here", map[string]string{}, 0, true},
		{"hand-edited to nonsense", map[string]string{scaledFromAnnotation: "lots"}, 0, true},
		{"hand-edited to a negative", map[string]string{scaledFromAnnotation: "-1"}, 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := recordedCount(tc.annotations, "payments/api")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("got %d, want an error", got)
				}
				// The caller turns this into a message a reader has to act on, so it has
				// to arrive as a rejection rather than as a failure to reach the cluster.
				if !errors.Is(err, ErrRequestRejected) {
					t.Errorf("error = %v, want it to wrap ErrRequestRejected", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("recordedCount: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// The dialog cannot show what it is about to restore a workload to unless the row carries
// it, and the row only carries what the projection puts there.
func TestScalableRowsCarryTheRecordedCount(t *testing.T) {
	for _, kind := range []string{"Deployment", "StatefulSet", "ReplicaSet"} {
		t.Run(kind, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "apps/v1", "kind": kind,
				"metadata": map[string]any{
					"name": "shop-web", "namespace": "storefront",
					"annotations": map[string]any{scaledFromAnnotation: "4", scaledToAnnotation: "0"},
				},
				"spec":   map[string]any{"replicas": int64(0)},
				"status": map[string]any{"readyReplicas": int64(0)},
			}}

			fields, _ := projectors[kind].project(obj)
			if fields["scaledFrom"] != "4" {
				t.Errorf("scaledFrom = %q, want \"4\"", fields["scaledFrom"])
			}

			// One Kubby has never scaled must arrive with nothing rather than a zero the
			// dialog would show as a real target.
			obj.SetAnnotations(map[string]string{})
			fields, _ = projectors[kind].project(obj)
			if recorded, present := fields["scaledFrom"]; present {
				t.Errorf("a workload never scaled from here reported %q", recorded)
			}
		})
	}
}
