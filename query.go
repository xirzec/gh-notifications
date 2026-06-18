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

// savedQuery is a persisted set of notification filters plus an optional
// mutating action and free-form tags. It is the on-disk representation of a
// reusable query.
type savedQuery struct {
	Name   string `yaml:"name"`
	Repo   string `yaml:"repo,omitempty"`
	Filter string `yaml:"filter,omitempty"`
	Type   string `yaml:"type,omitempty"`
	State  string `yaml:"state,omitempty"`
	Draft  bool   `yaml:"draft,omitempty"`
	All    bool   `yaml:"all,omitempty"`
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
	return doc.Queries, nil
}

// saveQueries writes the given queries to disk, creating the config directory if
// necessary. The file is written with 0600 permissions.
func saveQueries(queries []savedQuery) error {
	path := queriesFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := yaml.Marshal(queriesDoc{Queries: queries})
	if err != nil {
		return fmt.Errorf("encoding queries: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
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
	return savedQuery{
		Name:   name,
		Repo:   opts.repo,
		Filter: opts.filter,
		Type:   opts.itemType,
		State:  opts.state,
		Draft:  opts.draft,
		All:    opts.all,
		Action: actionFromOptions(opts),
		Tags:   opts.tags,
	}
}

// toOptions converts a savedQuery back into options for the fetch/filter
// pipeline. The action string is mapped to the corresponding mutating flag.
func (q savedQuery) toOptions() options {
	opts := options{
		repo:     q.Repo,
		filter:   q.Filter,
		itemType: q.Type,
		state:    q.State,
		draft:    q.Draft,
		all:      q.All,
	}
	switch q.Action {
	case "read":
		opts.markRead = true
	case "done":
		opts.markDone = true
	case "unsubscribe":
		opts.unsubscribe = true
	}
	return opts
}

// describe returns a one-line human-readable summary of the query's filters and
// action for listing.
func (q savedQuery) describe() string {
	var parts []string
	if q.Repo != "" {
		parts = append(parts, "repo="+q.Repo)
	}
	if q.Filter != "" {
		parts = append(parts, "filter="+q.Filter)
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
	if q.All {
		parts = append(parts, "all")
	}
	if q.Action != "" {
		parts = append(parts, "action="+q.Action)
	}
	if len(parts) == 0 {
		return "(no filters)"
	}
	return strings.Join(parts, " ")
}
