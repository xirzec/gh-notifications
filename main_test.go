package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseArgsUpdated(t *testing.T) {
	for _, value := range []string{">30", "<5", ">1", "<001", ">18446744073709551615"} {
		for _, args := range [][]string{{"--updated", value}, {"--updated=" + value}} {
			opts, err := parseArgs(args)
			if err != nil || opts.updated != value {
				t.Errorf("parseArgs(%q) = %+v, %v", args, opts, err)
			}
		}
	}
	for _, value := range []string{
		"", "30", ">", "<", ">0", "<0", ">-1", "<+1", ">1.5",
		">=30", "<=5", "=5", ">>5", ">five", " >30", "> 30", ">30 ",
		">18446744073709551616",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := parseArgs([]string{"--updated", value}); err == nil {
				t.Fatal("expected invalid --updated error")
			}
			if err := validateOptions(options{updated: value}); value != "" && err == nil {
				t.Fatal("validateOptions accepted invalid expression")
			}
		})
	}
	if _, err := parseArgs([]string{"--updated"}); err == nil {
		t.Error("missing argument accepted")
	}
	if opts, err := parseArgs(nil); err != nil || opts.updated != "" {
		t.Errorf("omitted flag: %+v, %v", opts, err)
	}
	opts, err := parseArgs([]string{"--updated", ">30", "--updated", "<5"})
	if err != nil || opts.updated != "<5" {
		t.Errorf("repeated flag: %+v, %v", opts, err)
	}
}

func TestFilterByUpdated(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 500, time.UTC)
	cutoff := now.Add(-5 * 24 * time.Hour)
	for _, tc := range []struct {
		name string
		at   time.Time
		old  bool
		new  bool
	}{
		{"exact boundary", cutoff, false, false},
		{"one nanosecond older", cutoff.Add(-time.Nanosecond), true, false},
		{"one nanosecond newer", cutoff.Add(time.Nanosecond), false, true},
		{"fractional day older", cutoff.Add(-12 * time.Hour), true, false},
		{"fractional day newer", cutoff.Add(12 * time.Hour), false, true},
		{"future", now.Add(time.Hour), false, true},
		{"now", now, false, true},
		{"missing", time.Time{}, false, false},
		{"ancient RFC3339 date", time.Date(1, 1, 2, 0, 0, 0, 0, time.UTC), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, comparison := range []struct {
				expr string
				want bool
			}{{">5", tc.old}, {"<5", tc.new}} {
				filter, err := parseUpdatedFilter(comparison.expr)
				if err != nil {
					t.Fatal(err)
				}
				got := filterByUpdated([]Notification{{ID: "1", UpdatedAt: tc.at}}, filter, now)
				if (len(got) == 1) != comparison.want {
					t.Errorf("%s: matched %v, want %v", comparison.expr, len(got) == 1, comparison.want)
				}
			}
		})
	}
	for _, expr := range []string{">18446744073709551615", "<18446744073709551615"} {
		filter, err := parseUpdatedFilter(expr)
		if err != nil {
			t.Fatal(err)
		}
		if got := filterByUpdated(nil, filter, now); len(got) != 0 {
			t.Errorf("empty input: %v", got)
		}
		got := filterByUpdated([]Notification{{UpdatedAt: cutoff}}, filter, now)
		if (len(got) == 1) != (expr[0] == '<') {
			t.Errorf("large threshold %s matched %v", expr, got)
		}
	}
}

func TestUpdatedRFC3339(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 500, time.UTC)
	doer := &recordingDoer{body: `[
		{"id":"equal","updated_at":"2026-10-14T14:00:00.000000500+02:00"},
		{"id":"older","updated_at":"2026-10-14T12:00:00.000000499Z"},
		{"id":"newer","updated_at":"2026-10-14T12:00:00.000000501Z"},
		{"id":"missing"},
		{"id":"null","updated_at":null}
	]`}
	notifications, err := fetchNotifications(doer, options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		expr string
		id   string
	}{{">5", "older"}, {"<5", "newer"}} {
		filter, err := parseUpdatedFilter(tc.expr)
		if err != nil {
			t.Fatal(err)
		}
		got := filterByUpdated(notifications, filter, now)
		if len(got) != 1 || got[0].ID != tc.id {
			t.Errorf("%s: got %+v, want %s", tc.expr, got, tc.id)
		}
	}
}

func TestLoadFilteredNotificationsUpdated(t *testing.T) {
	now := time.Now()
	notifications := []Notification{
		{ID: "1", UpdatedAt: now.Add(-40 * 24 * time.Hour), Subject: NotificationSubject{Title: "Fix old", Type: "PullRequest", URL: "https://api.github.com/repos/o/r/pulls/1"}},
		{ID: "2", UpdatedAt: now.Add(-time.Hour), Subject: NotificationSubject{Title: "Fix recent", Type: "PullRequest", URL: "https://api.github.com/repos/o/r/pulls/2"}},
		{ID: "3", UpdatedAt: now.Add(-40 * 24 * time.Hour), Subject: NotificationSubject{Title: "Other old", Type: "Issue"}},
		{ID: "4", UpdatedAt: now.Add(-40 * 24 * time.Hour), Subject: NotificationSubject{Title: "Fix release", Type: "Release"}},
		{ID: "5", Subject: NotificationSubject{Title: "Fix missing", Type: "PullRequest"}},
	}
	body, err := json.Marshal(notifications)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		opts options
		ids  []string
		gql  []int
	}{
		{"disabled", options{}, []string{"1", "2", "3", "4", "5"}, nil},
		{"older REST only", options{updated: ">30"}, []string{"1", "3", "4"}, nil},
		{"newer REST only", options{updated: "<5"}, []string{"2"}, nil},
		{"no matches", options{updated: ">90"}, nil, nil},
		{"combined", options{updated: ">30", filter: "Fix", itemType: "pr", state: "open", draft: true, author: "alice"}, []string{"1"}, []int{1}},
		{"no GraphQL candidates", options{updated: ">90", author: "alice"}, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeGQL{state: "OPEN", draft: true, author: "alice"}
			prev := newGraphQLClient
			newGraphQLClient = func() (graphQLDoer, error) {
				if tc.opts.state == "" && tc.opts.author == "" {
					t.Fatal("local filter initialized GraphQL client")
				}
				return fake, nil
			}
			t.Cleanup(func() { newGraphQLClient = prev })
			doer := &recordingDoer{body: string(body)}
			got, err := loadFilteredNotifications(newRateLimitTracker(doer), tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, n := range got {
				ids = append(ids, n.ID)
			}
			if !slices.Equal(ids, tc.ids) || !slices.Equal(fake.numbers, tc.gql) {
				t.Errorf("IDs %v, GraphQL items %v; want %v, %v", ids, fake.numbers, tc.ids, tc.gql)
			}
			if !slices.Equal(doer.calls, []string{"GET notifications?per_page=50"}) {
				t.Errorf("unexpected requests: %v", doer.calls)
			}
		})
	}
	doer := &recordingDoer{}
	if _, err := loadFilteredNotifications(newRateLimitTracker(doer), options{updated: ">0"}); err == nil || len(doer.calls) != 0 {
		t.Errorf("invalid expression: error %v, requests %v", err, doer.calls)
	}
}

