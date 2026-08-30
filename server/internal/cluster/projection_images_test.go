package cluster_test

import (
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/erolbeyaz/kubby/internal/cluster"
)

func container(name, image string) map[string]any {
	return map[string]any{"name": name, "image": image}
}

func objectWith(kind string, spec map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       kind,
		"metadata":   map[string]any{"name": strings.ToLower(kind) + "-1", "namespace": "default"},
		"spec":       spec,
	}}
}

func hasColumn(kind, key string) bool {
	for _, column := range cluster.ColumnsFor(kind) {
		if column.Key == key {
			return true
		}
	}
	return false
}

// The column names one image; the tooltip behind it names every container, because the
// first container is not necessarily the application (ADR-030).
func TestPodRowCarriesItsImagesAndNamesEveryContainer(t *testing.T) {
	pod := objectWith("Pod", map[string]any{
		"initContainers": []any{container("wait-for-db", "busybox:1.36")},
		"containers": []any{
			container("app", "registry.internal.example/team/api:1.4.0"),
			container("istio-proxy", "docker.io/istio/proxyv2:1.22.0"),
		},
	})

	row := cluster.Project("Pod", pod, time.Now())

	if got, want := row.Fields["image"], "registry.internal.example/team/api:1.4.0 +2"; got != want {
		t.Errorf("image column = %q, want %q", got, want)
	}

	detail := row.Fields["images"]
	for _, want := range []string{
		"app  registry.internal.example/team/api:1.4.0",
		"istio-proxy  docker.io/istio/proxyv2:1.22.0",
		"wait-for-db  busybox:1.36 (init)",
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("images tooltip %q does not mention %q", detail, want)
		}
	}

	if !hasColumn("Pod", "image") {
		t.Error("the Pod list has no image column")
	}
}

func TestSingleContainerPodShowsTheImageAlone(t *testing.T) {
	pod := objectWith("Pod", map[string]any{
		"containers": []any{container("app", "nginx:1.27")},
	})

	if got := cluster.Project("Pod", pod, time.Now()).Fields["image"]; got != "nginx:1.27" {
		t.Errorf("image column = %q, want %q", got, "nginx:1.27")
	}
}

// Every workload reads its images out of the pod template it will create, so a list of
// deployments answers "what is this running" without opening one.
func TestWorkloadRowsReadImagesFromTheirPodTemplate(t *testing.T) {
	template := map[string]any{
		"spec": map[string]any{"containers": []any{container("app", "ghcr.io/acme/web:2.1")}},
	}

	cases := []struct {
		kind   string
		object *unstructured.Unstructured
	}{
		{"Deployment", objectWith("Deployment", map[string]any{"template": template})},
		{"StatefulSet", objectWith("StatefulSet", map[string]any{"template": template})},
		{"DaemonSet", objectWith("DaemonSet", map[string]any{"template": template})},
		{"ReplicaSet", objectWith("ReplicaSet", map[string]any{"template": template})},
		{"Job", objectWith("Job", map[string]any{"template": template})},
		{"CronJob", objectWith("CronJob", map[string]any{
			"jobTemplate": map[string]any{"spec": map[string]any{"template": template}},
		})},
	}

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			row := cluster.Project(tc.kind, tc.object, time.Now())
			if got := row.Fields["image"]; got != "ghcr.io/acme/web:2.1" {
				t.Errorf("image column = %q, want %q", got, "ghcr.io/acme/web:2.1")
			}
			if !hasColumn(tc.kind, "image") {
				t.Errorf("the %s list has no image column", tc.kind)
			}
		})
	}
}

// A kind with no pod spec must not grow an empty column value that reads as a missing
// image rather than as a resource that has none.
func TestObjectsWithoutAPodSpecCarryNoImageField(t *testing.T) {
	service := objectWith("Service", map[string]any{"type": "ClusterIP"})

	row := cluster.Project("Service", service, time.Now())
	if _, ok := row.Fields["image"]; ok {
		t.Errorf("Service row carries an image field: %q", row.Fields["image"])
	}
}

