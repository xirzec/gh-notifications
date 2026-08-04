package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempQueriesFile points queriesFilePath at a temp file for the duration of
// the test.
func withTempQueriesFile(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	prev := queriesFilePath
	queriesFilePath = func() string { return filepath.Join(dir, "notifications.yml") }
	t.Cleanup(func() { queriesFilePath = prev })
}

func TestLoadQueriesMissingFile(t *testing.T) {
	withTempQueriesFile(t)
	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(queries) != 0 {
		t.Errorf("expected no queries, got %v", queries)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	withTempQueriesFile(t)
	in := []savedQuery{
		{Name: "cleanup", Repo: "o/r", Type: "pr", Action: "unsubscribe", Tags: []string{"cleanup"}},
		{Name: "triage", State: "open"},
	}
	if err := saveQueries(in); err != nil {
		t.Fatalf("saveQueries: %v", err)
	}
	out, err := loadQueries()
	if err != nil {
		t.Fatalf("loadQueries: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(out))
	}
	if out[0].Name != "cleanup" || out[0].Action != "unsubscribe" || out[0].Repo != "o/r" {
		t.Errorf("unexpected first query: %+v", out[0])
	}
	if len(out[0].Tags) != 1 || out[0].Tags[0] != "cleanup" {
		t.Errorf("tags not round-tripped: %+v", out[0].Tags)
	}
	if out[0].Scope != "" || out[0].All {
		t.Errorf("unexpected first query scope: %+v", out[0])
	}
	if out[1].State != "open" || out[1].Scope != "" || out[1].All {
		t.Errorf("unexpected second query: %+v", out[1])
	}

	data, err := os.ReadFile(queriesFilePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "\n    all:") || strings.Contains(string(data), "\n    scope:") {
		t.Errorf("new schema unexpectedly wrote a legacy scope field:\n%s", data)
	}
}

func TestLoadQueriesPreservesLegacyScopeForValidation(t *testing.T) {
	withTempQueriesFile(t)
	const legacy = `queries:
  - name: unread
    action: done
  - name: everything
    all: true
    action: unsubscribe
  - name: scoped-everything
    scope: all
  - name: scoped-unread
    scope: unread
`
	if err := os.WriteFile(queriesFilePath(), []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("loadQueries: %v", err)
	}
	if len(queries) != 4 {
		t.Fatalf("expected 4 queries, got %d", len(queries))
	}
	if queries[0].Scope != "" || queries[0].All {
		t.Errorf("legacy unread query changed: %+v", queries[0])
	}
	if !queries[1].All || queries[1].Scope != "" {
		t.Errorf("legacy all query was not preserved: %+v", queries[1])
	}
	if queries[2].Scope != savedQueryScopeAll || queries[2].All {
		t.Errorf("legacy all scope was not preserved: %+v", queries[2])
	}
	if queries[3].Scope != savedQueryScopeUnread || queries[3].All {
		t.Errorf("legacy unread scope was not preserved: %+v", queries[3])
	}
}

func TestLoadQueriesRejectsInvalidScope(t *testing.T) {
	withTempQueriesFile(t)
	const invalid = `queries:
  - name: cleanup
    scope: recent
`
	if err := os.WriteFile(queriesFilePath(), []byte(invalid), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := loadQueries()
	if err == nil || !strings.Contains(err.Error(), `invalid legacy scope "recent"`) {
		t.Fatalf("loadQueries error = %v, want invalid legacy scope", err)
	}
}

func TestUpsertQueryReplacesAndSorts(t *testing.T) {
	var queries []savedQuery
	queries = upsertQuery(queries, savedQuery{Name: "zeta"})
	queries = upsertQuery(queries, savedQuery{Name: "alpha"})
	queries = upsertQuery(queries, savedQuery{Name: "ALPHA", Repo: "o/r"})

	if len(queries) != 2 {
		t.Fatalf("expected 2 queries after upsert, got %d: %+v", len(queries), queries)
	}
	if queries[0].Name != "ALPHA" || queries[0].Repo != "o/r" {
		t.Errorf("expected case-insensitive replace and sort, got %+v", queries)
	}
	if queries[1].Name != "zeta" {
		t.Errorf("expected zeta last, got %+v", queries)
	}
}

func TestDeleteQueryByName(t *testing.T) {
	queries := []savedQuery{{Name: "a"}, {Name: "b"}}
	queries, ok := deleteQueryByName(queries, "A")
	if !ok {
		t.Fatal("expected delete to report removal")
	}
	if len(queries) != 1 || queries[0].Name != "b" {
		t.Errorf("unexpected queries after delete: %+v", queries)
	}
	if _, ok := deleteQueryByName(queries, "missing"); ok {
		t.Error("expected delete of missing name to report false")
	}
}

func TestFindQueryAndByTag(t *testing.T) {
	queries := []savedQuery{
		{Name: "one", Tags: []string{"cleanup", "daily"}},
		{Name: "two", Tags: []string{"daily"}},
		{Name: "three"},
	}
	if q, ok := findQuery(queries, "TWO"); !ok || q.Name != "two" {
		t.Errorf("findQuery case-insensitive failed: %+v %v", q, ok)
	}
	if _, ok := findQuery(queries, "nope"); ok {
		t.Error("expected findQuery miss")
	}
	daily := queriesByTag(queries, "DAILY")
	if len(daily) != 2 || daily[0].Name != "one" || daily[1].Name != "two" {
		t.Errorf("queriesByTag daily = %+v", daily)
	}
	if got := queriesByTag(queries, "none"); len(got) != 0 {
		t.Errorf("expected no matches, got %+v", got)
	}
}

func TestOptionsSavedQueryConversion(t *testing.T) {
	cases := []struct {
		name   string
		opts   options
		action string
	}{
		{"read", options{markRead: true}, "read"},
		{"done", options{markDone: true}, "done"},
		{"unsub", options{unsubscribe: true}, "unsubscribe"},
		{"all ignored", options{all: true}, ""},
		{"none", options{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := optionsToSavedQuery("q", c.opts)
			if q.Action != c.action {
				t.Errorf("action = %q, want %q", q.Action, c.action)
			}
			if q.Scope != "" || q.All {
				t.Errorf("new saved query retained a notification scope: %+v", q)
			}
			got, err := q.toOptions()
			if err != nil {
				t.Fatalf("toOptions: %v", err)
			}
			if got.markRead != c.opts.markRead || got.markDone != c.opts.markDone || got.unsubscribe != c.opts.unsubscribe {
				t.Errorf("round-trip action mismatch: %+v vs %+v", got, c.opts)
			}
			if got.all {
				t.Errorf("saved query unexpectedly enabled all notifications: %+v", got)
			}
		})
	}
}

func TestSavedQueryToOptionsFilters(t *testing.T) {
	q := savedQuery{
		Repo: "o/r", Filter: "bug", Type: "issue", State: "open",
		Draft: true, Scope: savedQueryScopeUnread,
	}
	opts, err := q.toOptions()
	if err != nil {
		t.Fatalf("toOptions: %v", err)
	}
	if opts.repo != "o/r" || opts.filter != "bug" || opts.itemType != "issue" ||
		opts.state != "open" || !opts.draft || opts.all {
		t.Errorf("toOptions did not map filters: %+v", opts)
	}
}

func TestSavedQueryDescriptionPartsLegacyScope(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query savedQuery
		want  string
	}{
		{"default scope hidden", savedQuery{Name: "default", Filter: "cleanup"}, `filter="cleanup"`},
		{"legacy unread hidden", savedQuery{Name: "unread", Scope: savedQueryScopeUnread}, "(no filters)"},
		{"legacy all bool", savedQuery{Name: "all", All: true}, "all: true (unsupported; remove before running)"},
		{"legacy all scope", savedQuery{Name: "all", Scope: savedQueryScopeAll}, "scope: all (unsupported; remove before running)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts, err := tc.query.descriptionParts()
			if err != nil {
				t.Fatalf("descriptionParts: %v", err)
			}
			if got := strings.Join(parts, " "); got != tc.want {
				t.Errorf("description = %q, want %q", got, tc.want)
			}
		})
	}
}
