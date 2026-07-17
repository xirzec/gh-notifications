package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
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
	ID         string              `json:"id"`
	Reason     string              `json:"reason"`
	Unread     bool                `json:"unread"`
	UpdatedAt  time.Time           `json:"updated_at"`
	Subject    NotificationSubject `json:"subject"`
	Repository NotificationRepo    `json:"repository"`
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
	// all, when true, includes notifications already marked as read.
	all bool
	// filter, when set, keeps only notifications whose title contains it
	// (case-insensitive).
	filter string
	// itemType, when set, keeps only notifications of a given subject type
	// (e.g. issue, pr).
	itemType string
	// state, when set, keeps only issues/PRs in the given state or close
	// reason. Requires fetching each item.
	state string
	// draft, when true, keeps only draft pull requests. Requires fetching
	// each item.
	draft bool
	// interactive, when true, prompts the user to pick a notification to
	// open in a web browser instead of printing the table.
	interactive bool
	// showReason, when true, includes the REASON column in the table output.
	showReason bool
	// markRead, when true, marks the (filtered) notifications as read.
	markRead bool
	// markDone, when true, marks the (filtered) notifications as done,
	// removing them from the inbox.
	markDone bool
	// unsubscribe, when true, unsubscribes from the (filtered) notification
	// threads.
	unsubscribe bool
	// dryRun, when true, reports what a mutating command would do without
	// calling the API.
	dryRun bool
	// assumeYes, when true, skips interactive confirmations.
	assumeYes bool
	// tags holds the tags attached by the `save` command.
	tags []string
	// help, when true, prints command usage without making API calls.
	help bool
}

// stringSliceFlag collects repeated string flag values into a slice.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }

func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func addFilterFlags(fs *flag.FlagSet, opts *options) {
	fs.StringVar(&opts.repo, "repo", "", "Filter notifications by repository (OWNER/REPO)")
	fs.StringVar(&opts.repo, "R", "", "Filter notifications by repository (OWNER/REPO) (shorthand)")
	fs.BoolVar(&opts.all, "all", false, "Include notifications already marked as read")
	fs.BoolVar(&opts.all, "a", false, "Include notifications already marked as read (shorthand)")
	fs.StringVar(&opts.filter, "filter", "", "Keep only notifications whose title contains this text (case-insensitive)")
	fs.StringVar(&opts.filter, "f", "", "Keep only notifications whose title contains this text (case-insensitive) (shorthand)")
	fs.StringVar(&opts.itemType, "type", "", "Keep only notifications of this type (issue, pr, commit, release, discussion, ...)")
	fs.StringVar(&opts.itemType, "t", "", "Keep only notifications of this type (shorthand)")
	fs.StringVar(&opts.state, "state", "", "Keep only issues/PRs in this state (open, closed, merged, not-planned, completed); closed excludes merged PRs")
	fs.BoolVar(&opts.draft, "draft", false, "Keep only draft pull requests")
}

func addActionFlags(fs *flag.FlagSet, opts *options) {
	fs.BoolVar(&opts.markRead, "mark-read", false, "Mark the matching notifications as read (asks for confirmation)")
	fs.BoolVar(&opts.markDone, "mark-done", false, "Mark the matching notifications as done, removing them from the inbox (asks for confirmation)")
	fs.BoolVar(&opts.unsubscribe, "unsubscribe", false, "Unsubscribe from the matching notification threads (asks for confirmation)")
}

func addAssumeYesFlags(fs *flag.FlagSet, opts *options) {
	fs.BoolVar(&opts.assumeYes, "yes", false, "Skip the confirmation prompt for mutating commands")
	fs.BoolVar(&opts.assumeYes, "y", false, "Skip the confirmation prompt for mutating commands (shorthand)")
}