func runningStatus(name string, ready bool) map[string]any {
	return map[string]any{
		"name":         name,
		"ready":        ready,
		"restartCount": int64(2),
		"containerID":  "containerd://d80a39fede42f882c9809cdfae6f2cdb349a4148e14bbf620ed3cd4bf876df57",
		"state":        map[string]any{"running": map[string]any{"startedAt": "2026-08-27T18:20:55Z"}},
	}
}

func terminatedStatus(name string, exitCode int64, reason string) map[string]any {
	return map[string]any{
		"name":  name,
		"ready": true,
		"state": map[string]any{"terminated": map[string]any{
			"exitCode":   exitCode,
			"reason":     reason,
			"startedAt":  "2026-08-27T18:20:34Z",
			"finishedAt": "2026-08-27T18:20:34Z",
		}},
	}
}

func statesOf(row cluster.Row) map[string]cluster.ContainerState {
	byName := map[string]cluster.ContainerState{}
	for _, state := range row.ContainerStates {
		byName[state.Name] = state
	}
	return byName
}

// A count of ready containers says how many are wrong and never which one. The row
// carries each container so the reader can be told without opening the pod.
func TestPodRowCarriesWhatEachContainerIsDoing(t *testing.T) {
	pod := objectWith("Pod", map[string]any{
		"initContainers": []any{container("istio-init", "docker.io/istio/proxyv2:1.22.0")},
		"containers": []any{
			container("fi-accounting-api", "registry.internal/team/api:1.4.0"),
			container("istio-proxy", "docker.io/istio/proxyv2:1.22.0"),
		},
	})
	pod.Object["status"] = map[string]any{
		"containerStatuses": []any{
			runningStatus("fi-accounting-api", true),
			runningStatus("istio-proxy", true),
		},
		"initContainerStatuses": []any{terminatedStatus("istio-init", 0, "Completed")},
	}

	row := cluster.Project("Pod", pod, time.Now())

	// Application containers first: what the pod is running now is what is looked for.
	if got := len(row.ContainerStates); got != 3 {
		t.Fatalf("row carries %d container states, want 3", got)
	}
	if got := row.ContainerStates[2].Name; got != "istio-init" {
		t.Errorf("third state is %q, want the init container last", got)
	}

	states := statesOf(row)

	app := states["fi-accounting-api"]
	if app.State != "running" || !app.Ready {
		t.Errorf("app container = %q ready=%v, want running and ready", app.State, app.Ready)
	}
	if app.StartedAt != "2026-08-27T18:20:55Z" {
		t.Errorf("app container startedAt = %q", app.StartedAt)
	}
	if app.Restarts != 2 {
		t.Errorf("app container restarts = %d, want 2", app.Restarts)
	}
	if app.Init {
		t.Error("the application container is marked as an init container")
	}

	initial := states["istio-init"]
	if initial.State != "terminated" || !initial.Init {
		t.Errorf("init container = %q init=%v, want terminated and marked init", initial.State, initial.Init)
	}
	// Zero is a meaningful exit code; an omitted one and a clean exit must differ.
	if initial.ExitCode == nil || *initial.ExitCode != 0 {
		t.Errorf("init container exit code = %v, want a reported 0", initial.ExitCode)
	}
	if initial.Reason != "Completed" || initial.FinishedAt == "" {
		t.Errorf("init container reason = %q finishedAt = %q", initial.Reason, initial.FinishedAt)
	}
}

func TestWaitingContainerReportsWhyItIsWaiting(t *testing.T) {
	pod := objectWith("Pod", map[string]any{
		"containers": []any{container("app", "nginx:does-not-exist")},
	})
	pod.Object["status"] = map[string]any{
		"containerStatuses": []any{map[string]any{
			"name":  "app",
			"ready": false,
			"state": map[string]any{"waiting": map[string]any{
				"reason":  "ImagePullBackOff",
				"message": "Back-off pulling image",
			}},
		}},
	}

	state := statesOf(cluster.Project("Pod", pod, time.Now()))["app"]
	if state.State != "waiting" || state.Reason != "ImagePullBackOff" {
		t.Errorf("waiting container = %q reason %q", state.State, state.Reason)
	}
	if state.ExitCode != nil {
		t.Errorf("a container that never ran reports an exit code: %d", *state.ExitCode)
	}
}

