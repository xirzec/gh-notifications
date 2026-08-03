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
		{Name: "cleanup", Repo: "o/r", Type: "pr", Scope: savedQueryScopeAll, Action: "unsubscribe", Tags: []string{"cleanup"}},
		{Name: "triage", State: "open", Scope: savedQueryScopeUnread},
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
	if out[0].Scope != savedQueryScopeAll || out[0].All {
		t.Errorf("unexpected first query scope: %+v", out[0])
	}
	if out[1].State != "open" || out[1].Scope != savedQueryScopeUnread || out[1].All {
		t.Errorf("unexpected second query: %+v", out[1])
	}

	data, err := os.ReadFile(queriesFilePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "\n    all:") {
		t.Errorf("new schema unexpectedly wrote legacy all field:\n%s", data)
	}
}

func TestLoadQueriesNormalizesLegacyScope(t *testing.T) {
	withTempQueriesFile(t)
	const legacy = `queries:
  - name: unread
    action: done
  - name: everything
    all: true
    action: unsubscribe
`
	if err := os.WriteFile(queriesFilePath(), []byte(legacy), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("loadQueries: %v", err)
	}
	if len(queries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(queries))
	}
	if queries[0].Scope != savedQueryScopeUnread || queries[0].All {
		t.Errorf("legacy unread query widened: %+v", queries[0])
	}
	if queries[1].Scope != savedQueryScopeAll || queries[1].All {
		t.Errorf("legacy all query changed scope: %+v", queries[1])
	}

	if err := saveQueries(queries); err != nil {
		t.Fatalf("saveQueries: %v", err)
	}
	data, err := os.ReadFile(queriesFilePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(data)
	for _, want := range []string{"scope: unread", "scope: all"} {
		if !strings.Contains(text, want) {
			t.Errorf("migrated YAML missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "\n    all:") {
		t.Errorf("migrated YAML retained legacy all field:\n%s", text)
	}
}

func TestLoadQueriesRejectsInvalidScope(t *testing.T) {
	for _, tc := range []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "unknown scope",
			yaml: `queries:
  - name: cleanup
    scope: recent
`,
			want: `invalid scope "recent"`,
		},
		{
			name: "scope and legacy all",
			yaml: `queries:
  - name: cleanup
    scope: unread
    all: true
`,
			want: "sets both scope and legacy all",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withTempQueriesFile(t)
			if err := os.WriteFile(queriesFilePath(), []byte(tc.yaml), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			_, err := loadQueries()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadQueries error = %v, want %q", err, tc.want)
			}
		})
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
		name      string
		opts      options
		action    string
		wantScope string
	}{
		{"read", options{markRead: true}, "read", savedQueryScopeAll},
		{"done", options{markDone: true}, "done", savedQueryScopeAll},
		{"unsub", options{unsubscribe: true}, "unsubscribe", savedQueryScopeAll},
		{"none", options{}, "", savedQueryScopeAll},
		{"unread", options{unreadOnly: true}, "", savedQueryScopeUnread},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := optionsToSavedQuery("q", c.opts)
			if q.Action != c.action {
				t.Errorf("action = %q, want %q", q.Action, c.action)
			}
			if q.Scope != c.wantScope {
				t.Errorf("scope = %q, want %q", q.Scope, c.wantScope)
			}
			got, err := q.toOptions()
			if err != nil {
				t.Fatalf("toOptions: %v", err)
			}
			if got.markRead != c.opts.markRead || got.markDone != c.opts.markDone || got.unsubscribe != c.opts.unsubscribe {
				t.Errorf("round-trip action mismatch: %+v vs %+v", got, c.opts)
			}
			if got.unreadOnly != c.opts.unreadOnly {
				t.Errorf("round-trip scope mismatch: %+v vs %+v", got, c.opts)
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
		opts.state != "open" || !opts.draft || !opts.unreadOnly {
		t.Errorf("toOptions did not map filters: %+v", opts)
	}
}

func TestSavedQueryDescriptionPartsScope(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query savedQuery
		want  string
	}{
		{"default scope hidden", savedQuery{Name: "all", Scope: savedQueryScopeAll, Filter: "cleanup"}, `filter="cleanup"`},
		{"unread scope shown", savedQuery{Name: "unread", Scope: savedQueryScopeUnread}, "scope=unread"},
		{"default scope only", savedQuery{Name: "all", Scope: savedQueryScopeAll}, "(no filters)"},
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