func TestUpdatedSavedQueries(t *testing.T) {
	withTempQueriesFile(t)
	var out bytes.Buffer
	for _, expr := range []string{">0", "<0", "", "30", ">1.5"} {
		if err := runSave([]string{"invalid", "--updated", expr}, strings.NewReader(""), &out); err == nil {
			t.Errorf("save accepted %q", expr)
		}
		if _, err := os.Stat(queriesFilePath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid save changed query file: %v", err)
		}
	}
	if err := runSave([]string{"stale", "--updated", ">30"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	queries, err := loadQueries()
	if err != nil || len(queries) != 1 || queries[0].Updated != ">30" {
		t.Fatalf("saved query: %+v, %v", queries, err)
	}
	opts, err := queries[0].toOptions()
	if err != nil || opts.updated != ">30" || optionsToSavedQuery("stale", opts).Updated != ">30" {
		t.Fatalf("options conversion: %+v, %v", opts, err)
	}
	out.Reset()
	if err := runListQueries(nil, &out); err != nil || !strings.Contains(out.String(), "updated=>30") {
		t.Errorf("list: %q, %v", out.String(), err)
	}
	if err := saveQueries([]savedQuery{{Name: "old"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(queriesFilePath())
	if err != nil || strings.Contains(string(data), "updated:") {
		t.Errorf("empty updated field persisted: %s, %v", data, err)
	}
	queries, err = loadQueries()
	if err != nil || len(queries) != 1 || queries[0].Updated != "" {
		t.Fatalf("legacy query: %+v, %v", queries, err)
	}
}

func TestExecuteSavedQueryUpdated(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name  string
		flags runFlags
		input string
		tail  []string
	}{
		{"confirmed", runFlags{}, "y\n", []string{"DELETE notifications/threads/old"}},
		{"assume yes", runFlags{assumeYes: true}, "", []string{"DELETE notifications/threads/old"}},
		{"dry run", runFlags{dryRun: true, assumeYes: true}, "", nil},
		{"aborted", runFlags{}, "n\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notifications := []Notification{
				{ID: "old", UpdatedAt: now.Add(-40 * 24 * time.Hour)},
				{ID: "recent", UpdatedAt: now.Add(-time.Hour)},
				{ID: "missing"},
			}
			body, err := json.Marshal(notifications)
			if err != nil {
				t.Fatal(err)
			}
			withFakeGraphQLClient(t, nil, errors.New("unexpected GraphQL initialization"))
			doer := &recordingDoer{body: string(body)}
			q := savedQuery{Name: "stale", Updated: ">30", Action: "done"}
			err = executeSavedQuery(newRateLimitTracker(doer), q, tc.flags, strings.NewReader(tc.input), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			want := append([]string{"GET notifications?per_page=50"}, tc.tail...)
			if !slices.Equal(doer.calls, want) {
				t.Errorf("requests %v, want %v", doer.calls, want)
			}
		})
	}
	doer := &recordingDoer{body: `[{"id":"bad","updated_at":"invalid"}]`}
	err := executeSavedQuery(newRateLimitTracker(doer), savedQuery{Name: "bad", Updated: ">30", Action: "done"}, runFlags{assumeYes: true}, strings.NewReader(""), io.Discard)
	if err == nil || !slices.Equal(doer.calls, []string{"GET notifications?per_page=50"}) {
		t.Errorf("malformed timestamp: %v, requests %v", err, doer.calls)
	}
}

func TestSavedQueryUpdatedValidation(t *testing.T) {
	withTempQueriesFile(t)
	queries := []savedQuery{
		{Name: "valid", Updated: ">30", Tags: []string{"cleanup"}},
		{Name: "invalid", Updated: ">0", Tags: []string{"cleanup"}},
	}
	if err := saveQueries(queries); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"invalid"}, {"--tag", "cleanup"}} {
		err := runSavedQuery(args, strings.NewReader(""), io.Discard)
		if err == nil || !strings.Contains(err.Error(), "invalid --updated") {
			t.Errorf("run %v: error %v", args, err)
		}
	}
	doer := &recordingDoer{}
	err := executeSavedQuery(newRateLimitTracker(doer), queries[1], runFlags{}, strings.NewReader(""), io.Discard)
	if err == nil || len(doer.calls) != 0 {
		t.Errorf("invalid query: %v, requests %v", err, doer.calls)
	}
}

func TestUpdatedHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"save", "--help"}} {
		var out bytes.Buffer
		if err := runWithIO(args, strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"--updated", "positive integer", "days", "prefix required", `">30"`, `"<5"`} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%v help missing %q", args, want)
			}
		}
	}
}

