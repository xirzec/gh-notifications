package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/tableprinter"
	"github.com/cli/go-gh/v2/pkg/term"
)

// maxPerPage is the maximum page size accepted by the notifications API.
const maxPerPage = 50

var linkRE = regexp.MustCompile(`<([^>]+)>;\s*rel="([^"]+)"`)

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
	// filter, when set, keeps only notifications whose title contains it
	// (case-insensitive).
	filter string
	// itemType, when set, keeps only notifications of a given subject type
	// (e.g. issue, pr).
	itemType string
	// state, when set, keeps only issues/PRs in the given state
	// (open, closed, or merged). Requires fetching each item.
	state string
	// interactive, when true, prompts the user to pick a notification to
	// open in a web browser instead of printing the table.
	interactive bool
	// showReason, when true, includes the REASON column in the table output.
	showReason bool
}

// parseArgs parses command-line arguments into options.
func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("gh-notifications", flag.ContinueOnError)
	var opts options
	fs.StringVar(&opts.repo, "repo", "", "Filter notifications by repository (OWNER/REPO)")
	fs.StringVar(&opts.repo, "R", "", "Filter notifications by repository (OWNER/REPO) (shorthand)")
	fs.StringVar(&opts.filter, "filter", "", "Keep only notifications whose title contains this text (case-insensitive)")
	fs.StringVar(&opts.filter, "f", "", "Keep only notifications whose title contains this text (case-insensitive) (shorthand)")
	fs.StringVar(&opts.itemType, "type", "", "Keep only notifications of this type (issue, pr, commit, release, discussion, ...)")
	fs.StringVar(&opts.itemType, "t", "", "Keep only notifications of this type (shorthand)")
	fs.StringVar(&opts.state, "state", "", "Keep only issues/PRs in this state (open, closed, merged)")
	fs.BoolVar(&opts.interactive, "interactive", false, "Interactively select a notification to open in the browser")
	fs.BoolVar(&opts.interactive, "i", false, "Interactively select a notification to open in the browser (shorthand)")
	fs.BoolVar(&opts.showReason, "show-reason", false, "Include the REASON column in the output")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	if opts.repo != "" {
		if !strings.Contains(strings.Trim(opts.repo, "/"), "/") || strings.Count(opts.repo, "/") != 1 {
			return options{}, fmt.Errorf("invalid repository %q: expected OWNER/REPO format", opts.repo)
		}
	}
	if opts.state != "" {
		switch strings.ToLower(opts.state) {
		case "open", "closed", "merged":
		default:
			return options{}, fmt.Errorf("invalid state %q: expected open, closed, or merged", opts.state)
		}
	}
	return opts, nil
}

