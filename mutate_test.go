package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// recordingDoer captures the REST requests issued against it.
type recordingDoer struct {
	calls []string
	err   error
}

func (d *recordingDoer) Request(method, path string, body io.Reader) (*http.Response, error) {
	d.calls = append(d.calls, method+" "+path)
	if d.err != nil {
		return nil, d.err
	}
	return &http.Response{
		Body:   io.NopCloser(strings.NewReader("")),
		Header: http.Header{},
	}, nil
}

func TestConfirm(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
		{"", false},
		{"maybe\n", false},
	}
	for _, c := range cases {
		var out bytes.Buffer
		got, err := confirm(strings.NewReader(c.input), &out, "Proceed?")
		if err != nil {
			t.Fatalf("input %q: unexpected error: %v", c.input, err)
		}
		if got != c.want {
			t.Errorf("confirm(%q) = %v, want %v", c.input, got, c.want)
		}
		if !strings.Contains(out.String(), "Proceed?") {
			t.Errorf("prompt not written for input %q", c.input)
		}
	}
}

func TestMarkThreadRead(t *testing.T) {
	doer := &recordingDoer{}
	if err := markThreadRead(doer, "123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(doer.calls) != 1 || doer.calls[0] != "PATCH notifications/threads/123" {
		t.Errorf("calls = %v", doer.calls)
	}
}

func TestMarkThreadDone(t *testing.T) {
	doer := &recordingDoer{}
	if err := markThreadDone(doer, "123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(doer.calls) != 1 || doer.calls[0] != "DELETE notifications/threads/123" {
		t.Errorf("calls = %v", doer.calls)
	}
}

func TestMarkThreadReadError(t *testing.T) {
	doer := &recordingDoer{err: errors.New("boom")}
	if err := markThreadRead(doer, "123"); err == nil {
		t.Error("expected error to propagate")
	}
}

func TestRunMarkReadEmpty(t *testing.T) {
	doer := &recordingDoer{}
	var out bytes.Buffer
	if err := runMarkRead(doer, nil, false, strings.NewReader(""), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(doer.calls) != 0 {
		t.Errorf("expected no API calls, got %v", doer.calls)
	}
	if !strings.Contains(out.String(), "No notifications") {
		t.Errorf("unexpected output %q", out.String())
	}
}

func TestRunMarkReadDryRun(t *testing.T) {
	doer := &recordingDoer{}
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "o/r"}},
		{ID: "2", Subject: NotificationSubject{Title: "B"}, Repository: NotificationRepo{FullName: "o/r"}},
	}
	var out bytes.Buffer
	// Provide "y" on stdin to prove dry-run never reads/acts on it.
	if err := runMarkRead(doer, notifications, true, strings.NewReader("y\n"), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(doer.calls) != 0 {
		t.Errorf("dry run made API calls: %v", doer.calls)
	}
	if !strings.Contains(out.String(), "Dry run") {
		t.Errorf("expected dry-run notice, got %q", out.String())
	}
}

func TestRunMarkReadConfirmed(t *testing.T) {
	doer := &recordingDoer{}
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "o/r"}},
		{ID: "2", Subject: NotificationSubject{Title: "B"}, Repository: NotificationRepo{FullName: "o/r"}},
	}
	var out bytes.Buffer
	if err := runMarkRead(doer, notifications, false, strings.NewReader("y\n"), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"PATCH notifications/threads/1", "PATCH notifications/threads/2"}
	if len(doer.calls) != 2 || doer.calls[0] != want[0] || doer.calls[1] != want[1] {
		t.Errorf("calls = %v, want %v", doer.calls, want)
	}
	if !strings.Contains(out.String(), "Marked 2 notification(s) as read") {
		t.Errorf("unexpected output %q", out.String())
	}
}

func TestRunMarkReadAborted(t *testing.T) {
	doer := &recordingDoer{}
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "o/r"}},
	}
	var out bytes.Buffer
	if err := runMarkRead(doer, notifications, false, strings.NewReader("n\n"), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(doer.calls) != 0 {
		t.Errorf("expected no API calls after abort, got %v", doer.calls)
	}
	if !strings.Contains(out.String(), "Aborted") {
		t.Errorf("expected abort message, got %q", out.String())
	}
}

func TestRunMarkDoneConfirmed(t *testing.T) {
	doer := &recordingDoer{}
	notifications := []Notification{
		{ID: "1", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "o/r"}},
	}
	var out bytes.Buffer
	if err := runMarkDone(doer, notifications, false, strings.NewReader("y\n"), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(doer.calls) != 1 || doer.calls[0] != "DELETE notifications/threads/1" {
		t.Errorf("calls = %v", doer.calls)
	}
	if !strings.Contains(out.String(), "Marked 1 notification(s) as done") {
		t.Errorf("unexpected output %q", out.String())
	}
}

func TestRunUnsubscribeConfirmed(t *testing.T) {
	doer := &recordingDoer{}
	notifications := []Notification{
		{ID: "9", Subject: NotificationSubject{Title: "A"}, Repository: NotificationRepo{FullName: "o/r"}},
	}
	var out bytes.Buffer
	if err := runUnsubscribe(doer, notifications, false, strings.NewReader("y\n"), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		"DELETE notifications/threads/9/subscription",
		"DELETE notifications/threads/9",
	}
	if len(doer.calls) != 2 || doer.calls[0] != want[0] || doer.calls[1] != want[1] {
		t.Errorf("calls = %v, want %v", doer.calls, want)
	}
	if !strings.Contains(out.String(), "Unsubscribed from and marked 1 notification(s) as done") {
		t.Errorf("unexpected output %q", out.String())
	}
}

func TestUnsubscribeAndDoneStopsOnError(t *testing.T) {
	doer := &recordingDoer{err: errors.New("boom")}
	if err := unsubscribeAndDone(doer, "9"); err == nil {
		t.Fatal("expected error to propagate")
	}
	// The subscription delete is attempted; the done delete must not run after it fails.
	if len(doer.calls) != 1 || doer.calls[0] != "DELETE notifications/threads/9/subscription" {
		t.Errorf("calls = %v, want only the subscription delete", doer.calls)
	}
}

func TestParseArgsMarkReadAndDryRun(t *testing.T) {
	opts, err := parseArgs([]string{"--mark-read", "--dry-run"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !opts.markRead || !opts.dryRun {
		t.Errorf("markRead=%v dryRun=%v, want both true", opts.markRead, opts.dryRun)
	}
}

func TestParseArgsMutationConflict(t *testing.T) {
	conflicts := [][]string{
		{"--mark-read", "--mark-done"},
		{"--mark-read", "--unsubscribe"},
		{"--mark-done", "--unsubscribe"},
		{"--mark-read", "--mark-done", "--unsubscribe"},
	}
	for _, args := range conflicts {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("expected error for %v", args)
		}
	}
}
