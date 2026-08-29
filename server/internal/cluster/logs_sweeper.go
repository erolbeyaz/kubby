package cluster

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/erolbeyaz/kubby/internal/logsearch"
	"github.com/erolbeyaz/kubby/internal/store"
)

// State says whether the answer can be trusted.
//
// The third value is the point of having three. An unreachable log store must not read
// as a cluster with nothing wrong in it: that mistake was made once already, when a
// Prometheus nobody could reach rendered as a healthy empty cluster (ADR-111), and the
// shape of it here is identical.
const (
	LogsStateOff     = "off"     // no source configured, which is an ordinary state
	LogsStateOK      = "ok"      // the store answered
	LogsStateUnknown = "unknown" // it did not, and nothing may be concluded
)

// LogFindings is one cluster's answer, as of one moment.
type LogFindings struct {
	State    string              `json:"state"`
	Detail   string              `json:"detail,omitempty"`
	Findings []logsearch.Finding `json:"findings"`
	SweptAt  time.Time           `json:"sweptAt"`
	Window   time.Duration       `json:"-"`

	byPod      map[string]int
	byWorkload map[string]*logsearch.Finding
}

// For reports the finding for one pod, if it has one.
func (f *LogFindings) For(namespace, pod string) *logsearch.Finding {
	if f == nil || f.byPod == nil {
		return nil
	}
	if at, ok := f.byPod[namespace+"/"+pod]; ok {
		return &f.Findings[at]
	}
	return nil
}

func (f *LogFindings) index() {
	f.byPod = make(map[string]int, len(f.Findings))
	for i, finding := range f.Findings {
		f.byPod[finding.Namespace+"/"+finding.Pod] = i
	}
}

// LogSweeper asks each cluster's log store what its pods are complaining about.
//
// On a schedule rather than on demand: the answer has to already be on the row when the
// list is drawn, and a query per page load would put the store's latency in front of
// every reader.
type LogSweeper struct {
	svc      *Service
	clusters *store.ClusterRepo
	logger   *slog.Logger
	interval time.Duration
	// analysis supplies the rules, the field names and the thresholds at the moment a
	// sweep runs, so an edited rule takes effect on the next tick rather than on the
	// next restart (ADR-102).
	analysis LogAnalysisFunc

	mu      sync.RWMutex
	results map[string]*LogFindings
}

// LogAnalysisFunc reports what a sweep should run. It is a function rather than a
// settings service so this package does not depend on the one that stores them.
type LogAnalysisFunc func(ctx context.Context) (logsearch.Fields, []logsearch.Rule, logsearch.SweepOptions, error)

func NewLogSweeper(svc *Service, db *store.DB, logger *slog.Logger, interval time.Duration, analysis LogAnalysisFunc) *LogSweeper {
	if interval <= 0 {
		interval = time.Minute
	}
	return &LogSweeper{
		svc:      svc,
		clusters: db.Clusters(),
		logger:   logger.With(slog.String("component", "log-sweeper")),
		interval: interval,
		analysis: analysis,
		results:  map[string]*LogFindings{},
	}
}

// configure reads the analysis settings, falling back to what Kubby ships with.
//
// A settings read that fails must not stop the sweep: the built-in rules are the ones
// that were running before anybody edited anything, and finding nothing because a
// database hiccuped is worse than finding what the defaults find.
func (s *LogSweeper) configure(ctx context.Context) (logsearch.Fields, []logsearch.Rule, logsearch.SweepOptions) {
	if s.analysis == nil {
		return logsearch.DefaultFields(), logsearch.DefaultRules(), logsearch.SweepOptions{}
	}

	fields, rules, opts, err := s.analysis(ctx)
	if err != nil {
		s.logger.Warn("could not read the log analysis settings; using the built-in rules",
			slog.String("error", err.Error()))
		return logsearch.DefaultFields(), logsearch.DefaultRules(), logsearch.SweepOptions{}
	}
	return fields, rules, opts
}

