package main

import (
	"path/filepath"
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
		{Name: "triage", State: "open", All: true},
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
	if out[1].State != "open" || !out[1].All {
		t.Errorf("unexpected second query: %+v", out[1])
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
		{"none", options{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := optionsToSavedQuery("q", c.opts)
			if q.Action != c.action {
				t.Errorf("action = %q, want %q", q.Action, c.action)
			}
			got := q.toOptions()
			if got.markRead != c.opts.markRead || got.markDone != c.opts.markDone || got.unsubscribe != c.opts.unsubscribe {
				t.Errorf("round-trip action mismatch: %+v vs %+v", got, c.opts)
			}
		})
	}
}

func TestSavedQueryToOptionsFilters(t *testing.T) {
	q := savedQuery{Repo: "o/r", Filter: "bug", Type: "issue", State: "open", Draft: true, All: true}
	opts := q.toOptions()
	if opts.repo != "o/r" || opts.filter != "bug" || opts.itemType != "issue" ||
		opts.state != "open" || !opts.draft || !opts.all {
		t.Errorf("toOptions did not map filters: %+v", opts)
	}
}
