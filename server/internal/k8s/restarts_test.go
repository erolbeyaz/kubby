package k8s

import "testing"

// ADR-030 §3: a single total that folds sidecar restarts into the workload's sends the
// reader looking in the wrong place — a correct number producing a wrong conclusion.
func TestBadgeCountsTheWorkloadNotTheMesh(t *testing.T) {
	c := NewClassifier(nil)

	summary := c.Summarise([]ContainerRestarts{
		{Name: "payments-api", Count: 1},
		{Name: "istio-proxy", Count: 4},
	}, nil)

	count, note := summary.Badge()
	if count != 1 {
		t.Fatalf("badge count = %d, want the application's 1", count)
	}
	if note != "+4 sidecar" {
		t.Fatalf("note = %q, want the sidecar restarts stated apart", note)
	}
	if summary.App != 1 || summary.Sidecar != 4 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestInitContainerRestartsAreTheirOwnTally(t *testing.T) {
	c := NewClassifier(nil)

	summary := c.Summarise(
		[]ContainerRestarts{{Name: "web", Count: 0}},
		[]ContainerRestarts{{Name: "migrate", Count: 3}},
	)

	if summary.Init != 3 || summary.App != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.Details[1].Role != RoleInit {
		t.Fatalf("init container role = %q", summary.Details[1].Role)
	}
}

func TestNoSidecarMeansNoNote(t *testing.T) {
	c := NewClassifier(nil)

	_, note := c.Summarise([]ContainerRestarts{{Name: "web", Count: 2}}, nil).Badge()

	if note != "" {
		t.Fatalf("note = %q, want none when nothing was injected", note)
	}
}

// Reading a restart badge should not require knowing that 137 means SIGKILL.
func TestExplainTurnsExitCodesIntoSentences(t *testing.T) {
	for _, tc := range []struct {
		name string
		term Termination
		want string
	}{
		{"oom", Termination{Reason: "OOMKilled", ExitCode: 137}, "Killed for exceeding its memory limit."},
		{"sigkill code", Termination{ExitCode: 137}, "Exit code 137: killed by SIGKILL, most often the memory limit."},
		{"sigterm code", Termination{ExitCode: 143}, "Exit code 143: terminated by SIGTERM during shutdown."},
		{"clean", Termination{Reason: "Completed"}, "Exited normally."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.term.Explain(); got != tc.want {
				t.Fatalf("explain = %q, want %q", got, tc.want)
			}
		})
	}

	var none *Termination
	if none.Explain() != "" {
		t.Fatal("a nil termination must explain nothing rather than panic")
	}
}