func TestRelativeAge(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		in   time.Time
		want string
	}{
		{"now", now.Add(-30 * time.Second), "now"},
		{"minutes", now.Add(-5 * time.Minute), "5m"},
		{"hours", now.Add(-3 * time.Hour), "3h"},
		{"days", now.Add(-50 * time.Hour), "2d"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := relativeAge(c.in, now); got != c.want {
				t.Errorf("relativeAge(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestRenderNotificationsEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := renderNotifications(&buf, nil, false, 80, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No notifications") {
		t.Errorf("expected empty message, got %q", buf.String())
	}
}

func TestRenderNotifications(t *testing.T) {
	notifications := []Notification{
		{
			Reason:     "mention",
			UpdatedAt:  time.Now().Add(-2 * time.Hour),
			Subject:    NotificationSubject{Title: "Fix the bug", Type: "PullRequest"},
			Repository: NotificationRepo{FullName: "octo/repo"},
		},
	}

	t.Run("default hides reason, shows type", func(t *testing.T) {
		var buf bytes.Buffer
		if err := renderNotifications(&buf, notifications, false, 80, false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out := buf.String()
		for _, want := range []string{"octo/repo", "PR", "Fix the bug"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q; got %q", want, out)
			}
		}
		if strings.Contains(out, "mention") {
			t.Errorf("expected reason hidden; got %q", out)
		}
	})

	t.Run("show-reason includes reason", func(t *testing.T) {
		var buf bytes.Buffer
		if err := renderNotifications(&buf, notifications, false, 80, true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out := buf.String()
		for _, want := range []string{"octo/repo", "PR", "mention", "Fix the bug"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q; got %q", want, out)
			}
		}
	})
}

func TestDisplayType(t *testing.T) {
	if got := displayType("PullRequest"); got != "PR" {
		t.Errorf("displayType(PullRequest) = %q, want PR", got)
	}
	if got := displayType("Issue"); got != "Issue" {
		t.Errorf("displayType(Issue) = %q, want Issue", got)
	}
}

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantRepo string
		wantErr  bool
	}{
		{"no args", nil, "", false},
		{"long flag", []string{"--repo", "octo/repo"}, "octo/repo", false},
		{"short flag", []string{"-R", "octo/repo"}, "octo/repo", false},
		{"equals form", []string{"--repo=octo/repo"}, "octo/repo", false},
		{"missing slash", []string{"--repo", "octorepo"}, "", true},
		{"too many slashes", []string{"--repo", "a/b/c"}, "", true},
		{"unknown flag", []string{"--nope"}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			opts, err := parseArgs(c.args)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if opts.repo != c.wantRepo {
				t.Errorf("repo = %q, want %q", opts.repo, c.wantRepo)
			}
		})
	}
}

func TestNotificationsEndpoint(t *testing.T) {
	if got := notificationsEndpoint(options{}); got != "notifications?per_page=50" {
		t.Errorf("endpoint = %q, want %q", got, "notifications?per_page=50")
	}
	if got := notificationsEndpoint(options{repo: "octo/repo"}); got != "repos/octo/repo/notifications?per_page=50" {
		t.Errorf("endpoint = %q, want %q", got, "repos/octo/repo/notifications?per_page=50")
	}
	if got := notificationsEndpoint(options{all: true}); got != "notifications?per_page=50&all=true" {
		t.Errorf("endpoint = %q, want %q", got, "notifications?per_page=50&all=true")
	}
	if got := notificationsEndpoint(options{repo: "octo/repo", all: true}); got != "repos/octo/repo/notifications?per_page=50&all=true" {
		t.Errorf("endpoint = %q, want %q", got, "repos/octo/repo/notifications?per_page=50&all=true")
	}
}

func TestParseArgsNotificationScope(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		all  bool
	}{
		{"default", nil, false},
		{"short all", []string{"-a"}, true},
		{"all", []string{"--all"}, true},
		{"all false", []string{"--all=false"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseArgs(tc.args)
			if err != nil {
				t.Fatalf("parseArgs(%v): %v", tc.args, err)
			}
			if opts.all != tc.all {
				t.Errorf("all = %v, want %v", opts.all, tc.all)
			}
		})
	}

	for _, arg := range []string{"-a=invalid", "--all=invalid"} {
		if _, err := parseArgs([]string{arg}); err == nil {
			t.Errorf("%s: expected invalid boolean error", arg)
		}
	}
}

func TestParseArgsFilter(t *testing.T) {
	for _, arg := range [][]string{{"-f", "bug"}, {"--filter", "bug"}, {"--filter=bug"}} {
		opts, err := parseArgs(arg)
		if err != nil {
			t.Fatalf("%v: unexpected error: %v", arg, err)
		}
		if opts.filter != "bug" {
			t.Errorf("%v: filter = %q, want %q", arg, opts.filter, "bug")
		}
	}
}

func TestFilterByTitle(t *testing.T) {
	notifications := []Notification{
		{Subject: NotificationSubject{Title: "Fix the login bug"}},
		{Subject: NotificationSubject{Title: "Add dark mode"}},
		{Subject: NotificationSubject{Title: "Another BUG report"}},
	}

	t.Run("empty filter returns all", func(t *testing.T) {
		if got := filterByTitle(notifications, ""); len(got) != 3 {
			t.Errorf("len = %d, want 3", len(got))
		}
	})

	t.Run("case-insensitive substring", func(t *testing.T) {
		got := filterByTitle(notifications, "bug")
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		for _, n := range got {
			if !strings.Contains(strings.ToLower(n.Subject.Title), "bug") {
				t.Errorf("unexpected match %q", n.Subject.Title)
			}
		}
	})

	t.Run("no matches", func(t *testing.T) {
		if got := filterByTitle(notifications, "nonexistent"); len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})
}

func TestParseArgsType(t *testing.T) {
	for _, arg := range [][]string{{"-t", "pr"}, {"--type", "pr"}, {"--type=pr"}} {
		opts, err := parseArgs(arg)
		if err != nil {
			t.Fatalf("%v: unexpected error: %v", arg, err)
		}
		if opts.itemType != "pr" {
			t.Errorf("%v: itemType = %q, want %q", arg, opts.itemType, "pr")
		}
	}
}

