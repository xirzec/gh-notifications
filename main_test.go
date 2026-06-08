package main

import (
	"bytes"
	"errors"
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
	for _, s := range []string{"open", "closed", "merged", "OPEN"} {
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
		state itemState
		want  string
		match bool
	}{
		{itemState{State: "open"}, "open", true},
		{itemState{State: "closed"}, "open", false},
		{itemState{State: "closed"}, "closed", true},
		{itemState{State: "closed", Merged: true}, "closed", false},
		{itemState{State: "closed", Merged: true}, "merged", true},
		{itemState{State: "open"}, "merged", false},
		{itemState{State: "open"}, "bogus", false},
	}
	for _, c := range cases {
		if got := matchesState(c.state, c.want); got != c.match {
			t.Errorf("matchesState(%+v, %q) = %v, want %v", c.state, c.want, got, c.match)
		}
	}
}

func TestFetchItemState(t *testing.T) {
	n := Notification{Subject: NotificationSubject{URL: "https://api.github.com/repos/o/r/pulls/1"}}

	t.Run("decodes state", func(t *testing.T) {
		s, ok := fetchItemState(fakeDoer{body: `{"state":"closed","merged":true}`}, n)
		if !ok || s.State != "closed" || !s.Merged {
			t.Errorf("got %+v ok=%v", s, ok)
		}
	})

	t.Run("no subject url", func(t *testing.T) {
		if _, ok := fetchItemState(fakeDoer{}, Notification{}); ok {
			t.Error("expected ok=false without subject url")
		}
	})

	t.Run("request error", func(t *testing.T) {
		if _, ok := fetchItemState(fakeDoer{err: errors.New("boom")}, n); ok {
			t.Error("expected ok=false on request error")
		}
	})
}

func TestFilterByState(t *testing.T) {
	notifications := []Notification{
		{Subject: NotificationSubject{Title: "issue", Type: "Issue", URL: "https://api/1"}},
		{Subject: NotificationSubject{Title: "pr", Type: "PullRequest", URL: "https://api/2"}},
		{Subject: NotificationSubject{Title: "release", Type: "Release", URL: "https://api/3"}},
	}

	t.Run("empty returns all", func(t *testing.T) {
		if got := filterByState(fakeDoer{}, notifications, ""); len(got) != 3 {
			t.Errorf("len = %d, want 3", len(got))
		}
	})

	t.Run("open keeps issue and pr, drops release", func(t *testing.T) {
		// fakeDoer returns the same open state for every request.
		got := filterByState(fakeDoer{body: `{"state":"open"}`}, notifications, "open")
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
		if got := filterByState(fakeDoer{body: `{"state":"open"}`}, notifications, "closed"); len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
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
