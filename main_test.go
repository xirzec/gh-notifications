package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

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
	if !strings.Contains(buf.String(), "No unread notifications") {
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

func TestParseArgsAll(t *testing.T) {
	for _, arg := range []string{"-a", "--all"} {
		opts, err := parseArgs([]string{arg})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", arg, err)
		}
		if !opts.all {
			t.Errorf("%s: expected all to be true", arg)
		}
	}
	opts, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.all {
		t.Error("expected all to default to false")
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
// same state (and draft flag) for every aliased item in the batch (or a null
// item when state is empty), and returns err when set. When rl is set, it
// includes a rateLimit object in the response.
type fakeGQL struct {
	state       string // GraphQL enum, e.g. "OPEN", "CLOSED", "MERGED"
	stateReason string // GraphQL enum, e.g. "NOT_PLANNED", "COMPLETED"
	draft       bool
	err         error
	calls       int
	rl          *rateLimit
}

func (f *fakeGQL) Do(query string, variables map[string]interface{}, response interface{}) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	n := 0
	for k := range variables {
		if strings.HasPrefix(k, "o") {
			n++
		}
	}
	data := map[string]interface{}{}
	for j := 0; j < n; j++ {
		var iopr interface{}
		if f.state != "" {
			iopr = map[string]interface{}{"state": f.state, "stateReason": f.stateReason, "isDraft": f.draft}
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
	b, _ := json.Marshal(data)
	return json.Unmarshal(b, response)
}

func TestFetchItemDetails(t *testing.T) {
	notifications := []Notification{
		{Subject: NotificationSubject{Type: "Issue", URL: "https://api.github.com/repos/o/r/issues/1"}},
		{Subject: NotificationSubject{Type: "PullRequest", URL: "https://api.github.com/repos/o/r/pulls/2"}},
		{Subject: NotificationSubject{Type: "Release", URL: "https://api.github.com/repos/o/r/releases/3"}},
		{Subject: NotificationSubject{Type: "Issue", URL: "not-a-url"}},
	}

	details, _ := fetchItemDetails(&fakeGQL{state: "CLOSED", stateReason: "NOT_PLANNED", draft: true}, notifications)
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
	_, quota := fetchItemDetails(fake, notifications)
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
	fake := &fakeGQL{state: "CLOSED"}
	details, _ := fetchItemDetails(fake, notifications)
	if len(details) != len(notifications) {
		t.Errorf("len(details) = %d, want %d", len(details), len(notifications))
	}
	if fake.calls != 2 {
		t.Errorf("expected 2 batched calls, got %d", fake.calls)
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
		if got := filterByState(notifications, details, ""); len(got) != 3 {
			t.Errorf("len = %d, want 3", len(got))
		}
	})

	t.Run("open keeps issue and pr, drops release", func(t *testing.T) {
		got := filterByState(notifications, details, "open")
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
		if got := filterByState(notifications, details, "closed"); len(got) != 0 {
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
		got := filterByState(items, d, "not-planned")
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
		if got := filterByDraft(notifications, details, false); len(got) != 3 {
			t.Errorf("len = %d, want 3", len(got))
		}
	})

	t.Run("keeps only draft PRs", func(t *testing.T) {
		got := filterByDraft(notifications, details, true)
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		if got[0].Subject.Title != "draft pr" {
			t.Errorf("kept %q", got[0].Subject.Title)
		}
	})
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
