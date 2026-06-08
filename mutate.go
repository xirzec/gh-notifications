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

// markThread applies the named mark action ("read" or "done") to a thread.
func markThread(doer requestDoer, threadID, action string) error {
	if action == "done" {
		return markThreadDone(doer, threadID)
	}
	return markThreadRead(doer, threadID)
}

// runMarkRead marks the given notifications as read (see runMark).
func runMarkRead(doer requestDoer, notifications []Notification, dryRun bool, in io.Reader, out io.Writer) error {
	return runMark(doer, notifications, dryRun, in, out, "read")
}

// runMarkDone marks the given notifications as done (see runMark).
func runMarkDone(doer requestDoer, notifications []Notification, dryRun bool, in io.Reader, out io.Writer) error {
	return runMark(doer, notifications, dryRun, in, out, "done")
}

// runMark applies a mark action ("read" or "done") to the given notifications
// after listing them and asking for confirmation. With dryRun set, it only
// reports what would happen and never calls the API.
func runMark(doer requestDoer, notifications []Notification, dryRun bool, in io.Reader, out io.Writer, action string) error {
	if len(notifications) == 0 {
		fmt.Fprintf(out, "No notifications to mark as %s\n", action)
		return nil
	}

	if dryRun {
		fmt.Fprintf(out, "Dry run: would mark %d notification(s) as %s:\n", len(notifications), action)
		listNotifications(out, notifications)
		return nil
	}

	fmt.Fprintf(out, "About to mark %d notification(s) as %s:\n", len(notifications), action)
	listNotifications(out, notifications)

	ok, err := confirm(in, out, fmt.Sprintf("Mark %d notification(s) as %s?", len(notifications), action))
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "Aborted; no notifications were changed")
		return nil
	}

	for _, n := range notifications {
		if err := markThread(doer, n.ID, action); err != nil {
			return fmt.Errorf("marking notification %s as %s: %w", n.ID, action, err)
		}
	}
	fmt.Fprintf(out, "Marked %d notification(s) as %s\n", len(notifications), action)
	return nil
}

// listNotifications writes a simple bullet list of notifications to out.
func listNotifications(out io.Writer, notifications []Notification) {
	for _, n := range notifications {
		fmt.Fprintf(out, "  - %s  %s\n", n.Repository.FullName, n.Subject.Title)
	}
}
