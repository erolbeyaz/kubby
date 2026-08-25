package cluster

import (
	"context"
	"strings"
	"testing"

	"github.com/erolbeyaz/kubby/internal/store"
)

// The deployment-wide lock, at the gate every write passes through.
//
// Tested here rather than only end to end because the switch has to hold for every verb
// and every kind, and enumerating those through HTTP would test the routing rather than
// the rule.
func TestTheKillSwitchRefusesEveryWriteWhateverElseIsTrue(t *testing.T) {
	svc := &Service{}
	podType, err := LookupType("pods")
	if err != nil {
		t.Fatalf("pods are not registered: %v", err)
	}

	for _, verb := range []Verb{VerbCreate, VerbUpdate, VerbPatch, VerbDelete} {
		verdict, err := svc.CheckWrite(context.Background(), &store.Cluster{Name: "any"},
			WriteRequest{Type: podType, Namespace: "default", Name: "x", Verb: verb},
			// Everything else says yes. The switch still has to say no.
			Permission{GlobalReadOnly: true, MayWrite: true},
			nil,
		)
		if err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
		if verdict.Allowed {
			t.Errorf("%s was allowed with the kill switch on", verb)
		}
		// The reason has to name which of four gates stopped it, or the reader is left
		// guessing why a permitted action was refused.
		if !strings.Contains(strings.ToLower(verdict.Reason), "read-only") {
			t.Errorf("%s was refused without naming the kill switch: %q", verb, verdict.Reason)
		}
	}
}

// And it is checked before anything else: a locked deployment must not reach the cluster
// to ask it questions, because the answer cannot change the outcome.
func TestTheKillSwitchIsCheckedBeforeTheCluster(t *testing.T) {
	svc := &Service{}
	podType, _ := LookupType("pods")

	// No credential, no informers, nothing that could answer. If the switch were checked
	// after the cluster, this would fail trying to reach one.
	verdict, err := svc.CheckWrite(context.Background(), &store.Cluster{Name: "unreachable"},
		WriteRequest{Type: podType, Namespace: "default", Name: "x", Verb: VerbDelete},
		Permission{GlobalReadOnly: true, MayWrite: true},
		nil,
	)
	if err != nil {
		t.Fatalf("the kill switch consulted the cluster: %v", err)
	}
	if verdict.Allowed {
		t.Fatal("allowed with the kill switch on")
	}
}
