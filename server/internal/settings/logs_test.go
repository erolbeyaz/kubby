package settings

import (
	"strings"
	"testing"
	"time"

	"github.com/erolbeyaz/kubby/internal/logsearch"
)

func TestDefaultLogAnalysisCarriesTheBuiltInRules(t *testing.T) {
	value := DefaultLogAnalysis()

	if len(value.Rules) != len(logsearch.DefaultRules()) {
		t.Errorf("%d rules, want %d", len(value.Rules), len(logsearch.DefaultRules()))
	}
	if value.Fields.Message != "log" || value.Fields.Pod != "kubernetes.pod_name" {
		t.Errorf("fields do not match Fluent Bit's spelling: %+v", value.Fields)
	}
	if value.WindowMinutes != defaultWindowMinutes || value.MinCount != defaultMinCount {
		t.Errorf("thresholds = %d minutes / %d lines", value.WindowMinutes, value.MinCount)
	}

	// The generic net has to survive being shown to an admin, or the feature only ever
	// finds problems somebody already anticipated.
	var generic bool
	for _, rule := range value.Rules {
		if rule.Class == logsearch.ClassGeneric && len(rule.Match) > 0 {
			generic = true
		}
	}
	if !generic {
		t.Error("no generic rule survived the conversion")
	}
}

// Every one of these saves would leave the feature running but finding less than the
// admin thinks, which is worse than refusing the save.
func TestSaveRefusesConfigurationThatWouldQuietlyFindNothing(t *testing.T) {
	cases := []struct {
		name  string
		value LogAnalysis
		want  string
	}{
		{
			"a rule with no phrases",
			LogAnalysis{Rules: []LogRule{{Name: "Empty"}}},
			"matches nothing",
		},
		{
			"a rule with no name",
			LogAnalysis{Rules: []LogRule{{Match: []string{"boom"}}}},
			"needs a name",
		},
		{
			"two rules with one name",
			LogAnalysis{Rules: []LogRule{
				{Name: "Redis", Match: []string{"a"}},
				{Name: "redis", Match: []string{"b"}},
			}},
			"two rules are called",
		},
		{
			"a class nothing renders",
			LogAnalysis{Rules: []LogRule{{Name: "R", Class: "urgent", Match: []string{"a"}}}},
			"class must be",
		},
		{
			// A pattern that does not compile loses the finding's summary silently.
			"a capture that does not compile",
			LogAnalysis{Rules: []LogRule{{Name: "R", Match: []string{"a"}, Capture: []string{"(unclosed"}}}},
			"not a valid pattern",
		},
		{
			"a window longer than a day",
			LogAnalysis{WindowMinutes: 2000},
			"between 1 and",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLogAnalysis(tc.value)
			if err == nil {
				t.Fatalf("%+v was accepted", tc.value)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("said %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestSaveAcceptsTheDefaults(t *testing.T) {
	if err := validateLogAnalysis(DefaultLogAnalysis()); err != nil {
		t.Errorf("the shipped configuration is invalid: %v", err)
	}
}

func TestDisabledRulesAreKeptButNotRun(t *testing.T) {
	rules := compileRules([]LogRule{
		{Name: "Kept", Match: []string{"boom"}},
		{Name: "Muted", Match: []string{"noisy"}, Disabled: true},
	})

	if len(rules) != 1 || rules[0].Name != "Kept" {
		t.Errorf("compiled %+v", rules)
	}
}

// A pattern that no longer compiles was valid when it was saved. Dropping the rule would
// lose the finding; dropping the pattern only loses its summary.
func TestARuleSurvivesACaptureThatNoLongerCompiles(t *testing.T) {
	rules := compileRules([]LogRule{
		{Name: "R", Match: []string{"boom"}, Capture: []string{"(broken", `(?P<db>\w+)`}},
	})

	if len(rules) != 1 {
		t.Fatalf("the rule was dropped: %+v", rules)
	}
	if len(rules[0].Capture) != 1 {
		t.Errorf("%d captures survived, want the one that compiles", len(rules[0].Capture))
	}
}

func TestAnEmptyClassBecomesGeneric(t *testing.T) {
	rules := compileRules([]LogRule{{Name: "R", Match: []string{"boom"}}})
	if rules[0].Class != logsearch.ClassGeneric {
		t.Errorf("class = %q", rules[0].Class)
	}
}

// A window of no minutes or a threshold of no lines would turn the feature off without
// saying so; an admin who saved a partial form did not ask for that.
func TestUnsetThresholdsFallBackRatherThanToZero(t *testing.T) {
	if got := minutes(0, defaultWindowMinutes); got != 15*time.Minute {
		t.Errorf("window = %s", got)
	}
	if got := positive(0, defaultMinCount); got != defaultMinCount {
		t.Errorf("min count = %d", got)
	}
}
