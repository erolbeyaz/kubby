package k8s

import (
	"reflect"
	"testing"
)

// Opening istio-proxy by default makes the tool lie: the user reads proxy access records
// believing they are application output.
func TestDefaultContainerSkipsInjectedSidecars(t *testing.T) {
	c := NewClassifier(nil)

	if got := c.DefaultContainer([]string{"istio-proxy", "payments-api"}); got != "payments-api" {
		t.Fatalf("default container = %q, want payments-api", got)
	}
}

func TestDefaultContainerFallsBackWhenEverythingIsASidecar(t *testing.T) {
	c := NewClassifier(nil)

	if got := c.DefaultContainer([]string{"istio-proxy", "vector"}); got != "istio-proxy" {
		t.Fatalf("default container = %q, want the first container", got)
	}
	if got := c.DefaultContainer(nil); got != "" {
		t.Fatalf("default container = %q, want empty", got)
	}
}

func TestClassifierHonoursConfiguredNames(t *testing.T) {
	c := NewClassifier([]string{"company-mesh"})

	if !c.IsSidecar("company-mesh") {
		t.Fatal("configured sidecar was not recognised")
	}
	// Replacing the list means replacing it: a cluster that injects something else does
	// not necessarily inject istio too.
	if c.IsSidecar("istio-proxy") {
		t.Fatal("configured list should replace the defaults, not extend them")
	}
}

func TestOrderedGroupsSidecarsAfterTheWorkload(t *testing.T) {
	c := NewClassifier(nil)

	got := c.Ordered([]string{"istio-proxy", "web", "api"}, []string{"migrate", "wait-for-db"})

	want := []Container{
		{Name: "api", Role: RoleApp},
		{Name: "web", Role: RoleApp},
		{Name: "istio-proxy", Role: RoleSidecar},
		{Name: "migrate", Role: RoleInit},
		{Name: "wait-for-db", Role: RoleInit},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered = %+v, want %+v", got, want)
	}
}

// Init containers run in the order they are declared; sorting them would describe a
// sequence that does not happen.
func TestOrderedKeepsInitContainersInDeclaredOrder(t *testing.T) {
	c := NewClassifier(nil)

	got := c.Ordered([]string{"web"}, []string{"z-last", "a-first"})

	if got[1].Name != "z-last" || got[2].Name != "a-first" {
		t.Fatalf("init containers were reordered: %+v", got)
	}
}
