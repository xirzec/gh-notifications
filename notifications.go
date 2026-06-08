package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/tableprinter"
	"github.com/cli/go-gh/v2/pkg/term"
)

// Notification represents a single GitHub notification thread.
// See https://docs.github.com/en/rest/activity/notifications
type Notification struct {
	ID         string             `json:"id"`
	Reason     string             `json:"reason"`
	Unread     bool               `json:"unread"`
	UpdatedAt  time.Time          `json:"updated_at"`
	Subject    NotificationSubject `json:"subject"`
	Repository NotificationRepo   `json:"repository"`
}

type NotificationSubject struct {
	Title string `json:"title"`
	Type  string `json:"type"`
	URL   string `json:"url"`
}

type NotificationRepo struct {
	FullName string `json:"full_name"`
}

// options holds parsed command-line options.
type options struct {
	// repo, when set, limits notifications to a single OWNER/REPO.
	repo string
}

// parseArgs parses command-line arguments into options.
func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("gh-notifications", flag.ContinueOnError)
	var opts options
	fs.StringVar(&opts.repo, "repo", "", "Filter notifications by repository (OWNER/REPO)")
	fs.StringVar(&opts.repo, "R", "", "Filter notifications by repository (OWNER/REPO) (shorthand)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	if opts.repo != "" {
		if !strings.Contains(strings.Trim(opts.repo, "/"), "/") || strings.Count(opts.repo, "/") != 1 {
			return options{}, fmt.Errorf("invalid repository %q: expected OWNER/REPO format", opts.repo)
		}
	}
	return opts, nil
}

// notificationsEndpoint returns the REST endpoint for the given options.
func notificationsEndpoint(opts options) string {
	if opts.repo != "" {
		return fmt.Sprintf("repos/%s/notifications", opts.repo)
	}
	return "notifications"
}

// fetchNotifications retrieves unread notifications for the authenticated user.
func fetchNotifications(client *api.RESTClient, opts options) ([]Notification, error) {
	var notifications []Notification
	if err := client.Get(notificationsEndpoint(opts), &notifications); err != nil {
		return nil, err
	}
	return notifications, nil
}

// renderNotifications writes the notifications to out as a table.
func renderNotifications(out io.Writer, notifications []Notification, isTTY bool, width int) error {
	if len(notifications) == 0 {
		_, err := fmt.Fprintln(out, "No unread notifications")
		return err
	}

	t := tableprinter.New(out, isTTY, width)
	t.AddHeader([]string{"REPOSITORY", "REASON", "TITLE", "AGE"})
	for _, n := range notifications {
		t.AddField(n.Repository.FullName)
		t.AddField(n.Reason)
		t.AddField(n.Subject.Title)
		t.AddField(relativeAge(n.UpdatedAt, time.Now()))
		t.EndRow()
	}
	return t.Render()
}

// relativeAge returns a short human-readable age string like "3h" or "2d".
func relativeAge(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// runNotifications fetches and displays the authenticated user's notifications.
func runNotifications(opts options) error {
	client, err := api.DefaultRESTClient()
	if err != nil {
		return err
	}

	notifications, err := fetchNotifications(client, opts)
	if err != nil {
		return err
	}

	terminal := term.FromEnv()
	width, _, _ := terminal.Size()
	return renderNotifications(terminal.Out(), notifications, terminal.IsTerminalOutput(), width)
}