// filterByTitle returns the notifications whose subject title contains the given
// text, matched case-insensitively. An empty filter returns the input unchanged.
func filterByTitle(notifications []Notification, filter string) []Notification {
	if filter == "" {
		return notifications
	}
	needle := strings.ToLower(filter)
	filtered := make([]Notification, 0, len(notifications))
	for _, n := range notifications {
		if strings.Contains(strings.ToLower(n.Subject.Title), needle) {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

// canonicalType maps a user-supplied type name (with friendly aliases such as
// "pr" or "issue") to the subject type string used by the notifications API.
// Unrecognized values are returned unchanged so any subject type can be matched.
func canonicalType(s string) string {
	switch strings.ToLower(strings.ReplaceAll(s, "-", "")) {
	case "pr", "pull", "pulls", "pullrequest", "pullrequests":
		return "PullRequest"
	case "issue", "issues":
		return "Issue"
	case "commit", "commits":
		return "Commit"
	case "release", "releases":
		return "Release"
	case "discussion", "discussions":
		return "Discussion"
	default:
		return s
	}
}

// filterByType returns the notifications whose subject type matches the given
// type, accepting friendly aliases. An empty type returns the input unchanged.
func filterByType(notifications []Notification, itemType string) []Notification {
	if itemType == "" {
		return notifications
	}
	want := canonicalType(itemType)
	filtered := make([]Notification, 0, len(notifications))
	for _, n := range notifications {
		if strings.EqualFold(n.Subject.Type, want) {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

// itemState holds the open/closed/merged status of an issue or pull request.
type itemState struct {
	State  string `json:"state"`
	Merged bool   `json:"merged"`
}

// matchesState reports whether an item's state satisfies the requested filter.
// "closed" excludes merged pull requests, which are matched by "merged".
func matchesState(s itemState, want string) bool {
	switch strings.ToLower(want) {
	case "open":
		return strings.EqualFold(s.State, "open")
	case "closed":
		return strings.EqualFold(s.State, "closed") && !s.Merged
	case "merged":
		return s.Merged
	default:
		return false
	}
}

// fetchItemState retrieves the state of a notification's subject (issue or PR).
func fetchItemState(doer requestDoer, n Notification) (itemState, bool) {
	if n.Subject.URL == "" {
		return itemState{}, false
	}
	resp, err := doer.Request(http.MethodGet, n.Subject.URL, nil)
	if err != nil {
		return itemState{}, false
	}
	var s itemState
	decodeErr := json.NewDecoder(resp.Body).Decode(&s)
	resp.Body.Close()
	if decodeErr != nil {
		return itemState{}, false
	}
	return s, true
}

// stateFetchConcurrency bounds the number of concurrent state lookups.
const stateFetchConcurrency = 10

// filterByState keeps only issues/PRs whose fetched state matches the requested
// state. Subjects without a state (commits, releases, etc.) are excluded. An
// empty state returns the input unchanged. State lookups run concurrently.
func filterByState(doer requestDoer, notifications []Notification, state string) []Notification {
	if state == "" {
		return notifications
	}

	keep := make([]bool, len(notifications))
	sem := make(chan struct{}, stateFetchConcurrency)
	var wg sync.WaitGroup

	for i := range notifications {
		n := notifications[i]
		if !strings.EqualFold(n.Subject.Type, "Issue") && !strings.EqualFold(n.Subject.Type, "PullRequest") {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, n Notification) {
			defer wg.Done()
			defer func() { <-sem }()
			if s, ok := fetchItemState(doer, n); ok && matchesState(s, state) {
				keep[idx] = true
			}
		}(i, n)
	}
	wg.Wait()

	filtered := make([]Notification, 0, len(notifications))
	for i, k := range keep {
		if k {
			filtered = append(filtered, notifications[i])
		}
	}
	return filtered
}

// notificationsEndpoint returns the REST endpoint for the given options.
func notificationsEndpoint(opts options) string {
	base := "notifications"
	if opts.repo != "" {
		base = fmt.Sprintf("repos/%s/notifications", opts.repo)
	}
	return fmt.Sprintf("%s?per_page=%d", base, maxPerPage)
}

// findNextPage returns the URL of the next page from the response Link header.
func findNextPage(resp *http.Response) (string, bool) {
	for _, m := range linkRE.FindAllStringSubmatch(resp.Header.Get("Link"), -1) {
		if len(m) > 2 && m[2] == "next" {
			return m[1], true
		}
	}
	return "", false
}

// fetchNotifications retrieves all unread notifications for the authenticated
// user, following pagination until every page has been collected.
func fetchNotifications(client *api.RESTClient, opts options) ([]Notification, error) {
	var all []Notification
	requestPath := notificationsEndpoint(opts)
	for {
		resp, err := client.Request(http.MethodGet, requestPath, nil)
		if err != nil {
			return nil, err
		}

		var page []Notification
		decodeErr := json.NewDecoder(resp.Body).Decode(&page)
		closeErr := resp.Body.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		all = append(all, page...)

		next, hasNext := findNextPage(resp)
		if !hasNext {
			break
		}
		requestPath = next
	}
	return all, nil
}

// displayType returns a concise label for a notification's subject type,
// shortening the verbose "PullRequest" to "PR".
func displayType(subjectType string) string {
	if subjectType == "PullRequest" {
		return "PR"
	}
	return subjectType
}

// renderNotifications writes the notifications to out as a table. The REASON
// column is included only when showReason is true.
func renderNotifications(out io.Writer, notifications []Notification, isTTY bool, width int, showReason bool) error {
	if len(notifications) == 0 {
		_, err := fmt.Fprintln(out, "No unread notifications")
		return err
	}

	t := tableprinter.New(out, isTTY, width)
	header := []string{"REPOSITORY", "TYPE"}
	if showReason {
		header = append(header, "REASON")
	}
	header = append(header, "TITLE", "AGE")
	t.AddHeader(header)

	for _, n := range notifications {
		t.AddField(n.Repository.FullName)
		t.AddField(displayType(n.Subject.Type))
		if showReason {
			t.AddField(n.Reason)
		}
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

	notifications = filterByTitle(notifications, opts.filter)
	notifications = filterByType(notifications, opts.itemType)
	notifications = filterByState(client, notifications, opts.state)

	if opts.interactive {
		return selectAndOpen(client, notifications)
	}

	terminal := term.FromEnv()
	width, _, _ := terminal.Size()
	return renderNotifications(terminal.Out(), notifications, terminal.IsTerminalOutput(), width, opts.showReason)
}

// requestDoer is the subset of api.RESTClient used to resolve web URLs.
// It allows the resolution logic to be unit tested with a fake.
type requestDoer interface {
	Request(method, path string, body io.Reader) (*http.Response, error)
}

// resolveWebURL determines the browser URL for a notification. It fetches the
// subject's API resource to read its html_url, falling back to the repository
// page when the subject has no resolvable web URL (e.g. discussions or alerts).
func resolveWebURL(doer requestDoer, n Notification) string {
	if n.Subject.URL != "" {
		if resp, err := doer.Request(http.MethodGet, n.Subject.URL, nil); err == nil {
			var subject struct {
				HTMLURL string `json:"html_url"`
			}
			decodeErr := json.NewDecoder(resp.Body).Decode(&subject)
			resp.Body.Close()
			if decodeErr == nil && subject.HTMLURL != "" {
				return subject.HTMLURL
			}
		}
	}
	return "https://github.com/" + n.Repository.FullName
}