func TestCanonicalType(t *testing.T) {
	cases := map[string]string{
		"pr":           "PullRequest",
		"PR":           "PullRequest",
		"pull":         "PullRequest",
		"pull-request": "PullRequest",
		"issue":        "Issue",
		"Issues":       "Issue",
		"commit":       "Commit",
		"release":      "Release",
		"discussion":   "Discussion",
		"CheckSuite":   "CheckSuite",
	}
	for in, want := range cases {
		if got := canonicalType(in); got != want {
			t.Errorf("canonicalType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFilterByType(t *testing.T) {
	notifications := []Notification{
		{Subject: NotificationSubject{Title: "A", Type: "Issue"}},
		{Subject: NotificationSubject{Title: "B", Type: "PullRequest"}},
		{Subject: NotificationSubject{Title: "C", Type: "PullRequest"}},
		{Subject: NotificationSubject{Title: "D", Type: "Release"}},
	}

	t.Run("empty returns all", func(t *testing.T) {
		if got := filterByType(notifications, ""); len(got) != 4 {
			t.Errorf("len = %d, want 4", len(got))
		}
	})

	t.Run("pr alias", func(t *testing.T) {
		got := filterByType(notifications, "pr")
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		for _, n := range got {
			if n.Subject.Type != "PullRequest" {
				t.Errorf("unexpected type %q", n.Subject.Type)
			}
		}
	})

	t.Run("issue", func(t *testing.T) {
		if got := filterByType(notifications, "issue"); len(got) != 1 {
			t.Errorf("len = %d, want 1", len(got))
		}
	})

	t.Run("no matches", func(t *testing.T) {
		if got := filterByType(notifications, "commit"); len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})
}

func TestParseArgsState(t *testing.T) {
	for _, s := range []string{"open", "closed", "merged", "OPEN", "not-planned", "not_planned", "unplanned", "completed"} {
		if _, err := parseArgs([]string{"--state", s}); err != nil {
			t.Errorf("state %q: unexpected error: %v", s, err)
		}
	}
	if _, err := parseArgs([]string{"--state", "bogus"}); err == nil {
		t.Error("expected error for invalid state")
	}
}

func TestParseArgsAuthor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		author  string
		wantErr bool
	}{
		{"include", []string{"--author", "octocat"}, "octocat", false},
		{"include equals", []string{"--author=dependabot[bot]"}, "dependabot[bot]", false},
		{"missing author", []string{"--author"}, "", true},
		{"blank author", []string{"--author", " "}, "", true},
		{"bot suffix only", []string{"--author", "[bot]"}, "", true},
		{"negation unsupported", []string{"--author", "!dependabot"}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseArgs: %v", err)
			}
			if opts.author != tc.author {
				t.Errorf("author = %q, want %q", opts.author, tc.author)
			}
		})
	}
}

func TestNormalizeAuthor(t *testing.T) {
	for login, want := range map[string]string{
		"octocat":           "octocat",
		" Octocat ":         "octocat",
		"dependabot":        "dependabot",
		"Dependabot[bot]":   "dependabot",
		"dependabot-helper": "dependabot-helper",
		"":                  "",
	} {
		if got := normalizeAuthor(login); got != want {
			t.Errorf("normalizeAuthor(%q) = %q, want %q", login, got, want)
		}
	}
}

func TestMatchesState(t *testing.T) {
	cases := []struct {
		detail itemDetail
		want   string
		match  bool
	}{
		{itemDetail{state: "open"}, "open", true},
		{itemDetail{state: "closed"}, "open", false},
		{itemDetail{state: "closed"}, "closed", true},
		{itemDetail{state: "merged"}, "closed", false},
		{itemDetail{state: "merged"}, "merged", true},
		{itemDetail{state: "open"}, "merged", false},
		{itemDetail{state: "closed", stateReason: "not_planned"}, "not-planned", true},
		{itemDetail{state: "closed", stateReason: "not_planned"}, "not_planned", true},
		{itemDetail{state: "closed", stateReason: "not_planned"}, "unplanned", true},
		{itemDetail{state: "closed", stateReason: "completed"}, "not-planned", false},
		{itemDetail{state: "open", stateReason: ""}, "not-planned", false},
		{itemDetail{state: "closed", stateReason: "not_planned"}, "closed", true},
		{itemDetail{state: "closed", stateReason: "completed"}, "completed", true},
		{itemDetail{state: "closed", stateReason: "not_planned"}, "completed", false},
	}
	for _, c := range cases {
		if got := matchesState(c.detail, c.want); got != c.match {
			t.Errorf("matchesState(%+v, %q) = %v, want %v", c.detail, c.want, got, c.match)
		}
	}
}

func TestParseSubjectRef(t *testing.T) {
	t.Run("issue url", func(t *testing.T) {
		ref, ok := parseSubjectRef("https://api.github.com/repos/octo/repo/issues/123")
		if !ok || ref.owner != "octo" || ref.repo != "repo" || ref.number != 123 {
			t.Errorf("got %+v ok=%v", ref, ok)
		}
	})
	t.Run("pull url", func(t *testing.T) {
		ref, ok := parseSubjectRef("https://api.github.com/repos/octo/repo/pulls/45")
		if !ok || ref.number != 45 {
			t.Errorf("got %+v ok=%v", ref, ok)
		}
	})
	t.Run("unparseable url", func(t *testing.T) {
		if _, ok := parseSubjectRef("https://api.github.com/repos/octo/repo/releases/3"); ok {
			t.Error("expected ok=false for release url")
		}
		if _, ok := parseSubjectRef(""); ok {
			t.Error("expected ok=false for empty url")
		}
	})
}

// fakeGQL is a test stand-in for the GraphQL client's Do method. It reports the
// same details for every aliased item unless items supplies details by item
// number. An empty state produces a null item.
type fakeGQL struct {
	state       string // GraphQL enum, e.g. "OPEN", "CLOSED", "MERGED"
	stateReason string // GraphQL enum, e.g. "NOT_PLANNED", "COMPLETED"
	draft       bool
	author      string
	items       map[int]itemDetail
	raw         string
	err         error
	failAt      int
	calls       int
	query       string
	numbers     []int
	rl          *rateLimit
}

func (f *fakeGQL) Do(query string, variables map[string]interface{}, response interface{}) error {
	f.calls++
	f.query = query
	if f.err != nil && (f.failAt == 0 || f.calls == f.failAt) {
		return f.err
	}
	if f.raw != "" {
		return json.Unmarshal([]byte(f.raw), response)
	}
	n := 0
	for k := range variables {
		if strings.HasPrefix(k, "o") {
			n++
		}
	}
	data := map[string]interface{}{}
	for j := 0; j < n; j++ {
		number, ok := variables[fmt.Sprintf("n%d", j)].(int)
		if !ok {
			return fmt.Errorf("missing item number for alias i%d", j)
		}
		f.numbers = append(f.numbers, number)
		detail := itemDetail{state: f.state, stateReason: f.stateReason, isDraft: f.draft, author: f.author}
		if f.items != nil {
			detail = f.items[number]
		}
		var iopr interface{}
		if detail.state != "" {
			var author interface{}
			if detail.author != "" {
				author = map[string]interface{}{"login": detail.author}
			}
			iopr = map[string]interface{}{
				"state": strings.ToUpper(detail.state), "stateReason": strings.ToUpper(detail.stateReason),
				"isDraft": detail.isDraft, "author": author,
			}
		}
		data[fmt.Sprintf("i%d", j)] = map[string]interface{}{"issueOrPullRequest": iopr}
	}
	if f.rl != nil {
		data["rateLimit"] = map[string]interface{}{
			"limit":     f.rl.limit,
			"remaining": f.rl.remaining,
			"resetAt":   f.rl.reset.Format(time.RFC3339),
		}
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, response)
}

