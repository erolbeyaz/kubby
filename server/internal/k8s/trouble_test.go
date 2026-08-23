package k8s

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func pod(status map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": "probe", "namespace": "payments"},
		"status":     status,
	}}
}

// "Pending" and "Failed" are the question, not the answer. Every one of these asserts
// that the answer comes back with them.
func TestPodTroubleNamesTheActualCause(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     map[string]any
		wantReason string
		wantDetail string
	}{
		{
			name: "image cannot be pulled",
			status: map[string]any{
				"phase": "Pending",
				"containerStatuses": []any{map[string]any{
					"name": "app",
					"state": map[string]any{"waiting": map[string]any{
						"reason":  "ImagePullBackOff",
						"message": `Back-off pulling image "registry.invalid/nope:1.0"`,
					}},
				}},
			},
			wantReason: "ImagePullBackOff",
			wantDetail: "registry.invalid/nope:1.0",
		},
		{
			name: "nothing can schedule it",
			status: map[string]any{
				"phase": "Pending",
				"conditions": []any{map[string]any{
					"type": "PodScheduled", "status": "False", "reason": "Unschedulable",
					"message": "0/3 nodes are available: insufficient cpu.",
				}},
			},
			wantReason: "Unschedulable",
			wantDetail: "insufficient cpu",
		},
		{
			name: "waiting on a volume",
			status: map[string]any{
				"phase": "Pending",
				"conditions": []any{map[string]any{
					"type": "PodScheduled", "status": "False", "reason": "Unschedulable",
					"message": "pod has unbound immediate PersistentVolumeClaims",
				}},
			},
			wantReason: "Unschedulable",
			wantDetail: "unbound immediate PersistentVolumeClaims",
		},
		{
			name: "killed for memory while running",
			status: map[string]any{
				"phase": "Running",
				"containerStatuses": []any{map[string]any{
					"name": "app", "restartCount": int64(3),
					"state":     map[string]any{"running": map[string]any{}},
					"lastState": map[string]any{"terminated": map[string]any{"reason": "OOMKilled", "exitCode": int64(137)}},
				}},
			},
			wantReason: "OOMKilled",
			wantDetail: "memory limit",
		},
		{
			name: "the application itself failed",
			status: map[string]any{
				"phase": "Running",
				"containerStatuses": []any{map[string]any{
					"name": "app",
					"state": map[string]any{"waiting": map[string]any{
						"reason": "CrashLoopBackOff",
					}},
				}},
			},
			wantReason: "CrashLoopBackOff",
			wantDetail: "keeps exiting",
		},
		{
			name: "a missing ConfigMap or Secret",
			status: map[string]any{
				"phase": "Pending",
				"containerStatuses": []any{map[string]any{
					"name": "app",
					"state": map[string]any{"waiting": map[string]any{
						"reason": "CreateContainerConfigError",
					}},
				}},
			},
			wantReason: "CreateContainerConfigError",
			wantDetail: "ConfigMap, Secret or key is missing",
		},
		{
			name: "the node threw it out",
			status: map[string]any{
				"phase": "Failed", "reason": "Evicted",
				"message": "The node was low on resource: memory.",
			},
			wantReason: "Evicted",
			wantDetail: "low on resource",
		},
		{
			name: "running but not serving",
			status: map[string]any{
				"phase": "Running",
				"conditions": []any{map[string]any{
					"type": "Ready", "status": "False",
					"message": "containers with unready status: [app]",
				}},
			},
			wantReason: "NotReady",
			wantDetail: "unready status",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trouble := PodTrouble(pod(tc.status))

			if trouble == nil {
				t.Fatal("no trouble reported")
			}
			if trouble.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", trouble.Reason, tc.wantReason)
			}
			if !strings.Contains(trouble.Detail, tc.wantDetail) {
				t.Errorf("detail = %q, want it to mention %q", trouble.Detail, tc.wantDetail)
			}
			if trouble.Severity == "" {
				t.Error("trouble with no severity cannot be coloured")
			}
		})
	}
}

// Every rollout puts pods through ContainerCreating; marking it would fill the list with
// warnings and teach the reader to ignore them.
func TestStartingIsNotTrouble(t *testing.T) {
	trouble := PodTrouble(pod(map[string]any{
		"phase": "Pending",
		"containerStatuses": []any{map[string]any{
			"name":  "app",
			"state": map[string]any{"waiting": map[string]any{"reason": "ContainerCreating"}},
		}},
	}))

	if trouble != nil && trouble.Reason != "Pending" {
		t.Fatalf("trouble = %+v, want nothing specific", trouble)
	}
}

func TestHealthyPodHasNoTrouble(t *testing.T) {
	trouble := PodTrouble(pod(map[string]any{
		"phase":      "Running",
		"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
		"containerStatuses": []any{map[string]any{
			"name": "app", "state": map[string]any{"running": map[string]any{}},
		}},
	}))

	if trouble != nil {
		t.Fatalf("trouble = %+v, want none", trouble)
	}
}

func TestCompletedPodIsNotTrouble(t *testing.T) {
	trouble := PodTrouble(pod(map[string]any{
		"phase": "Succeeded",
		"containerStatuses": []any{map[string]any{
			"name":  "worker",
			"state": map[string]any{"terminated": map[string]any{"reason": "Completed", "exitCode": int64(0)}},
		}},
	}))

	if trouble != nil {
		t.Fatalf("trouble = %+v, want none", trouble)
	}
}

// kubectl's STATUS column is not the pod's phase, and the difference is the whole point:
// a crash-looping pod has phase "Running".
func TestPodStatusPrefersTheContainerOverThePhase(t *testing.T) {
	status := PodStatus(pod(map[string]any{
		"phase": "Running",
		"containerStatuses": []any{map[string]any{
			"name":  "app",
			"state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}},
		}},
	}))

	if status != "CrashLoopBackOff" {
		t.Fatalf("status = %q, want the container's own state", status)
	}
}

func TestPodStatusKeepsThePhaseWhenNothingIsWrong(t *testing.T) {
	status := PodStatus(pod(map[string]any{
		"phase": "Running",
		"containerStatuses": []any{map[string]any{
			"name": "app", "state": map[string]any{"running": map[string]any{}},
		}},
	}))

	if status != "Running" {
		t.Fatalf("status = %q, want Running", status)
	}
}

// While an init container is failing, nothing else has started, so its state is the
// pod's state.
func TestPodStatusPrefersAFailingInitContainer(t *testing.T) {
	status := PodStatus(pod(map[string]any{
		"phase": "Pending",
		"initContainerStatuses": []any{map[string]any{
			"name":  "migrate",
			"state": map[string]any{"waiting": map[string]any{"reason": "CreateContainerConfigError"}},
		}},
		"containerStatuses": []any{map[string]any{
			"name":  "app",
			"state": map[string]any{"waiting": map[string]any{"reason": "PodInitializing"}},
		}},
	}))

	if status != "CreateContainerConfigError" {
		t.Fatalf("status = %q", status)
	}
}

// A completed job's container is terminated, and that is not a status worth shouting.
func TestPodStatusIgnoresACleanExit(t *testing.T) {
	status := PodStatus(pod(map[string]any{
		"phase": "Succeeded",
		"containerStatuses": []any{map[string]any{
			"name":  "worker",
			"state": map[string]any{"terminated": map[string]any{"reason": "Completed", "exitCode": int64(0)}},
		}},
	}))

	if status != "Succeeded" {
		t.Fatalf("status = %q, want the phase", status)
	}
}
