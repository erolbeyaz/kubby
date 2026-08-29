package logsearch

import (
	"context"
	"encoding/json"
	"strings"
)

// MessageMapping is how the log store indexes the message field, which decides what
// kind of query can find a substring in it.
//
// This is not a detail. A `text` field is broken into terms and a phrase query finds
// "Cannot open database" inside a longer line; a `keyword` field is one opaque value
// and the same query matches only a line that is exactly that phrase and nothing else.
// The same rules against the same logs find everything in one deployment and nothing in
// the other, with no error either way.
type MessageMapping struct {
	// Type is what the store says, e.g. text, match_only_text, keyword, wildcard.
	Type string `json:"type,omitempty"`
	// Analyzed reports whether substring matching works with a phrase query.
	Analyzed bool `json:"analyzed"`
	// IgnoreAbove is a keyword field's length limit. A line longer than this is stored
	// but never indexed — it is in the document and cannot be searched for, which loses
	// exactly the long stack traces this feature exists to find.
	IgnoreAbove int `json:"ignoreAbove,omitempty"`
}

// analyzedTypes index their content as terms, which is what a phrase query needs.
var analyzedTypes = map[string]bool{
	"text":               true,
	"match_only_text":    true,
	"search_as_you_type": true,
	"wildcard":           true,
}

// messageMapping asks the store how the message field is indexed.
//
// Unknown on any failure rather than a guess: the caller widens the query to cover both
// shapes, which costs a little and finds everything, where guessing wrong finds nothing.
func (c *Client) messageMapping(ctx context.Context, field string) MessageMapping {
	payload, err := c.do(ctx, "GET", "/"+c.index+"/_mapping", nil)
	if err != nil {
		return MessageMapping{}
	}

	var indices map[string]struct {
		Mappings map[string]any `json:"mappings"`
	}
	if err := json.Unmarshal(payload, &indices); err != nil {
		return MessageMapping{}
	}

	// A pattern spans many indices and they need not agree. One keyword among them
	// decides it: a phrase query would silently skip that index, and the wider query
	// still finds everything in the others.
	found := MessageMapping{}
	for _, index := range indices {
		mapping := fieldMapping(index.Mappings, field)
		if mapping == nil {
			continue
		}
		kind, _ := mapping["type"].(string)
		current := MessageMapping{Type: kind, Analyzed: analyzedTypes[kind]}
		if limit, ok := mapping["ignore_above"].(float64); ok {
			current.IgnoreAbove = int(limit)
		}

		if found.Type == "" || (found.Analyzed && !current.Analyzed) {
			found = current
		}
	}
	return found
}

// fieldMapping walks a dotted field name through Elasticsearch's nested `properties`.
func fieldMapping(mappings map[string]any, field string) map[string]any {
	node, ok := mappings["properties"].(map[string]any)
	if !ok {
		return nil
	}

	segments := strings.Split(field, ".")
	for i, segment := range segments {
		child, ok := node[segment].(map[string]any)
		if !ok {
			return nil
		}
		if i == len(segments)-1 {
			return child
		}
		node, ok = child["properties"].(map[string]any)
		if !ok {
			return nil
		}
	}
	return nil
}

// phraseQuery finds a substring however the field happens to be indexed.
//
// On an analyzed field a phrase query is exact and fast. On a keyword field the whole
// value is one term, so the only thing that finds a substring is a wildcard — slower,
// and used only where it is the sole option.
func phraseQuery(field, phrase string, mapping MessageMapping) map[string]any {
	if mapping.Type != "" && !mapping.Analyzed {
		return map[string]any{"wildcard": map[string]any{
			field: map[string]any{"value": "*" + escapeWildcard(phrase) + "*", "case_insensitive": true},
		}}
	}
	if mapping.Type != "" {
		return map[string]any{"match_phrase": map[string]any{field: phrase}}
	}

	// The mapping could not be read. Both clauses are valid against either type and
	// exactly one of them can match, so the query is correct wherever it lands.
	return map[string]any{"bool": map[string]any{
		"should": []any{
			map[string]any{"match_phrase": map[string]any{field: phrase}},
			map[string]any{"wildcard": map[string]any{
				field: map[string]any{"value": "*" + escapeWildcard(phrase) + "*", "case_insensitive": true},
			}},
		},
		"minimum_should_match": 1,
	}}
}

// escapeWildcard keeps a phrase's own `*` and `?` from being read as pattern syntax.
func escapeWildcard(phrase string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`)
	return replacer.Replace(phrase)
}
