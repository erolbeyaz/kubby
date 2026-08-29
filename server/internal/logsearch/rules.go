package logsearch

import (
	"regexp"
	"strings"
)

// Class is what kind of problem a rule found, because the fix differs.
//
// A dependency that cannot be reached is a network or a service; one that refuses the
// login is a credential; one that times out is load or a firewall. Reporting all three
// as "connection problem" sends the reader to the wrong place — the failure that
// started this work was a login rejection over a TCP connection that had succeeded.
const (
	ClassAuth        = "auth"
	ClassUnreachable = "unreachable"
	ClassTimeout     = "timeout"
	ClassGeneric     = "generic"
)

// Rule is one thing worth noticing in a log line.
//
// The query is matched against the message field and nothing else. What it matches on
// matters: an exception type or a numeric error code is stable across versions and
// languages, while the prose around it is neither — the same failure arrives as
// System.Data.SqlClient in one pod and Microsoft.Data.SqlClient in the next.
type Rule struct {
	Name  string
	Class string
	// Match is the phrases worth noticing; any one of them is a hit.
	//
	// Phrases rather than query_string syntax. A message field arrives mapped as `text`
	// in one deployment and `keyword` in the next, and the two need different queries
	// for the same substring — a phrase list can be turned into either, while a hand-
	// written query works in one and silently finds nothing in the other.
	Match []string
	// Capture pulls the parts worth putting in a one-line summary out of the matched
	// message. Named groups are used; unmatched patterns are simply skipped.
	Capture []*regexp.Regexp
}

func mustCapture(patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		out = append(out, regexp.MustCompile(pattern))
	}
	return out
}

// DefaultRules is the set Kubby ships with.
//
// Deliberately not a list of technologies. The last rule catches anything that calls
// itself an exception or a fatal error, whatever wrote it, and the named ones exist to
// turn a match into a sentence worth reading rather than to find it in the first place.
//
// TODO: these are compiled in. The phase plan has them editable from the settings
// screen, so a team can add the pattern their own stack produces without a release.
func DefaultRules() []Rule {
	return []Rule{
		{
			Name:  "SQL Server",
			Class: ClassAuth,
			// 4060 cannot open database · 18456 login failed · 40613 unavailable.
			// `.SqlException` rather than `SqlException`: matching is case-insensitive
			// so the bare form also matched PostgreSQL's `PSQLException`, and the
			// wrong name on a finding sends the reader to the wrong database.
			Match: []string{".SqlException", "SQLServerException", "Error Number:4060", "Error Number:18456", "Error Number:40613", "Cannot open database", "Login failed for user"},
			Capture: mustCapture(
				`database "(?P<database>[^"]+)"`,
				`user '(?P<user>[^']+)'`,
				`Error Number:(?P<code>\d+)`,
			),
		},
		{
			Name:  "PostgreSQL",
			Class: ClassGeneric,
			Match: []string{"PSQLException", "org.postgresql", "password authentication failed", "could not connect to server"},
			Capture: mustCapture(
				`(?P<reason>password authentication failed[^,.]*)`,
				`database "(?P<database>[^"]+)"`,
			),
		},
		{
			Name:    "JDBC connection pool",
			Class:   ClassUnreachable,
			Match:   []string{"JDBCConnectionException", "Unable to acquire JDBC Connection", "HikariPool", "CannotCreateTransactionException"},
			Capture: mustCapture(`(?P<reason>Unable to acquire JDBC Connection)`),
		},
		{
			Name:  "Redis",
			Class: ClassUnreachable,
			Match: []string{"RedisConnectionException", "RedisTimeoutException", "NOAUTH", "LOADING Redis"},
		},
		{
			Name:  "RabbitMQ",
			Class: ClassUnreachable,
			Match: []string{"BrokerUnreachableException", "ACCESS_REFUSED", "AMQP connection"},
		},
		{
			Name:  "Kafka",
			Class: ClassUnreachable,
			Match: []string{"Broker may not be available", "KafkaException", "NotLeaderForPartition", "GroupCoordinatorNotAvailable"},
		},
		{
			Name:  "MongoDB",
			Class: ClassUnreachable,
			Match: []string{"MongoConnectionException", "MongoTimeoutException", "MongoSocketException"},
		},
		{
			Name:  "Connection refused",
			Class: ClassUnreachable,
			Match: []string{"ECONNREFUSED", "connection refused", "no route to host", "EAI_AGAIN", "getaddrinfo"},
			Capture: mustCapture(
				`ECONNREFUSED (?P<address>[0-9a-fA-F:.\[\]]+:\d+)`,
				`dial tcp (?P<address>\S+):`,
			),
		},
		{
			Name:  "Timeout",
			Class: ClassTimeout,
			Match: []string{"ETIMEDOUT", "i/o timeout", "connection timed out", "context deadline exceeded"},
		},
		{
			// The net that catches what nobody wrote a rule for. Without it this feature
			// only ever finds problems somebody already anticipated, which is the
			// opposite of what it is for.
			Name:  "Application error",
			Class: ClassGeneric,
			Match: []string{"Exception", "FATAL", "panic:", "Traceback"},
		},
	}
}

// query builds the clause that finds any of this rule's phrases.
func (r Rule) query(field string, mapping MessageMapping) map[string]any {
	clauses := make([]any, 0, len(r.Match))
	for _, phrase := range r.Match {
		if strings.TrimSpace(phrase) == "" {
			continue
		}
		clauses = append(clauses, phraseQuery(field, phrase, mapping))
	}
	if len(clauses) == 0 {
		return nil
	}
	return map[string]any{"bool": map[string]any{"should": clauses, "minimum_should_match": 1}}
}

// Summarise turns a matched line into one sentence.
//
// A tooltip cannot hold thirty lines of stack trace, and the first line of one is
// rarely the part that says what went wrong. What a reader needs is the identity of the
// thing that failed: which database, which user, which address.
func (r Rule) Summarise(message string) string {
	details := make([]string, 0, len(r.Capture))
	seen := map[string]bool{}

	for _, pattern := range r.Capture {
		match := pattern.FindStringSubmatch(message)
		if match == nil {
			continue
		}
		for i, name := range pattern.SubexpNames() {
			if name == "" || i >= len(match) || match[i] == "" || seen[name] {
				continue
			}
			seen[name] = true
			details = append(details, name+" "+match[i])
		}
	}
	return strings.Join(details, " · ")
}