// Only kinds that run containers carry the states; a service row must not grow an
// empty array that the client has to tell apart from a pod with no containers.
func TestOnlyPodsCarryContainerStates(t *testing.T) {
	row := cluster.Project("Deployment", objectWith("Deployment", map[string]any{}), time.Now())
	if row.ContainerStates != nil {
		t.Errorf("a Deployment row carries container states: %+v", row.ContainerStates)
	}
}

// An ingress host is an address, and the reason anyone opens that list. Turning it into
// a link saves retyping what is already on the screen — but only when the scheme and the
// path are the ones that actually serve.
func TestIngressHostsCarryTheAddressTheyResolveTo(t *testing.T) {
	ingress := objectWith("Ingress", map[string]any{
		"ingressClassName": "nginx",
		"tls":              []any{map[string]any{"hosts": []any{"secure.example.com"}}},
		"rules": []any{
			map[string]any{
				"host": "secure.example.com",
				"http": map[string]any{"paths": []any{map[string]any{"path": "/app"}}},
			},
			map[string]any{
				"host": "plain.example.com",
				"http": map[string]any{"paths": []any{map[string]any{"path": "/"}}},
			},
		},
	})

	fields := cluster.Project("Ingress", ingress, time.Now()).Fields

	if got, want := fields["hosts"], "secure.example.com,plain.example.com"; got != want {
		t.Errorf("hosts = %q, want %q", got, want)
	}
	// The host in spec.tls is https; the one that is not is not.
	want := "https://secure.example.com/app,http://plain.example.com/"
	if got := fields["hostUrls"]; got != want {
		t.Errorf("hostUrls = %q, want %q", got, want)
	}
}

// A regex path is a pattern rather than somewhere to go, so the link falls back to the
// root instead of sending the reader to a literal `/api/(.*)`.
func TestARegexPathIsNotUsedAsALink(t *testing.T) {
	ingress := objectWith("Ingress", map[string]any{
		"rules": []any{map[string]any{
			"host": "example.com",
			"http": map[string]any{"paths": []any{map[string]any{"path": "/api/(.*)"}}},
		}},
	})

	if got := cluster.Project("Ingress", ingress, time.Now()).Fields["hostUrls"]; got != "http://example.com/" {
		t.Errorf("hostUrls = %q", got)
	}
}

// An HTTPRoute had no projection at all and showed only its age, which says nothing
// about where it sends traffic or what accepted it.
func TestHTTPRouteReportsItsHostsAndGateways(t *testing.T) {
	route := objectWith("HTTPRoute", map[string]any{
		"hostnames":  []any{"api.example.com", "www.example.com"},
		"parentRefs": []any{map[string]any{"name": "public-gateway"}},
		"rules":      []any{map[string]any{}, map[string]any{}},
	})

	fields := cluster.Project("HTTPRoute", route, time.Now()).Fields

	if got, want := fields["hosts"], "api.example.com,www.example.com"; got != want {
		t.Errorf("hosts = %q, want %q", got, want)
	}
	if got, want := fields["hostUrls"], "https://api.example.com/,https://www.example.com/"; got != want {
		t.Errorf("hostUrls = %q, want %q", got, want)
	}
	if got := fields["gateways"]; got != "public-gateway" {
		t.Errorf("gateways = %q", got)
	}
	if got := fields["rules"]; got != "2" {
		t.Errorf("rules = %q", got)
	}

	if !hasColumn("HTTPRoute", "hosts") {
		t.Error("the HTTPRoute list has no hosts column")
	}
}

// The column has to say that its value goes somewhere, or the client renders text.
func TestHostColumnsAreMarkedAsLinks(t *testing.T) {
	for _, kind := range []string{"Ingress", "HTTPRoute"} {
		var linked bool
		for _, column := range cluster.ColumnsFor(kind) {
			if column.Key == "hosts" && column.Link == cluster.LinkExternal {
				linked = true
			}
		}
		if !linked {
			t.Errorf("%s hosts is not an external link", kind)
		}
	}
}
