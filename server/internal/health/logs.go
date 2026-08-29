package health

import (
	"context"
	"fmt"
	"time"

	"github.com/erolbeyaz/kubby/internal/logsearch"
)

// LogDetector reports what the cluster's own applications keep saying about themselves.
//
// The only detector that does not read the cluster. Everything else here asks the
// Kubernetes API what is wrong; this carries an answer from a system that already read
// every log line on every node, because a pod can be Running and Ready while its log
// says it cannot reach its database and the API has no idea.
type LogDetector struct {
	Findings   []logsearch.Finding
	Namespaces []string
}

func (d *LogDetector) Name() string { return "logs" }

func (d *LogDetector) Detect(context.Context, Reader) ([]Finding, error) {
	if len(d.Findings) == 0 {
		return nil, nil
	}

	wanted := map[string]bool{}
	for _, namespace := range d.Namespaces {
		wanted[namespace] = true
	}

	findings := make([]Finding, 0, len(d.Findings))
	for _, found := range d.Findings {
		if len(wanted) > 0 && !wanted[found.Namespace] {
			continue
		}

		findings = append(findings, Finding{
			Category:  "logs",
			Severity:  severityFor(found.Severity),
			Kind:      "Pod",
			Namespace: found.Namespace,
			Name:      found.Pod,
			Reason:    found.Rule,
			Detail:    logDetail(found),
			Container: found.Container,
			Count:     int(found.Count),
			LastSeen:  rfc3339(found.LastSeen),
			TypeKey:   "pods",
		})
	}
	return findings, nil
}

// logDetail is the sentence a reader gets instead of opening the pod: how long it has
// been going on, what failed, and the line itself.
//
// Duration first. The number that decides whether to stop and look now is not how many
// lines there are but how long it has been true — the failure this was built for had
// been repeating for twenty-two hours behind a green row.
func logDetail(found logsearch.Finding) string {
	detail := ""
	if !found.FirstSeen.IsZero() {
		detail = "failing for " + humanDuration(found.LastSeen.Sub(found.FirstSeen)) + " · "
	}
	if found.Pods > 1 {
		detail += fmt.Sprintf("%d pods · ", found.Pods)
	}
	if found.Summary != "" {
		detail += found.Summary + " · "
	}
	return detail + found.Sample
}

// severityFor maps a sweep's two levels onto the panel's three. A pod failing through
// every slice of the window is not a warning about something that might happen.
func severityFor(severity string) string {
	if severity == logsearch.SeverityError {
		return SeverityCritical
	}
	return SeverityWarning
}

func humanDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return "under a minute"
	}
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
