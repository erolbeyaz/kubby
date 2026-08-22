package timeutil

import (
	"strings"
	"testing"
	"time"
)

// Kubernetes reports UTC; the team reads Europe/Istanbul (UTC+3). A silent 3 hour skew
// on a restart timestamp causes misdiagnosis, so the boundary is pinned by tests.
func TestFormatAlwaysEmitsUTC(t *testing.T) {
	istanbul, err := time.LoadLocation(DisplayTimezone)
	if err != nil {
		t.Fatalf("load %s: %v", DisplayTimezone, err)
	}

	local := time.Date(2026, 8, 22, 13, 15, 30, 0, istanbul)
	got := Format(local)

	if !strings.HasSuffix(got, "Z") {
		t.Fatalf("Format(%v) = %q, want a UTC timestamp ending in Z", local, got)
	}
	if !strings.HasPrefix(got, "2026-08-22T10:15:30") {
		t.Fatalf("Format(%v) = %q, want 10:15:30 UTC (13:15:30 +03)", local, got)
	}
}

func TestParseNormalisesToUTC(t *testing.T) {
	parsed, err := Parse("2026-08-22T13:15:30+03:00")
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if parsed.Location() != time.UTC {
		t.Errorf("Parse location = %v, want UTC", parsed.Location())
	}
	if parsed.Hour() != 10 {
		t.Errorf("Parse hour = %d, want 10", parsed.Hour())
	}
}

func TestParseRejectsMalformedInput(t *testing.T) {
	for _, in := range []string{"", "22.08.2026 13:15", "2026-08-22 13:15:30"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", in)
		}
	}
}

func TestRoundTripPreservesInstant(t *testing.T) {
	want := time.Date(2026, 8, 22, 10, 15, 30, 0, time.UTC)

	got, err := Parse(Format(want))
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("round trip = %v, want %v", got, want)
	}
}

func TestAgeMatchesKubectlStyle(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m30s"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h30m"},
		{5 * time.Hour, "5h"},
		{50 * time.Hour, "2d2h"},
		{30 * 24 * time.Hour, "30d"},
		{400 * 24 * time.Hour, "1y35d"},
	}

	for _, c := range cases {
		if got := Age(c.in); got != c.want {
			t.Errorf("Age(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A clock skew must not render as a huge negative age.
func TestAgeHandlesNegativeDurations(t *testing.T) {
	if got := Age(-30 * time.Second); got != "30s" {
		t.Errorf("Age(-30s) = %q, want 30s", got)
	}
}