func withFakeGraphQLClient(t *testing.T, doer graphQLDoer, err error) {
	t.Helper()
	prev := newGraphQLClient
	newGraphQLClient = func() (graphQLDoer, error) { return doer, err }
	t.Cleanup(func() { newGraphQLClient = prev })
}

func TestFetchItemDetails(t *testing.T) {
	notifications := []Notification{
		{Subject: NotificationSubject{Type: "Issue", URL: "https://api.github.com/repos/o/r/issues/1"}},
		{Subject: NotificationSubject{Type: "PullRequest", URL: "https://api.github.com/repos/o/r/pulls/2"}},
		{Subject: NotificationSubject{Type: "Release", URL: "https://api.github.com/repos/o/r/releases/3"}},
		{Subject: NotificationSubject{Type: "Issue", URL: "not-a-url"}},
	}

	fake := &fakeGQL{state: "CLOSED", stateReason: "NOT_PLANNED", draft: true, author: "dependabot"}
	details, _, err := fetchItemDetails(fake, notifications)
	if err != nil {
		t.Fatalf("fetchItemDetails: %v", err)
	}
	if len(details) != 2 {
		t.Fatalf("len(details) = %d, want 2", len(details))
	}
	if details[0].state != "closed" || details[1].state != "closed" {
		t.Errorf("details = %v, want indices 0 and 1 closed", details)
	}
	if details[0].stateReason != "not_planned" {
		t.Errorf("expected stateReason not_planned, got %q", details[0].stateReason)
	}
	if !details[1].isDraft {
		t.Errorf("expected isDraft true for index 1")
	}
	if details[0].author != "dependabot" || details[1].author != "dependabot" {
		t.Errorf("author missing from issue/PR details: %+v", details)
	}
	for _, fragment := range []string{"... on Issue{state stateReason author{login}}", "... on PullRequest{state isDraft author{login}}"} {
		if !strings.Contains(fake.query, fragment) {
			t.Errorf("query missing %q: %s", fragment, fake.query)
		}
	}
	if _, ok := details[2]; ok {
		t.Error("release should not have details")
	}
	if _, ok := details[3]; ok {
		t.Error("unparseable url should not have details")
	}
}

func TestFetchItemDetailsQuota(t *testing.T) {
	notifications := []Notification{
		{Subject: NotificationSubject{Type: "Issue", URL: "https://api.github.com/repos/o/r/issues/1"}},
	}
	fake := &fakeGQL{state: "OPEN", rl: &rateLimit{remaining: 4990, limit: 5000, reset: time.Unix(1700000000, 0)}}
	_, quota, err := fetchItemDetails(fake, notifications)
	if err != nil {
		t.Fatalf("fetchItemDetails: %v", err)
	}
	if quota == nil {
		t.Fatal("expected a GraphQL quota")
	}
	if quota.remaining != 4990 || quota.limit != 5000 {
		t.Errorf("quota = %+v", quota)
	}
}

func TestFetchItemDetailsBatches(t *testing.T) {
	notifications := make([]Notification, stateBatchSize+5)
	for i := range notifications {
		notifications[i] = Notification{Subject: NotificationSubject{
			Type: "Issue",
			URL:  fmt.Sprintf("https://api.github.com/repos/o/r/issues/%d", i+1),
		}}
	}
	fake := &fakeGQL{state: "CLOSED", author: "dependabot"}
	details, _, err := fetchItemDetails(fake, notifications)
	if err != nil {
		t.Fatalf("fetchItemDetails: %v", err)
	}
	if len(details) != len(notifications) {
		t.Errorf("len(details) = %d, want %d", len(details), len(notifications))
	}
	if fake.calls != 2 {
		t.Errorf("expected 2 batched calls, got %d", fake.calls)
	}
	for i := range notifications {
		if details[i].author != "dependabot" {
			t.Errorf("missing author at index %d: %+v", i, details[i])
		}
	}
}

func TestFetchItemDetailsErrors(t *testing.T) {
	notifications := []Notification{
		{Subject: NotificationSubject{Type: "PullRequest", URL: "https://api.github.com/repos/o/r/pulls/1"}},
	}
	for _, tc := range []struct {
		name string
		fake fakeGQL
		want string
	}{
		{"API failure", fakeGQL{err: errors.New("boom")}, "fetching item details: boom"},
		{"missing alias", fakeGQL{raw: `{}`}, "missing response"},
		{"invalid item", fakeGQL{raw: `{"i0":"unexpected"}`}, "decoding item details for o/r#1"},
		{"missing item", fakeGQL{raw: `{"i0":{}}`}, "missing item response"},
		{"missing author", fakeGQL{raw: `{"i0":{"issueOrPullRequest":{"state":"OPEN","isDraft":false}}}`}, "missing author response"},
		{"invalid author", fakeGQL{raw: `{"i0":{"issueOrPullRequest":{"state":"OPEN","isDraft":false,"author":"dependabot"}}}`}, "decoding author for o/r#1"},
		{"missing login", fakeGQL{raw: `{"i0":{"issueOrPullRequest":{"state":"OPEN","isDraft":false,"author":{}}}}`}, "missing author login"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			details, _, err := fetchItemDetails(&tc.fake, notifications)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if details != nil {
				t.Errorf("failed lookup returned partial details: %+v", details)
			}
		})
	}
}

