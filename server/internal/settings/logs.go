package settings

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/erolbeyaz/kubby/internal/logsearch"
)

// KeyLogAnalysis is where the rules and field names live.
const KeyLogAnalysis = "log_analysis"

// LogFields names the parts of a log document Kubby reads.
//
// Deployment-wide rather than per cluster: a fleet is normally shipped by one pipeline,
// so the spelling is the same everywhere. A cluster whose logs are shaped differently is
// a case to handle when it exists rather than a column to add now.
type LogFields struct {
	Timestamp string `json:"timestamp,omitempty"`
	Message   string `json:"message,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Container string `json:"container,omitempty"`
}

// LogRule is one thing worth noticing in a log line, as an admin edits it.
type LogRule struct {
	Name  string `json:"name"`
	Class string `json:"class"`
	// Match is the phrases; any one of them is a hit.
	Match []string `json:"match"`
	// Capture pulls the identity of what failed out of the matched line, as regular
	// expressions with named groups.
	Capture []string `json:"capture,omitempty"`
	// Disabled keeps a rule in the list without running it, which is what someone wants
	// when a rule is too noisy in their environment — deleting it loses the wording.
	Disabled bool `json:"disabled,omitempty"`
}

// LogAnalysis is the whole configuration of what counts as a problem.
type LogAnalysis struct {
	Fields LogFields `json:"fields"`
	// Rules replaces the built-in set when it is non-empty.
	Rules []LogRule `json:"rules"`
	// WindowMinutes is how far back each sweep looks.
	WindowMinutes int `json:"windowMinutes,omitempty"`
	// MinCount is how many matching lines a pod must have written to be worth saying
	// anything about. A retry that succeeded on the second attempt logged one failure.
	MinCount int `json:"minCount,omitempty"`
}

const (
	defaultWindowMinutes = 15
	defaultMinCount      = 3
	maxWindowMinutes     = 24 * 60
	maxRules             = 60
	maxPhrasesPerRule    = 40
)

var logClasses = map[string]bool{
	logsearch.ClassAuth:        true,
	logsearch.ClassUnreachable: true,
	logsearch.ClassTimeout:     true,
	logsearch.ClassGeneric:     true,
}

// DefaultLogAnalysis is what Kubby ships with: the built-in rules, Fluent Bit's field
// names, and thresholds chosen to keep a single stumble off the screen.
func DefaultLogAnalysis() LogAnalysis {
	fields := logsearch.DefaultFields()

	rules := make([]LogRule, 0, len(logsearch.DefaultRules()))
	for _, rule := range logsearch.DefaultRules() {
		captures := make([]string, 0, len(rule.Capture))
		for _, pattern := range rule.Capture {
			captures = append(captures, pattern.String())
		}
		rules = append(rules, LogRule{
			Name: rule.Name, Class: rule.Class, Match: rule.Match, Capture: captures,
		})
	}

	return LogAnalysis{
		Fields: LogFields{
			Timestamp: fields.Timestamp, Message: fields.Message, Pod: fields.Pod,
			Namespace: fields.Namespace, Container: fields.Container,
		},
		Rules:         rules,
		WindowMinutes: defaultWindowMinutes,
		MinCount:      defaultMinCount,
	}
}

// LogAnalysisConfig reports what a sweep should run, ready to use.
//
// Anything unset falls back to the built-in value rather than to zero: a window of no
// minutes or a rule list of none would turn the feature off silently, and an admin who
// saved a partial form did not ask for that.
func (s *Service) LogAnalysisConfig(ctx context.Context) (logsearch.Fields, []logsearch.Rule, logsearch.SweepOptions, error) {
	stored := LogAnalysis{}
	if _, err := s.repo.Get(ctx, KeyLogAnalysis, &stored); err != nil {
		return logsearch.Fields{}, nil, logsearch.SweepOptions{}, err
	}

	fields := logsearch.Fields{
		Timestamp: stored.Fields.Timestamp, Message: stored.Fields.Message,
		Pod: stored.Fields.Pod, Namespace: stored.Fields.Namespace,
		Container: stored.Fields.Container,
	}

	rules := logsearch.DefaultRules()
	if len(stored.Rules) > 0 {
		rules = compileRules(stored.Rules)
	}

	opts := logsearch.SweepOptions{
		Window:   minutes(stored.WindowMinutes, defaultWindowMinutes),
		MinCount: positive(stored.MinCount, defaultMinCount),
	}
	return fields, rules, opts, nil
}

// SaveLogAnalysis validates and stores it.
func (s *Service) SaveLogAnalysis(ctx context.Context, value LogAnalysis, by uuid.UUID) error {
	if err := validateLogAnalysis(value); err != nil {
		return err
	}
	return s.repo.Put(ctx, KeyLogAnalysis, value, by)
}

func validateLogAnalysis(value LogAnalysis) error {
	if value.WindowMinutes < 0 || value.WindowMinutes > maxWindowMinutes {
		return fmt.Errorf("the window must be between 1 and %d minutes", maxWindowMinutes)
	}
	if value.MinCount < 0 {
		return fmt.Errorf("the minimum count cannot be negative")
	}
	if len(value.Rules) > maxRules {
		return fmt.Errorf("at most %d rules", maxRules)
	}

	seen := map[string]bool{}
	for _, rule := range value.Rules {
		name := strings.TrimSpace(rule.Name)
		if name == "" {
			return fmt.Errorf("every rule needs a name")
		}
		if seen[strings.ToLower(name)] {
			return fmt.Errorf("two rules are called %q; a finding names one rule", name)
		}
		seen[strings.ToLower(name)] = true

		if rule.Class != "" && !logClasses[rule.Class] {
			return fmt.Errorf("%q: class must be auth, unreachable, timeout or generic", name)
		}
		if len(nonEmpty(rule.Match)) == 0 {
			return fmt.Errorf("%q: a rule with no phrases matches nothing", name)
		}
		if len(rule.Match) > maxPhrasesPerRule {
			return fmt.Errorf("%q: at most %d phrases", name, maxPhrasesPerRule)
		}

		// A capture that does not compile would disable itself quietly and the finding
		// would simply lose its summary, which is a hard thing to notice.
		for _, pattern := range rule.Capture {
			if strings.TrimSpace(pattern) == "" {
				continue
			}
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("%q: %s is not a valid pattern: %w", name, pattern, err)
			}
		}
	}
	return nil
}

// compileRules turns stored rules into runnable ones, dropping what cannot run.
//
// A pattern that does not compile is skipped rather than fatal: it was valid when it was
// saved, and a rule that lost its summary still finds the problem.
func compileRules(stored []LogRule) []logsearch.Rule {
	rules := make([]logsearch.Rule, 0, len(stored))
	for _, rule := range stored {
		if rule.Disabled {
			continue
		}
		phrases := nonEmpty(rule.Match)
		if len(phrases) == 0 {
			continue
		}

		captures := make([]*regexp.Regexp, 0, len(rule.Capture))
		for _, pattern := range rule.Capture {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				continue
			}
			captures = append(captures, compiled)
		}

		class := rule.Class
		if class == "" {
			class = logsearch.ClassGeneric
		}
		rules = append(rules, logsearch.Rule{
			Name: strings.TrimSpace(rule.Name), Class: class,
			Match: phrases, Capture: captures,
		})
	}
	return rules
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func minutes(value, fallback int) time.Duration {
	return time.Duration(positive(value, fallback)) * time.Minute
}

func positive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
