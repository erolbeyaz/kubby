package health

import (
	"context"
	"sync"
	"time"
)

// DefaultDetectorTimeout bounds a single detector. One slow list must not hold the panel.
const DefaultDetectorTimeout = 15 * time.Second

// Collector runs detectors together and returns what it got.
type Collector struct {
	Reader    Reader
	Detectors []Detector
	Timeout   time.Duration
}

// Collect runs every detector concurrently.
//
// A detector that fails is recorded and skipped, never fatal: a user denied access to
// nodes should still see their crash-looping pods. Returning nothing because one list was
// forbidden is the difference between a useful panel and a useless one.
func (c *Collector) Collect(ctx context.Context) *Report {
	report := &Report{Counts: map[string]int{}}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		findings []Finding
	)

	for _, detector := range c.Detectors {
		wg.Add(1)

		go func() {
			defer wg.Done()

			scoped, cancel := context.WithTimeout(ctx, c.timeout())
			defer cancel()

			found, err := detector.Detect(scoped, c.Reader)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if report.Failed == nil {
					report.Failed = map[string]string{}
				}
				report.Failed[detector.Name()] = err.Error()
				return
			}
			findings = append(findings, found...)
		}()
	}
	wg.Wait()

	Sort(findings)
	report.Findings = findings
	for _, finding := range findings {
		report.Counts[finding.Severity]++
	}
	return report
}

func (c *Collector) timeout() time.Duration {
	if c.Timeout <= 0 {
		return DefaultDetectorTimeout
	}
	return c.Timeout
}