func TestFetchItemDetailsRequiredFields(t *testing.T) {
	for _, tc := range []struct {
		name        string
		subjectType string
		item        string
		wantErr     string
		wantDraft   bool
	}{
		{"issue missing state", "Issue", `{"author":{"login":"alice"}}`, "missing state", false},
		{"issue null state", "Issue", `{"state":null,"author":{"login":"alice"}}`, "missing state", false},
		{"PR missing state", "PullRequest", `{"isDraft":false,"author":{"login":"alice"}}`, "missing state", false},
		{"PR null state", "PullRequest", `{"state":null,"isDraft":false,"author":{"login":"alice"}}`, "missing state", false},
		{"PR missing draft", "PullRequest", `{"state":"OPEN","author":{"login":"alice"}}`, "missing isDraft", false},
		{"PR null draft", "PullRequest", `{"state":"OPEN","isDraft":null,"author":{"login":"alice"}}`, "missing isDraft", false},
		{"ready PR", "PullRequest", `{"state":"OPEN","isDraft":false,"author":{"login":"alice"}}`, "", false},
		{"draft PR", "PullRequest", `{"state":"OPEN","isDraft":true,"author":{"login":"alice"}}`, "", true},
		{"issue without draft", "Issue", `{"state":"OPEN","author":{"login":"alice"}}`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notifications := []Notification{
				{Subject: NotificationSubject{Type: tc.subjectType, URL: "https://api.github.com/repos/o/r/issues/1"}},
			}
			fake := &fakeGQL{raw: fmt.Sprintf(`{"i0":{"issueOrPullRequest":%s}}`, tc.item)}
			details, _, err := fetchItemDetails(fake, notifications)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || details != nil {
					t.Fatalf("details = %v, error = %v, want %q", details, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("fetchItemDetails: %v", err)
			}
			detail, ok := details[0]
			if !ok || detail.state != "open" || detail.author != "alice" || detail.isDraft != tc.wantDraft {
				t.Errorf("detail = %+v, present = %v, want draft = %v", detail, ok, tc.wantDraft)
			}
		})
	}
}

func TestFetchItemDetailsLaterBatchError(t *testing.T) {
	notifications := make([]Notification, stateBatchSize+1)
	for i := range notifications {
		notifications[i].Subject = NotificationSubject{
			Type: "PullRequest", URL: fmt.Sprintf("https://api.github.com/repos/o/r/pulls/%d", i+1),
		}
	}
	boom := errors.New("boom")
	fake := &fakeGQL{state: "OPEN", author: "dependabot", err: boom, failAt: 2, rl: &rateLimit{remaining: 4990, limit: 5000}}
	details, quota, err := fetchItemDetails(fake, notifications)
	if !errors.Is(err, boom) || details != nil || fake.calls != 2 {
		t.Fatalf("details = %v, calls = %d, error = %v", details, fake.calls, err)
	}
	if quota == nil || quota.remaining != 4990 {
		t.Errorf("last successful quota was lost: %+v", quota)
	}
}

func TestFilterByState(t *testing.T) {
	notifications := []Notification{
		{Subject: NotificationSubject{Title: "issue", Type: "Issue"}},
		{Subject: NotificationSubject{Title: "pr", Type: "PullRequest"}},
		{Subject: NotificationSubject{Title: "release", Type: "Release"}},
	}
	details := map[int]itemDetail{
		0: {state: "open"},
		1: {state: "open"},
	}

	t.Run("empty returns all", func(t *testing.T) {
		if got := filterByItemDetails(notifications, details, options{}); len(got) != 3 {
			t.Errorf("len = %d, want 3", len(got))
		}
	})

	t.Run("open keeps issue and pr, drops release", func(t *testing.T) {
		got := filterByItemDetails(notifications, details, options{state: "open"})
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		for _, n := range got {
			if n.Subject.Type == "Release" {
				t.Error("release should be excluded by state filter")
			}
		}
	})

	t.Run("closed excludes open items", func(t *testing.T) {
		if got := filterByItemDetails(notifications, details, options{state: "closed"}); len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})

	t.Run("not-planned keeps only matching closed issues", func(t *testing.T) {
		items := []Notification{
			{Subject: NotificationSubject{Title: "unplanned", Type: "Issue"}},
			{Subject: NotificationSubject{Title: "completed", Type: "Issue"}},
			{Subject: NotificationSubject{Title: "open", Type: "Issue"}},
		}
		d := map[int]itemDetail{
			0: {state: "closed", stateReason: "not_planned"},
			1: {state: "closed", stateReason: "completed"},
			2: {state: "open"},
		}
		got := filterByItemDetails(items, d, options{state: "not-planned"})
		if len(got) != 1 || got[0].Subject.Title != "unplanned" {
			t.Errorf("got %v, want only the not-planned issue", got)
		}
	})
}

func TestFilterByDraft(t *testing.T) {
	notifications := []Notification{
		{Subject: NotificationSubject{Title: "draft pr", Type: "PullRequest"}},
		{Subject: NotificationSubject{Title: "ready pr", Type: "PullRequest"}},
		{Subject: NotificationSubject{Title: "issue", Type: "Issue"}},
	}
	details := map[int]itemDetail{
		0: {state: "open", isDraft: true},
		1: {state: "open", isDraft: false},
		2: {state: "open", isDraft: false},
	}

	t.Run("false returns all", func(t *testing.T) {
		if got := filterByItemDetails(notifications, details, options{}); len(got) != 3 {
			t.Errorf("len = %d, want 3", len(got))
		}
	})

	t.Run("keeps only draft PRs", func(t *testing.T) {
		got := filterByItemDetails(notifications, details, options{draft: true})
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		if got[0].Subject.Title != "draft pr" {
			t.Errorf("kept %q", got[0].Subject.Title)
		}
	})
}

