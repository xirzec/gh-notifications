package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cli/go-gh/v2/pkg/config"
	"gopkg.in/yaml.v3"
)

const (
	savedQueryScopeAll    = "all"
	savedQueryScopeUnread = "unread"
)

// savedQuery is a persisted set of notification filters plus an optional
// mutating action and free-form tags. It is the on-disk representation of a
// reusable query.
type savedQuery struct {
	Name  string `yaml:"name"`
	Scope string `yaml:"scope,omitempty"`
	// All is retained only to read saved queries written before scope was
	// explicit. New writes normalize it into Scope.
	All    bool   `yaml:"all,omitempty"`
	Repo   string `yaml:"repo,omitempty"`
	Filter string `yaml:"filter,omitempty"`
	Type   string `yaml:"type,omitempty"`
	State  string `yaml:"state,omitempty"`
	Draft  bool   `yaml:"draft,omitempty"`
	// Action, when set, is one of "read", "done", or "unsubscribe".
	Action string   `yaml:"action,omitempty"`
	Tags   []string `yaml:"tags,omitempty"`
}

// queriesDoc is the top-level structure of the saved-queries YAML file.
type queriesDoc struct {
	Queries []savedQuery `yaml:"queries"`
}

// queriesFilePath returns the path to the saved-queries file. It is a variable
// so tests can point it at a temporary location.
var queriesFilePath = func() string {
	return filepath.Join(config.ConfigDir(), "notifications.yml")
}

// loadQueries reads all saved queries from disk. A missing file yields an empty
// slice and no error.
func loadQueries() ([]savedQuery, error) {
	path := queriesFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var doc queriesDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	queries, err := normalizeSavedQueries(doc.Queries)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return queries, nil
}

// saveQueries writes the given queries to disk, creating the config directory if
// necessary. The file is written with 0600 permissions.
func saveQueries(queries []savedQuery) error {
	path := queriesFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	normalized, err := normalizeSavedQueries(queries)
	if err != nil {
		return fmt.Errorf("validating queries: %w", err)
	}
	data, err := yaml.Marshal(queriesDoc{Queries: normalized})
	if err != nil {
		return fmt.Errorf("encoding queries: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// notificationScope resolves the explicit scope or its legacy `all` equivalent.
// A legacy query without `all: true` remains unread-only so upgrading cannot
// silently widen an existing saved action.
func (q savedQuery) notificationScope() (string, error) {
	scope := strings.ToLower(strings.TrimSpace(q.Scope))
	if scope != "" {
		if q.All {
			return "", fmt.Errorf("query %q sets both scope and legacy all", q.Name)
		}
		switch scope {
		case savedQueryScopeAll, savedQueryScopeUnread:
			return scope, nil
		default:
			return "", fmt.Errorf("query %q has invalid scope %q: expected all or unread", q.Name, q.Scope)
		}
	}
	if q.All {
		return savedQueryScopeAll, nil
	}
	return savedQueryScopeUnread, nil
}

func normalizeSavedQueries(queries []savedQuery) ([]savedQuery, error) {
	normalized := make([]savedQuery, len(queries))
	for i, q := range queries {
		scope, err := q.notificationScope()
		if err != nil {
			return nil, err
		}
		q.Scope = scope
		q.All = false
		normalized[i] = q
	}
	return normalized, nil
}

// findQuery returns the query with the given name (case-insensitive) and whether
// it was found.
func findQuery(queries []savedQuery, name string) (savedQuery, bool) {
	for _, q := range queries {
		if strings.EqualFold(q.Name, name) {
			return q, true
		}
	}
	return savedQuery{}, false
}

// queriesByTag returns the queries carrying the given tag (case-insensitive),
// preserving their stored order.
func queriesByTag(queries []savedQuery, tag string) []savedQuery {
	var out []savedQuery
	for _, q := range queries {
		for _, t := range q.Tags {
			if strings.EqualFold(t, tag) {
				out = append(out, q)
				break
			}
		}
	}
	return out
}

// upsertQuery inserts q, replacing any existing query with the same name
// (case-insensitive). The returned slice is kept sorted by name for stable
// listing and storage.
func upsertQuery(queries []savedQuery, q savedQuery) []savedQuery {
	replaced := false
	for i := range queries {
		if strings.EqualFold(queries[i].Name, q.Name) {
			queries[i] = q
			replaced = true
			break
		}
	}
	if !replaced {
		queries = append(queries, q)
	}
	sort.Slice(queries, func(i, j int) bool {
		return strings.ToLower(queries[i].Name) < strings.ToLower(queries[j].Name)
	})
	return queries
}

// deleteQueryByName removes the query with the given name (case-insensitive),
// returning the new slice and whether a query was removed.
func deleteQueryByName(queries []savedQuery, name string) ([]savedQuery, bool) {
	for i := range queries {
		if strings.EqualFold(queries[i].Name, name) {
			return append(queries[:i], queries[i+1:]...), true
		}
	}
	return queries, false
}

// actionFromOptions returns the mutating action implied by opts, or "" if none.
func actionFromOptions(opts options) string {
	switch {
	case opts.markRead:
		return "read"
	case opts.markDone:
		return "done"
	case opts.unsubscribe:
		return "unsubscribe"
	default:
		return ""
	}
}

// optionsToSavedQuery builds a savedQuery from parsed options.
func optionsToSavedQuery(name string, opts options) savedQuery {
	scope := savedQueryScopeAll
	if opts.unreadOnly {
		scope = savedQueryScopeUnread
	}
	return savedQuery{
		Name:   name,
		Repo:   opts.repo,
		Filter: opts.filter,
		Type:   opts.itemType,
		State:  opts.state,
		Draft:  opts.draft,
		Scope:  scope,
		Action: actionFromOptions(opts),
		Tags:   opts.tags,
	}
}

// toOptions converts a savedQuery back into options for the fetch/filter
// pipeline. The action string is mapped to the corresponding mutating flag.
func (q savedQuery) toOptions() (options, error) {
	scope, err := q.notificationScope()
	if err != nil {
		return options{}, err
	}
	opts := options{
		repo:       q.Repo,
		filter:     q.Filter,
		itemType:   q.Type,
		state:      q.State,
		draft:      q.Draft,
		unreadOnly: scope == savedQueryScopeUnread,
	}
	switch q.Action {
	case "read":
		opts.markRead = true
	case "done":
		opts.markDone = true
	case "unsubscribe":
		opts.unsubscribe = true
	}
	return opts, nil
}

// descriptionParts returns the query's scope, filters, and action as
// independently renderable fields.
func (q savedQuery) descriptionParts() ([]string, error) {
	scope, err := q.notificationScope()
	if err != nil {
		return nil, err
	}
	var parts []string
	if scope == savedQueryScopeUnread {
		parts = append(parts, "scope="+scope)
	}
	if q.Repo != "" {
		parts = append(parts, "repo="+q.Repo)
	}
	if q.Filter != "" {
		parts = append(parts, fmt.Sprintf("filter=%q", q.Filter))
	}
	if q.Type != "" {
		parts = append(parts, "type="+q.Type)
	}
	if q.State != "" {
		parts = append(parts, "state="+q.State)
	}
	if q.Draft {
		parts = append(parts, "draft")
	}
	if q.Action != "" {
		parts = append(parts, "action="+q.Action)
	}
	if len(parts) == 0 {
		return []string{"(no filters)"}, nil
	}
	return parts, nil
}
