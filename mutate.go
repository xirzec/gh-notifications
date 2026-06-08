package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// confirm prints prompt and reads a yes/no answer from in. Anything other than
// "y"/"yes" (case-insensitive) is treated as no, so the safe default is to not
// proceed.
func confirm(in io.Reader, out io.Writer, prompt string) (bool, error) {
	fmt.Fprintf(out, "%s [y/N] ", prompt)
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// markThreadRead marks a single notification thread as read.
// See https://docs.github.com/en/rest/activity/notifications#mark-a-thread-as-read
func markThreadRead(doer requestDoer, threadID string) error {
	resp, err := doer.Request(http.MethodPatch, "notifications/threads/"+threadID, nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// markThreadDone marks a single notification thread as done, removing it from
// the inbox.
// See https://docs.github.com/en/rest/activity/notifications#mark-a-thread-as-done
func markThreadDone(doer requestDoer, threadID string) error {
	resp, err := doer.Request(http.MethodDelete, "notifications/threads/"+threadID, nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// unsubscribeThread deletes a thread's subscription so it stops generating
// future notifications.
// See https://docs.github.com/en/rest/activity/notifications#delete-a-thread-subscription
func unsubscribeThread(doer requestDoer, threadID string) error {
	resp, err := doer.Request(http.MethodDelete, "notifications/threads/"+threadID+"/subscription", nil)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

// unsubscribeAndDone unsubscribes from a thread and then marks it as done,
// mirroring the GitHub web "Unsubscribe" action which also clears the thread
// from the inbox.
func unsubscribeAndDone(doer requestDoer, threadID string) error {
	if err := unsubscribeThread(doer, threadID); err != nil {
		return err
	}
	return markThreadDone(doer, threadID)
}

// threadAction describes a mutating action applied to notification threads,
// including the API call and the user-facing phrasing for each message. The
// %d-bearing templates are formatted with the notification count.
type threadAction struct {
	apply   func(doer requestDoer, threadID string) error
	empty   string // shown when there is nothing to act on
	dryRun  string // "Dry run: would ... %d ...:"
	pending string // "About to ... %d ...:"
	confirm string // confirmation question "... %d ...?"
	done    string // success message "... %d ..."
	prompt  string // interactive prompt; formatted with the item title (%q)
	past    string // past participle for the interactive status message
}

// threadActions maps an action name to its behavior and phrasing. It is the
// single source of truth shared by the CLI and interactive flows.
var threadActions = map[string]threadAction{
	"read": {
		apply:   markThreadRead,
		empty:   "No notifications to mark as read",
		dryRun:  "Dry run: would mark %d notification(s) as read:",
		pending: "About to mark %d notification(s) as read:",
		confirm: "Mark %d notification(s) as read?",
		done:    "Marked %d notification(s) as read",
		prompt:  "Mark %q as read?",
		past:    "read",
	},
	"done": {
		apply:   markThreadDone,
		empty:   "No notifications to mark as done",
		dryRun:  "Dry run: would mark %d notification(s) as done:",
		pending: "About to mark %d notification(s) as done:",
		confirm: "Mark %d notification(s) as done?",
		done:    "Marked %d notification(s) as done",
		prompt:  "Mark %q as done?",
		past:    "done",
	},
	"unsubscribe": {
		apply:   unsubscribeAndDone,
		empty:   "No notifications to unsubscribe from",
		dryRun:  "Dry run: would unsubscribe from %d notification(s) (also marking them done):",
		pending: "About to unsubscribe from %d notification(s) (also marking them done):",
		confirm: "Unsubscribe from %d notification(s) (also marks them done)?",
		done:    "Unsubscribed from and marked %d notification(s) as done",
		prompt:  "Unsubscribe from %q (also marks it done)?",
		past:    "unsubscribed & done",
	},
}

// runMarkRead marks the given notifications as read (see runMark).
func runMarkRead(doer requestDoer, notifications []Notification, dryRun bool, in io.Reader, out io.Writer) error {
	return runMark(doer, notifications, dryRun, in, out, "read")
}

// runMarkDone marks the given notifications as done (see runMark).
func runMarkDone(doer requestDoer, notifications []Notification, dryRun bool, in io.Reader, out io.Writer) error {
	return runMark(doer, notifications, dryRun, in, out, "done")
}

// runUnsubscribe unsubscribes from the given notifications (see runMark).
func runUnsubscribe(doer requestDoer, notifications []Notification, dryRun bool, in io.Reader, out io.Writer) error {
	return runMark(doer, notifications, dryRun, in, out, "unsubscribe")
}

// runMark applies a thread action to the given notifications after listing them
// and asking for confirmation. With dryRun set, it only reports what would
// happen and never calls the API.
func runMark(doer requestDoer, notifications []Notification, dryRun bool, in io.Reader, out io.Writer, action string) error {
	a := threadActions[action]

	if len(notifications) == 0 {
		fmt.Fprintln(out, a.empty)
		return nil
	}

	if dryRun {
		fmt.Fprintf(out, a.dryRun+"\n", len(notifications))
		listNotifications(out, notifications)
		return nil
	}

	fmt.Fprintf(out, a.pending+"\n", len(notifications))
	listNotifications(out, notifications)

	ok, err := confirm(in, out, fmt.Sprintf(a.confirm, len(notifications)))
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "Aborted; no notifications were changed")
		return nil
	}

	for _, n := range notifications {
		if err := a.apply(doer, n.ID); err != nil {
			return fmt.Errorf("applying %s to notification %s: %w", action, n.ID, err)
		}
	}
	fmt.Fprintf(out, a.done+"\n", len(notifications))
	return nil
}

// listNotifications writes a simple bullet list of notifications to out.
func listNotifications(out io.Writer, notifications []Notification) {
	for _, n := range notifications {
		fmt.Fprintf(out, "  - %s  %s\n", n.Repository.FullName, n.Subject.Title)
	}
}