func TestFilterByItemDetails(t *testing.T) {
	notifications := []Notification{
		{ID: "closed", Subject: NotificationSubject{Type: "PullRequest"}},
		{ID: "bot", Subject: NotificationSubject{Type: "PullRequest"}},
		{ID: "ready", Subject: NotificationSubject{Type: "PullRequest"}},
		{ID: "draft", Subject: NotificationSubject{Type: "PullRequest"}},
		{ID: "issue", Subject: NotificationSubject{Type: "Issue"}},
		{ID: "release", Subject: NotificationSubject{Type: "Release"}},
		{ID: "deleted", Subject: NotificationSubject{Type: "PullRequest"}},
		{ID: "unavailable", Subject: NotificationSubject{Type: "PullRequest"}},
		{ID: "prefix", Subject: NotificationSubject{Type: "PullRequest"}},
	}
	details := map[int]itemDetail{
		0: {state: "closed", isDraft: true, author: "alice"},
		1: {state: "open", isDraft: true, author: "dependabot"},
		2: {state: "open", author: "Alice"},
		3: {state: "open", isDraft: true, author: "alice"},
		4: {state: "open", author: "alice"},
		6: {state: "open", isDraft: true},
		8: {state: "open", author: "dependabot-helper"},
	}
	for _, tc := range []struct {
		name string
		opts options
		want string
	}{
		{"no filters", options{}, "closed,bot,ready,draft,issue,release,deleted,unavailable,prefix"},
		{"creator", options{author: "ALICE"}, "closed,ready,draft,issue"},
		{"bot", options{author: "dependabot"}, "bot"},
		{"bot suffix", options{author: "Dependabot[bot]"}, "bot"},
		{"exact login", options{author: "dependabot-helper"}, "prefix"},
		{"state and draft", options{state: "open", draft: true}, "bot,draft,deleted"},
		{"state draft and author", options{state: "open", draft: true, author: "alice"}, "draft"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ids []string
			for _, n := range filterByItemDetails(notifications, details, tc.opts) {
				ids = append(ids, n.ID)
			}
			if got := strings.Join(ids, ","); got != tc.want {
				t.Errorf("IDs = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFetchItemDetailsUnavailableAuthor(t *testing.T) {
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Type: "PullRequest", URL: "https://api.github.com/repos/o/r/pulls/1"}},
	}
	for _, raw := range []string{
		`{"i0":{"issueOrPullRequest":{"state":"OPEN","isDraft":false,"author":null}}}`,
		`{"i0":{"issueOrPullRequest":null}}`,
		`{"i0":null}`,
	} {
		fake := &fakeGQL{raw: raw}
		details, _, err := fetchItemDetails(fake, notifications)
		if err != nil {
			t.Fatalf("fetchItemDetails(%s): %v", raw, err)
		}
		if got := filterByItemDetails(notifications, details, options{author: "dependabot"}); len(got) != 0 {
			t.Errorf("unknown author matched inclusion: %+v", got)
		}
		if got := filterByItemDetails(notifications, details, options{}); len(got) != 1 {
			t.Errorf("unfiltered notification was dropped: %+v", got)
		}
	}
}

func TestLoadFilteredNotifications(t *testing.T) {
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "Other change", Type: "PullRequest", URL: "https://api.github.com/repos/o/r/pulls/1"}},
		{ID: "2", Subject: NotificationSubject{Title: "Fix release", Type: "Release"}},
		{ID: "3", Subject: NotificationSubject{Title: "Fix closed", Type: "PullRequest", URL: "https://api.github.com/repos/o/r/pulls/3"}},
		{ID: "4", Subject: NotificationSubject{Title: "Fix bot", Type: "PullRequest", URL: "https://api.github.com/repos/o/r/pulls/4"}},
		{ID: "5", Subject: NotificationSubject{Title: "Fix ready", Type: "PullRequest", URL: "https://api.github.com/repos/o/r/pulls/5"}},
		{ID: "6", Subject: NotificationSubject{Title: "Fix draft", Type: "PullRequest", URL: "https://api.github.com/repos/o/r/pulls/6"}},
	}
	body, err := json.Marshal(notifications)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		opts    options
		want    string
		numbers []int
	}{
		{"no item filters", options{}, "1,2,3,4,5,6", nil},
		{"author only", options{author: "ALICE"}, "3,5,6", []int{1, 3, 4, 5, 6}},
		{"all filters", options{filter: "Fix", itemType: "pr", state: "open", draft: true, author: "alice"}, "6", []int{3, 4, 5, 6}},
		{"no matches", options{filter: "absent", author: "alice"}, "", nil},
		{"unsupported type excluded", options{itemType: "release", author: "alice"}, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeGQL{
				items: map[int]itemDetail{
					1: {state: "open", author: "bob"},
					3: {state: "closed", isDraft: true, author: "alice"},
					4: {state: "open", isDraft: true, author: "dependabot"},
					5: {state: "open", author: "alice"},
					6: {state: "open", isDraft: true, author: "alice"},
				},
				rl: &rateLimit{remaining: 4999, limit: 5000},
			}
			withFakeGraphQLClient(t, fake, nil)
			doer := &recordingDoer{body: string(body)}
			tracker := newRateLimitTracker(doer)
			got, err := loadFilteredNotifications(tracker, tc.opts)
			if err != nil {
				t.Fatalf("loadFilteredNotifications: %v", err)
			}
			var ids []string
			for _, n := range got {
				ids = append(ids, n.ID)
			}
			if strings.Join(ids, ",") != tc.want {
				t.Errorf("IDs = %v, want %q", ids, tc.want)
			}
			if !slices.Equal(fake.numbers, tc.numbers) {
				t.Errorf("GraphQL fetched %v, want %v", fake.numbers, tc.numbers)
			}
			wantCalls := 0
			if len(tc.numbers) > 0 {
				wantCalls = 1
				if tracker.gql == nil || tracker.gql.remaining != 4999 {
					t.Errorf("GraphQL quota not recorded: %+v", tracker.gql)
				}
			}
			if fake.calls != wantCalls || !slices.Equal(doer.calls, []string{"GET notifications?per_page=50"}) {
				t.Errorf("GraphQL calls = %d, REST calls = %v", fake.calls, doer.calls)
			}
		})
	}
}