// Findings reports the last answer for a cluster.
//
// A cluster nobody has swept yet reports `unknown`, not an empty result: "we have not
// looked" and "we looked and found nothing" are different things and only one of them
// is reassuring.
func (s *LogSweeper) Findings(clusterID string) *LogFindings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if found, ok := s.results[clusterID]; ok {
		return found
	}
	return &LogFindings{State: LogsStateUnknown, Detail: "not swept yet"}
}

// Run sweeps every cluster until the context is cancelled.
func (s *LogSweeper) Run(ctx context.Context) {
	s.logger.Info("log sweeping started", slog.Duration("interval", s.interval))

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("log sweeping stopped")
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

func (s *LogSweeper) sweep(ctx context.Context) {
	clusters, err := s.clusters.List(ctx)
	if err != nil {
		s.logger.Error("could not list clusters", slog.String("error", err.Error()))
		return
	}

	// Read once per sweep rather than per cluster: it is one answer for the whole fleet
	// and a fleet of twenty would otherwise mean twenty identical reads a minute.
	fields, rules, opts := s.configure(ctx)

	for _, c := range clusters {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s.sweepOne(ctx, c, fields, rules, opts)
	}
}

func (s *LogSweeper) sweepOne(ctx context.Context, c *store.Cluster, fields logsearch.Fields, rules []logsearch.Rule, opts logsearch.SweepOptions) {
	id := c.ID.String()

	if c.LogsURL == "" || c.LogsIndex == "" {
		s.store(id, &LogFindings{State: LogsStateOff, SweptAt: time.Now().UTC()})
		return
	}

	// Bounded independently of the tick: one slow store must not delay the others.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	client, err := s.svc.LogsClient(ctx, c, fields)
	if err != nil {
		s.unknown(id, c.Name, err)
		return
	}

	findings, err := client.Sweep(ctx, rules, opts)
	if err != nil {
		s.unknown(id, c.Name, err)
		return
	}

	result := &LogFindings{
		State:    LogsStateOK,
		Findings: findings,
		SweptAt:  time.Now().UTC(),
		Window:   opts.Window,
	}
	result.index()
	result.rollUp(s.resolveOwners(ctx, c))
	s.store(id, result)
}

func (s *LogSweeper) unknown(id, name string, err error) {
	// Logged once per sweep rather than swallowed: a source that has been failing for an
	// hour should be visible somewhere other than a tooltip.
	s.logger.Warn("log source could not be read",
		slog.String("cluster", name), slog.String("error", err.Error()))

	s.store(id, &LogFindings{
		State:   LogsStateUnknown,
		Detail:  err.Error(),
		SweptAt: time.Now().UTC(),
	})
}

func (s *LogSweeper) store(id string, result *LogFindings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[id] = result
}

// Forget drops a cluster's answer, so a source that was just changed is not reported
// from the old one until the next tick.
func (s *LogSweeper) Forget(clusterID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.results, clusterID)
}

// Attach joins what the logs are saying onto the rows a list is about to return.
//
// Done here rather than inside the projection because a finding does not come from the
// object: the projection reads a Kubernetes resource, and this reads a separate system
// that may be unreachable while the cluster is perfectly fine.
//
// Rows are matched by namespace and name for pods, and by ownership for the workloads
// above them — a deployment whose pods are all failing is where the reader is going to
// look, and three identical marks on three replicas is the same news three times.
func (s *LogSweeper) Attach(clusterID, kind string, rows []Row) {
	found := s.Findings(clusterID)
	if found.State != LogsStateOK || len(found.Findings) == 0 {
		return
	}

	if kind == "Pod" {
		for i := range rows {
			if finding := found.For(rows[i].Namespace, rows[i].Name); finding != nil {
				rows[i].LogFinding = finding
			}
		}
		return
	}

	for i := range rows {
		if finding := found.forWorkload(rows[i].Namespace, kind, rows[i].Name); finding != nil {
			rows[i].LogFinding = finding
		}
	}
}

// workloadKey identifies the thing a list row is about.
func workloadKey(namespace, kind, name string) string {
	return namespace + "/" + kind + "/" + name
}