// newFlagSet creates the flag set for the default notification listing and
// mutation flow.
func newFlagSet(name string, opts *options) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	addFilterFlags(fs, opts)
	fs.BoolVar(&opts.interactive, "interactive", false, "Interactively select a notification to open in the browser")
	fs.BoolVar(&opts.interactive, "i", false, "Interactively select a notification to open in the browser (shorthand)")
	fs.BoolVar(&opts.showReason, "show-reason", false, "Include the REASON column in the output")
	addActionFlags(fs, opts)
	fs.BoolVar(&opts.dryRun, "dry-run", false, "Show what a mutating command would do without calling the API")
	addAssumeYesFlags(fs, opts)
	addHelpFlags(fs, &opts.help)
	return fs
}

// validateOptions checks parsed options for invalid or conflicting values.
func validateOptions(opts options) error {
	if opts.repo != "" {
		if !strings.Contains(strings.Trim(opts.repo, "/"), "/") || strings.Count(opts.repo, "/") != 1 {
			return fmt.Errorf("invalid repository %q: expected OWNER/REPO format", opts.repo)
		}
	}
	if opts.state != "" {
		if !validStates[normalizeState(opts.state)] {
			return fmt.Errorf("invalid state %q: expected open, closed, merged, not-planned, or completed", opts.state)
		}
	}
	mutations := 0
	for _, set := range []bool{opts.markRead, opts.markDone, opts.unsubscribe} {
		if set {
			mutations++
		}
	}
	if mutations > 1 {
		return fmt.Errorf("only one of --mark-read, --mark-done, or --unsubscribe may be used at a time")
	}
	return nil
}

// parseArgs parses command-line arguments into options.
func parseArgs(args []string) (options, error) {
	var opts options
	fs := newFlagSet("gh-notifications", &opts)
	setRootUsage(fs)
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.help {
		return opts, nil
	}
	if err := validateOptions(opts); err != nil {
		return options{}, err
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

// graphQLDoer is the subset of api.GraphQLClient used to batch-fetch item
// states. It allows the batching logic to be unit tested with a fake.
type graphQLDoer interface {
	Do(query string, variables map[string]interface{}, response interface{}) error
}

// subjectRef identifies an issue or pull request by repository and number.
type subjectRef struct {
	owner  string
	repo   string
	number int
}

// subjectURLRE extracts owner/repo/number from a notification subject API URL,
// e.g. https://api.github.com/repos/OWNER/REPO/issues/123 or .../pulls/123.
var subjectURLRE = regexp.MustCompile(`/repos/([^/]+)/([^/]+)/(?:issues|pulls)/(\d+)`)

// parseSubjectRef parses an issue/PR reference from a subject API URL.
func parseSubjectRef(apiURL string) (subjectRef, bool) {
	m := subjectURLRE.FindStringSubmatch(apiURL)
	if m == nil {
		return subjectRef{}, false
	}
	number, err := strconv.Atoi(m[3])
	if err != nil {
		return subjectRef{}, false
	}
	return subjectRef{owner: m[1], repo: m[2], number: number}, true
}

// normalizeState canonicalizes a user-supplied state name: lower-cased with
// hyphens treated as underscores (so "not-planned" == "not_planned").
func normalizeState(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "-", "_"))
}

// validStates lists the accepted --state values (after normalization).
var validStates = map[string]bool{
	"open":        true,
	"closed":      true,
	"merged":      true,
	"not_planned": true,
	"unplanned":   true,
	"notplanned":  true,
	"completed":   true,
}

// matchesState reports whether an item's detail satisfies the requested state
// filter. "closed" matches any closed item; "not-planned"/"completed" further
// require the corresponding issue close reason. A merged pull request reports
// state "merged" rather than "closed".
func matchesState(d itemDetail, want string) bool {
	switch normalizeState(want) {
	case "open":
		return d.state == "open"
	case "closed":
		return d.state == "closed"
	case "merged":
		return d.state == "merged"
	case "not_planned", "unplanned", "notplanned":
		return d.state == "closed" && d.stateReason == "not_planned"
	case "completed":
		return d.state == "closed" && d.stateReason == "completed"
	default:
		return false
	}
}

