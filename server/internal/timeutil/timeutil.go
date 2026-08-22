// Package timeutil centralises Kubby's time handling.
//
// Per ADR-026 the server works exclusively in UTC and emits RFC 3339 with an explicit
// offset. Conversion to a display timezone (Europe/Istanbul by default) happens in the
// browser, never here — a 3 hour skew on a restart timestamp causes misdiagnosis.
package timeutil

import (
	"fmt"
	"time"
)

// TimestampLayout is the single serialisation format for every timestamp Kubby emits.
const TimestampLayout = time.RFC3339Nano

// DisplayTimezone is the default timezone the frontend renders in. The server never
// applies it; it is published so the frontend and tests agree on one default.
const DisplayTimezone = "Europe/Istanbul"

// Now returns the current instant in UTC.
func Now() time.Time { return time.Now().UTC() }

// Format renders t as RFC 3339 in UTC.
func Format(t time.Time) string { return t.UTC().Format(TimestampLayout) }

// Parse reads an RFC 3339 timestamp and normalises it to UTC.
func Parse(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", s, err)
	}
	return t.UTC(), nil
}

// Age renders a duration the way kubectl does in its AGE column: the two most
// significant units, or a single unit once the value is large.
func Age(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		if s := int(d.Seconds()) % 60; s > 0 {
			return fmt.Sprintf("%dm%ds", int(d.Minutes()), s)
		}
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		if m := int(d.Minutes()) % 60; m > 0 {
			return fmt.Sprintf("%dh%dm", int(d.Hours()), m)
		}
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 365*24*time.Hour:
		days := int(d.Hours()) / 24
		if h := int(d.Hours()) % 24; h > 0 && days < 10 {
			return fmt.Sprintf("%dd%dh", days, h)
		}
		return fmt.Sprintf("%dd", days)
	default:
		years := int(d.Hours()) / (24 * 365)
		days := (int(d.Hours()) % (24 * 365)) / 24
		if days > 0 && years < 10 {
			return fmt.Sprintf("%dy%dd", years, days)
		}
		return fmt.Sprintf("%dy", years)
	}
}