func (f *LogFindings) forWorkload(namespace, kind, name string) *logsearch.Finding {
	if f == nil || f.byWorkload == nil {
		return nil
	}
	if rolled, ok := f.byWorkload[workloadKey(namespace, kind, name)]; ok {
		return rolled
	}
	return nil
}

// rollUp gathers each workload's pods into one finding.
//
// Nine rows saying "connection refused" for nine replicas of the same deployment is the
// same news nine times; what the reader needs is that the deployment is failing and how
// widely. The loudest pod's line stands for the group, because they are the same line.
func (f *LogFindings) rollUp(owners map[string][]workloadRef) {
	f.byWorkload = map[string]*logsearch.Finding{}

	for i := range f.Findings {
		finding := f.Findings[i]
		for _, owner := range owners[finding.Namespace+"/"+finding.Pod] {
			key := workloadKey(finding.Namespace, owner.kind, owner.name)

			rolled, seen := f.byWorkload[key]
			if !seen {
				copied := finding
				copied.Pods = 1
				f.byWorkload[key] = &copied
				continue
			}

			rolled.Pods++
			rolled.Count += finding.Count
			// The worst state any of them is in, and the earliest anyone saw it.
			if finding.Severity == logsearch.SeverityError {
				rolled.Severity = logsearch.SeverityError
			}
			if finding.FirstSeen.Before(rolled.FirstSeen) {
				rolled.FirstSeen = finding.FirstSeen
			}
			if finding.LastSeen.After(rolled.LastSeen) {
				rolled.LastSeen = finding.LastSeen
			}
		}
	}
}

// workloadRef is one rung of the ownership chain a pod hangs from.
type workloadRef struct {
	kind string
	name string
}

// resolveOwners maps each pod to the workloads above it.
//
// Both rungs: a pod belongs to a ReplicaSet and, through it, to a Deployment, and the
// reader looks at the Deployment. Read from the informer cache, so this costs a map walk
// rather than a request per pod.
//
// A cluster that cannot be listed simply gets no roll-up: the pods' own findings are
// still correct, and inventing an ownership chain would be worse than not drawing one.
func (s *LogSweeper) resolveOwners(ctx context.Context, c *store.Cluster) map[string][]workloadRef {
	pods, err := s.list(ctx, c, "pods")
	if err != nil {
		return nil
	}
	replicaSets, err := s.list(ctx, c, "apps/replicasets")
	if err != nil {
		// Pods still roll up to whatever owns them directly; only the Deployment rung
		// is missing.
		replicaSets = nil
	}

	above := make(map[string]workloadRef, len(replicaSets))
	for _, row := range replicaSets {
		if owner := row.Fields["controlledBy"]; owner != "" {
			above[row.Namespace+"/"+row.Name] = workloadRef{
				kind: row.Fields["controlledByKind"], name: owner,
			}
		}
	}

	owners := make(map[string][]workloadRef, len(pods))
	for _, row := range pods {
		kind, name := row.Fields["controlledByKind"], row.Fields["controlledBy"]
		if name == "" {
			continue
		}
		chain := []workloadRef{{kind: kind, name: name}}
		if kind == "ReplicaSet" {
			if grandparent, ok := above[row.Namespace+"/"+name]; ok && grandparent.name != "" {
				chain = append(chain, grandparent)
			}
		}
		owners[row.Namespace+"/"+row.Name] = chain
	}
	return owners
}

func (s *LogSweeper) list(ctx context.Context, c *store.Cluster, key string) ([]Row, error) {
	resourceType, err := LookupType(key)
	if err != nil {
		return nil, err
	}
	result, err := s.svc.List(ctx, c, ListRequest{Type: resourceType, Limit: listAllForRollUp}, nil)
	if err != nil {
		return nil, err
	}
	return result.Rows, nil
}

// listAllForRollUp is generous on purpose: a cluster with more pods than this has other
// problems, and a truncated list would silently under-report which workloads are failing.
const listAllForRollUp = 5000
