package main

import (
	"bytes"
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
	if err := renderNotifications(&buf, nil, false, 80); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No unread notifications") {
		t.Errorf("expected empty message, got %q", buf.String())
	}
}

func TestRenderNotifications(t *testing.T) {
	var buf bytes.Buffer
	notifications := []Notification{
		{
			Reason:     "mention",
			UpdatedAt:  time.Now().Add(-2 * time.Hour),
			Subject:    NotificationSubject{Title: "Fix the bug", Type: "Issue"},
			Repository: NotificationRepo{FullName: "octo/repo"},
		},
	}
	if err := renderNotifications(&buf, notifications, false, 80); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"octo/repo", "mention", "Fix the bug"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q; got %q", want, out)
		}
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
	if got := notificationsEndpoint(options{}); got != "notifications" {
		t.Errorf("endpoint = %q, want %q", got, "notifications")
	}
	if got := notificationsEndpoint(options{repo: "octo/repo"}); got != "repos/octo/repo/notifications" {
		t.Errorf("endpoint = %q, want %q", got, "repos/octo/repo/notifications")
	}
}