func TestLoadFilteredNotificationsPreservesGraphQLQuota(t *testing.T) {
	boom := errors.New("boom")
	for _, tc := range []struct {
		name      string
		err       error
		quota     *rateLimit
		itemCount int
		failAt    int
		want      int
	}{
		{"first batch fails", boom, nil, 1, 0, 4999},
		{"no new quota", nil, nil, 1, 0, 4999},
		{"later batch fails", boom, &rateLimit{remaining: 4990, limit: 5000}, stateBatchSize + 1, 2, 4990},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeGQL{state: "OPEN", author: "alice", rl: &rateLimit{remaining: 4999, limit: 5000}}
			withFakeGraphQLClient(t, fake, nil)
			doer := &recordingDoer{body: `[{"subject":{"type":"PullRequest","url":"https://api.github.com/repos/o/r/pulls/1"}}]`}
			tracker := newRateLimitTracker(doer)
			opts := options{author: "alice"}
			if _, err := loadFilteredNotifications(tracker, opts); err != nil {
				t.Fatalf("initial lookup: %v", err)
			}
			if tracker.gql == nil || tracker.gql.remaining != 4999 {
				t.Fatalf("initial quota not recorded: %+v", tracker.gql)
			}

			notifications := make([]Notification, tc.itemCount)
			for i := range notifications {
				notifications[i].Subject = NotificationSubject{
					Type: "PullRequest", URL: fmt.Sprintf("https://api.github.com/repos/o/r/pulls/%d", i+1),
				}
			}
			body, err := json.Marshal(notifications)
			if err != nil {
				t.Fatal(err)
			}
			doer.body = string(body)
			fake.calls, fake.failAt, fake.err, fake.rl = 0, tc.failAt, tc.err, tc.quota
			got, err := loadFilteredNotifications(tracker, opts)
			if !errors.Is(err, tc.err) || (err != nil && got != nil) {
				t.Fatalf("notifications = %v, error = %v, want error %v", got, err, tc.err)
			}
			if tracker.gql == nil || tracker.gql.remaining != tc.want {
				t.Fatalf("quota = %+v, want remaining %d", tracker.gql, tc.want)
			}
			var out bytes.Buffer
			tracker.report(&out)
			want := fmt.Sprintf("GitHub GraphQL API quota: %d/5000 remaining", tc.want)
			if !strings.Contains(out.String(), want) {
				t.Errorf("report = %q, want %q", out.String(), want)
			}
		})
	}
}

func TestLoadFilteredNotificationsErrors(t *testing.T) {
	boom := errors.New("boom")
	for _, tc := range []struct {
		name      string
		restErr   error
		clientErr error
		gqlErr    error
	}{
		{"REST", boom, nil, nil},
		{"GraphQL initialization", nil, boom, nil},
		{"GraphQL lookup", nil, nil, boom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withFakeGraphQLClient(t, &fakeGQL{err: tc.gqlErr}, tc.clientErr)
			doer := &recordingDoer{
				body: `[{"subject":{"type":"PullRequest","url":"https://api.github.com/repos/o/r/pulls/1"}}]`,
				err:  tc.restErr,
			}
			got, err := loadFilteredNotifications(newRateLimitTracker(doer), options{author: "dependabot"})
			if !errors.Is(err, boom) || got != nil {
				t.Fatalf("notifications = %v, error = %v", got, err)
			}
		})
	}
}

func TestFindNextPage(t *testing.T) {
	t.Run("with next link", func(t *testing.T) {
		resp := &http.Response{Header: http.Header{}}
		resp.Header.Set("Link", `<https://api.github.com/notifications?page=2>; rel="next", <https://api.github.com/notifications?page=5>; rel="last"`)
		got, ok := findNextPage(resp)
		if !ok {
			t.Fatal("expected a next page")
		}
		if got != "https://api.github.com/notifications?page=2" {
			t.Errorf("next = %q", got)
		}
	})
	t.Run("without next link", func(t *testing.T) {
		resp := &http.Response{Header: http.Header{}}
		resp.Header.Set("Link", `<https://api.github.com/notifications?page=1>; rel="prev"`)
		if _, ok := findNextPage(resp); ok {
			t.Error("expected no next page")
		}
	})
	t.Run("no link header", func(t *testing.T) {
		resp := &http.Response{Header: http.Header{}}
		if _, ok := findNextPage(resp); ok {
			t.Error("expected no next page")
		}
	})
}

func TestParseArgsInteractive(t *testing.T) {
	for _, arg := range []string{"-i", "--interactive"} {
		opts, err := parseArgs([]string{arg})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", arg, err)
		}
		if !opts.interactive {
			t.Errorf("%s: expected interactive to be true", arg)
		}
	}
	opts, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.interactive {
		t.Error("expected interactive to default to false")
	}
}

func TestNotificationItem(t *testing.T) {
	n := Notification{
		Reason:     "mention",
		UpdatedAt:  time.Now().Add(-3 * time.Hour),
		Subject:    NotificationSubject{Title: "Fix the bug"},
		Repository: NotificationRepo{FullName: "octo/repo"},
	}
	item := notificationItem{n: n}

	if item.Title() != "Fix the bug" {
		t.Errorf("Title = %q", item.Title())
	}
	if desc := item.Description(); !strings.Contains(desc, "octo/repo") || !strings.Contains(desc, "mention") || !strings.Contains(desc, "3h") {
		t.Errorf("Description = %q", desc)
	}
	for _, want := range []string{"octo/repo", "mention", "Fix the bug"} {
		if !strings.Contains(item.FilterValue(), want) {
			t.Errorf("FilterValue %q missing %q", item.FilterValue(), want)
		}
	}
}

// fakeDoer is a test stand-in for the REST client's Request method.
type fakeDoer struct {
	body string
	err  error
}

func (f fakeDoer) Request(method, path string, body io.Reader) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		Body:   io.NopCloser(strings.NewReader(f.body)),
		Header: http.Header{},
	}, nil
}

func TestResolveWebURL(t *testing.T) {
	n := Notification{
		Subject:    NotificationSubject{URL: "https://api.github.com/repos/octo/repo/issues/1"},
		Repository: NotificationRepo{FullName: "octo/repo"},
	}

	t.Run("uses subject html_url", func(t *testing.T) {
		doer := fakeDoer{body: `{"html_url":"https://github.com/octo/repo/issues/1"}`}
		if got := resolveWebURL(doer, n); got != "https://github.com/octo/repo/issues/1" {
			t.Errorf("url = %q", got)
		}
	})

	t.Run("falls back to repo on request error", func(t *testing.T) {
		doer := fakeDoer{err: errors.New("boom")}
		if got := resolveWebURL(doer, n); got != "https://github.com/octo/repo" {
			t.Errorf("url = %q", got)
		}
	})

	t.Run("falls back to repo when no subject url", func(t *testing.T) {
		bare := Notification{Repository: NotificationRepo{FullName: "octo/repo"}}
		if got := resolveWebURL(fakeDoer{}, bare); got != "https://github.com/octo/repo" {
			t.Errorf("url = %q", got)
		}
	})

	t.Run("falls back when html_url empty", func(t *testing.T) {
		doer := fakeDoer{body: `{"html_url":""}`}
		if got := resolveWebURL(doer, n); got != "https://github.com/octo/repo" {
			t.Errorf("url = %q", got)
		}
	})
}