// stateBatchSize bounds how many items are requested per GraphQL call to keep
// query complexity within the API's limits.
const stateBatchSize = 50

// isIssueOrPR reports whether the subject type can have an open/closed state.
func isIssueOrPR(subjectType string) bool {
	return strings.EqualFold(subjectType, "Issue") || strings.EqualFold(subjectType, "PullRequest")
}

// parseGraphQLRateLimit reads the GraphQL `rateLimit` object from a response.
func parseGraphQLRateLimit(raw json.RawMessage) (rateLimit, bool) {
	var v struct {
		Limit     int    `json:"limit"`
		Remaining int    `json:"remaining"`
		ResetAt   string `json:"resetAt"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return rateLimit{}, false
	}
	rl := rateLimit{remaining: v.Remaining, limit: v.Limit}
	if t, err := time.Parse(time.RFC3339, v.ResetAt); err == nil {
		rl.reset = t
	}
	return rl, true
}

// itemDetail holds the GraphQL-fetched attributes of an issue or pull request.
type itemDetail struct {
	state       string // normalized: "open", "closed", or "merged"
	stateReason string // normalized issue close reason: "completed", "not_planned", "reopened", or ""
	isDraft     bool   // true only for draft pull requests
}

// fetchItemDetails returns a map from notification index to its issue/PR detail,
// fetched in batches via a single GraphQL query per batch. Notifications that
// are not issues/PRs, or whose subject URL cannot be parsed, are absent from the
// map. The second return value, when non-nil, is the GraphQL API quota reported
// by the most recent batch.
func fetchItemDetails(doer graphQLDoer, notifications []Notification) (map[int]itemDetail, *rateLimit) {
	type entry struct {
		idx int
		ref subjectRef
	}
	var entries []entry
	for i, n := range notifications {
		if !isIssueOrPR(n.Subject.Type) {
			continue
		}
		if ref, ok := parseSubjectRef(n.Subject.URL); ok {
			entries = append(entries, entry{idx: i, ref: ref})
		}
	}

	details := make(map[int]itemDetail)
	var quota *rateLimit
	for start := 0; start < len(entries); start += stateBatchSize {
		end := start + stateBatchSize
		if end > len(entries) {
			end = len(entries)
		}
		batch := entries[start:end]

		params := make([]string, 0, len(batch))
		fields := make([]string, 0, len(batch))
		variables := make(map[string]interface{}, len(batch)*3)
		for j, e := range batch {
			o, r, num := fmt.Sprintf("o%d", j), fmt.Sprintf("r%d", j), fmt.Sprintf("n%d", j)
			params = append(params, fmt.Sprintf("$%s:String!,$%s:String!,$%s:Int!", o, r, num))
			fields = append(fields, fmt.Sprintf(
				"i%d: repository(owner:$%s,name:$%s){issueOrPullRequest(number:$%s){__typename ... on Issue{state stateReason} ... on PullRequest{state isDraft}}}",
				j, o, r, num))
			variables[o] = e.ref.owner
			variables[r] = e.ref.repo
			variables[num] = e.ref.number
		}
		query := fmt.Sprintf("query(%s){rateLimit{limit remaining resetAt}\n%s}", strings.Join(params, ","), strings.Join(fields, "\n"))

		var resp map[string]json.RawMessage
		if err := doer.Do(query, variables, &resp); err != nil {
			continue
		}

		if rlRaw, ok := resp["rateLimit"]; ok {
			if rl, ok := parseGraphQLRateLimit(rlRaw); ok {
				rl := rl
				quota = &rl
			}
		}

		for j, e := range batch {
			itemRaw, ok := resp[fmt.Sprintf("i%d", j)]
			if !ok {
				continue
			}
			var node struct {
				IssueOrPullRequest *struct {
					State       string `json:"state"`
					StateReason string `json:"stateReason"`
					IsDraft     bool   `json:"isDraft"`
				} `json:"issueOrPullRequest"`
			}
			if err := json.Unmarshal(itemRaw, &node); err == nil && node.IssueOrPullRequest != nil {
				details[e.idx] = itemDetail{
					state:       strings.ToLower(node.IssueOrPullRequest.State),
					stateReason: strings.ToLower(node.IssueOrPullRequest.StateReason),
					isDraft:     node.IssueOrPullRequest.IsDraft,
				}
			}
		}
	}
	return details, quota
}

// filterByState keeps only issues/PRs whose state matches the requested state,
// using pre-fetched details. An empty state returns the input unchanged.
func filterByState(notifications []Notification, details map[int]itemDetail, state string) []Notification {
	if state == "" {
		return notifications
	}
	filtered := make([]Notification, 0, len(notifications))
	for i, n := range notifications {
		if d, ok := details[i]; ok && matchesState(d, state) {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

// filterByDraft keeps only draft pull requests, using pre-fetched details. When
// draft is false it returns the input unchanged.
func filterByDraft(notifications []Notification, details map[int]itemDetail, draft bool) []Notification {
	if !draft {
		return notifications
	}
	filtered := make([]Notification, 0, len(notifications))
	for i, n := range notifications {
		if d, ok := details[i]; ok && d.isDraft {
			filtered = append(filtered, n)
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
	endpoint := fmt.Sprintf("%s?per_page=%d", base, maxPerPage)
	if opts.all {
		endpoint += "&all=true"
	}
	return endpoint
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
func fetchNotifications(doer requestDoer, opts options) ([]Notification, error) {
	var all []Notification
	requestPath := notificationsEndpoint(opts)
	for {
		resp, err := doer.Request(http.MethodGet, requestPath, nil)
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

// loadFilteredNotifications fetches the authenticated user's notifications and
// applies all of opts' filters (title, type, and the batched GraphQL
// state/draft filters), returning the resulting slice. REST and GraphQL quota
// are recorded on the tracker.
func loadFilteredNotifications(tracker *rateLimitTracker, opts options) ([]Notification, error) {
	notifications, err := fetchNotifications(tracker, opts)
	if err != nil {
		return nil, err
	}

	notifications = filterByTitle(notifications, opts.filter)
	notifications = filterByType(notifications, opts.itemType)
	if opts.state != "" || opts.draft {
		gqlClient, err := api.DefaultGraphQLClient()
		if err != nil {
			return nil, err
		}
		details, gqlQuota := fetchItemDetails(gqlClient, notifications)
		tracker.setGraphQL(gqlQuota)
		notifications = filterByState(notifications, details, opts.state)
		notifications = filterByDraft(notifications, details, opts.draft)
	}
	return notifications, nil
}

// runNotifications fetches and displays the authenticated user's notifications.
func runNotifications(opts options) error {
	client, err := api.DefaultRESTClient()
	if err != nil {
		return err
	}

	// Track the REST rate limit across all requests and report it on exit.
	tracker := newRateLimitTracker(client)
	defer tracker.report(os.Stderr)

	notifications, err := loadFilteredNotifications(tracker, opts)
	if err != nil {
		return err
	}

	if opts.markRead {
		return runMarkRead(tracker, notifications, opts.dryRun, opts.assumeYes, os.Stdin, os.Stdout)
	}
	if opts.markDone {
		return runMarkDone(tracker, notifications, opts.dryRun, opts.assumeYes, os.Stdin, os.Stdout)
	}
	if opts.unsubscribe {
		return runUnsubscribe(tracker, notifications, opts.dryRun, opts.assumeYes, os.Stdin, os.Stdout)
	}

	if opts.interactive {
		return selectAndOpen(tracker, notifications)
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
