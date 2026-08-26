package promql

import "testing"

// One broken pod is one problem.
//
// A pod whose image will not pull is also in phase Pending and also NotReady, and all
// three are true. Reporting all three counted three broken pods as ten problems, put a
// verdict above the table that nothing in the table explained, and made the pending
// count on the overview disagree with every row under it.
func TestOneBrokenPodIsOneFinding(t *testing.T) {
	findings := collapsePodFindings([]Finding{
		{Kind: "Pod", Namespace: "shop", Name: "web", Container: "nginx",
			Reason: "ImagePullBackOff", Severity: "error", rank: rankWaiting},
		{Kind: "Pod", Namespace: "shop", Name: "web", Reason: "Pending", rank: rankPending},
		{Kind: "Pod", Namespace: "shop", Name: "web", Reason: "NotReady", rank: rankNotReady},
	})

	if len(findings) != 1 {
		t.Fatalf("one broken pod produced %d findings: %v", len(findings), findings)
	}
	if findings[0].Reason != "ImagePullBackOff" {
		t.Errorf("kept %q rather than the reason that says what to do", findings[0].Reason)
	}
}

// The exit reason explains the backoff rather than competing with it — and it is the
// line that saves opening the pod to find out why it keeps dying.
func TestTheExitReasonExplainsTheBackoff(t *testing.T) {
	findings := collapsePodFindings([]Finding{
		{Kind: "Pod", Namespace: "shop", Name: "api", Container: "app",
			Reason: "CrashLoopBackOff", Detail: "container will not start", rank: rankWaiting},
		{Kind: "Pod", Namespace: "shop", Name: "api", Container: "app",
			Reason: "OOMKilled", Detail: "last exit reason", rank: rankTerminated},
	})

	if len(findings) != 1 {
		t.Fatalf("one container produced %d findings: %v", len(findings), findings)
	}
	if findings[0].Reason != "CrashLoopBackOff" {
		t.Errorf("the state lost to the exit reason: %q", findings[0].Reason)
	}
	if findings[0].Detail != "last exit: OOMKilled" {
		t.Errorf("the exit reason was dropped: %q", findings[0].Detail)
	}
}

// Two containers in one pod failing for two reasons are two things to fix.
func TestEachFailingContainerIsKept(t *testing.T) {
	findings := collapsePodFindings([]Finding{
		{Kind: "Pod", Namespace: "shop", Name: "api", Container: "app",
			Reason: "CrashLoopBackOff", rank: rankWaiting},
		{Kind: "Pod", Namespace: "shop", Name: "api", Container: "sidecar",
			Reason: "ImagePullBackOff", rank: rankWaiting},
	})

	if len(findings) != 2 {
		t.Fatalf("two failing containers produced %d findings: %v", len(findings), findings)
	}
}

// A pod nobody can place has no container statuses at all, and it is exactly the case
// the pending count exists for. Narrowing pending must not lose it.
func TestAnUnschedulablePodKeepsItsPendingFinding(t *testing.T) {
	findings := collapsePodFindings([]Finding{
		{Kind: "Pod", Namespace: "data", Name: "reporting-0", Reason: "Pending", rank: rankPending},
		{Kind: "Pod", Namespace: "data", Name: "reporting-0", Reason: "NotReady", rank: rankNotReady},
	})

	if len(findings) != 1 || findings[0].Reason != "Pending" {
		t.Fatalf("expected one Pending finding, got %v", findings)
	}
}

// A Deployment and the pods under it are separate objects, and a reader wants both: the
// pod says what broke, the Deployment says what it is costing.
func TestNonPodFindingsPassThrough(t *testing.T) {
	findings := collapsePodFindings([]Finding{
		{Kind: "Pod", Namespace: "shop", Name: "web-1", Container: "nginx",
			Reason: "ImagePullBackOff", rank: rankWaiting},
		{Kind: "Deployment", Namespace: "shop", Name: "web", Reason: "Unavailable"},
		{Kind: "Node", Name: "worker-3", Reason: "DiskPressure"},
	})

	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d: %v", len(findings), findings)
	}
}
