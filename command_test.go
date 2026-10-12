package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

func TestExtractName(t *testing.T) {
	name, rest := extractName([]string{"foo", "--repo", "o/r"})
	if name != "foo" || len(rest) != 2 {
		t.Errorf("extractName = %q, %v", name, rest)
	}
	name, rest = extractName([]string{"--tag", "cleanup"})
	if name != "" || len(rest) != 2 {
		t.Errorf("expected no name, got %q, %v", name, rest)
	}
	name, rest = extractName(nil)
	if name != "" || len(rest) != 0 {
		t.Errorf("expected empty, got %q, %v", name, rest)
	}
}

func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"--state", "bogus", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			if err := runWithIO(args, strings.NewReader(""), &out); err != nil {
				t.Fatalf("runWithIO(%v): %v", args, err)
			}
			for _, want := range []string{
				"gh notifications [flags]",
				"Saved query commands:",
				"save <name> [flags]",
				"run --tag <tag> [flags]",
				"delete <name>",
				"edit",
				"--mark-read",
				"--author",
			} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("help missing %q:\n%s", want, out.String())
				}
			}
			if strings.Contains(out.String(), "\n  -help") || strings.Contains(out.String(), "\n  -mark-read") {
				t.Errorf("long flags should use two dashes:\n%s", out.String())
			}
		})
	}
}

func TestHelpUsesConventionalFlagPrefixes(t *testing.T) {
	var out bytes.Buffer
	if err := runWithIO([]string{"--help"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("runWithIO: %v", err)
	}
	for _, want := range []string{"\n  -h ", "\n  --help ", "\n  -R string ", "\n  --repo string "} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help missing conventionally formatted flag %q:\n%s", want, out.String())
		}
	}
}

func TestRunSubcommandHelp(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"save", []string{"save", "--help"}, "gh notifications save <name> [flags]"},
		{"save after name", []string{"save", "cleanup", "--help"}, "gh notifications save <name> [flags]"},
		{"list", []string{"list", "-h"}, "gh notifications list"},
		{"run", []string{"run", "--help"}, "gh notifications run <name> [flags]"},
		{"delete", []string{"delete", "--help"}, "gh notifications delete <name>"},
		{"edit", []string{"edit", "--help"}, "gh notifications edit"},
		{"help topic", []string{"help", "save"}, "gh notifications save <name> [flags]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := runWithIO(tc.args, strings.NewReader(""), &out); err != nil {
				t.Fatalf("runWithIO(%v): %v", tc.args, err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Errorf("help missing %q:\n%s", tc.want, out.String())
			}
		})
	}
}

func TestRunHelpErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown topic", []string{"help", "unknown"}, `unknown help topic "unknown"`},
		{"too many arguments", []string{"help", "save", "extra"}, "help accepts at most one command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runWithIO(tc.args, strings.NewReader(""), io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runWithIO(%v) error = %v, want %q", tc.args, err, tc.want)
			}
		})
	}
}

func TestHelpDoesNotModifySavedQueries(t *testing.T) {
	withTempQueriesFile(t)

	editorCalled := false
	prev := editorRunner
	editorRunner = func(*exec.Cmd) error {
		editorCalled = true
		return nil
	}
	t.Cleanup(func() { editorRunner = prev })

	for _, args := range [][]string{{"save", "--help"}, {"edit", "--help"}} {
		var out bytes.Buffer
		if err := runWithIO(args, strings.NewReader(""), &out); err != nil {
			t.Fatalf("runWithIO(%v): %v", args, err)
		}
		if _, err := os.Stat(queriesFilePath()); !os.IsNotExist(err) {
			t.Fatalf("runWithIO(%v) created the saved queries file; stat error = %v", args, err)
		}
	}
	if editorCalled {
		t.Error("edit --help launched the editor")
	}
}

func TestSaveHelpOnlyDocumentsSupportedFlags(t *testing.T) {
	var out bytes.Buffer
	if err := runWithIO([]string{"save", "--help"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("runWithIO: %v", err)
	}
	for _, want := range []string{"--tag value", "--yes", "--mark-read", "--author"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("save help missing %q:\n%s", want, out.String())
		}
	}
	for _, unwanted := range []string{"--all", "--dry-run", "--interactive", "--show-reason", "--unread-only"} {
		if strings.Contains(out.String(), unwanted) {
			t.Errorf("save help unexpectedly contains %q:\n%s", unwanted, out.String())
		}
	}
}

func TestCommandFlagSetsRejectUnsupportedNoOpFlags(t *testing.T) {
	t.Run("default rejects tag", func(t *testing.T) {
		var opts options
		fs := newFlagSet("gh-notifications", &opts)
		fs.SetOutput(io.Discard)
		if err := fs.Parse([]string{"--tag", "cleanup"}); err == nil {
			t.Error("expected --tag to be rejected by the default command")
		}
	})

	for _, arg := range []string{"--all", "--dry-run", "--interactive", "--show-reason", "--unread-only"} {
		t.Run("save rejects "+arg, func(t *testing.T) {
			var opts options
			fs, _ := newSaveFlagSet(&opts)
			fs.SetOutput(io.Discard)
			if err := fs.Parse([]string{arg}); err == nil {
				t.Errorf("expected %s to be rejected by save", arg)
			}
		})
	}
}

func TestRunSaveAndList(t *testing.T) {
	withTempQueriesFile(t)
	var out bytes.Buffer
	if err := runSave([]string{"cleanup", "--repo", "o/r", "--type", "pr", "--unsubscribe", "--tag", "cleanup"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("runSave: %v", err)
	}
	if !strings.Contains(out.String(), "Saved query \"cleanup\"") {
		t.Errorf("unexpected save output %q", out.String())
	}

	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("loadQueries: %v", err)
	}
	if len(queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(queries))
	}
	q := queries[0]
	if q.Repo != "o/r" || q.Type != "pr" || q.Scope != "" || q.All ||
		q.Action != "unsubscribe" || len(q.Tags) != 1 || q.Tags[0] != "cleanup" {
		t.Errorf("saved query = %+v", q)
	}

	// Re-saving the same name with --yes reports an update without prompting.
	out.Reset()
	if err := runSave([]string{"cleanup", "--repo", "o/r2", "--yes"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("runSave update: %v", err)
	}
	if !strings.Contains(out.String(), "Updated query") {
		t.Errorf("expected update message, got %q", out.String())
	}

	out.Reset()
	if err := runListQueries(nil, &out); err != nil {
		t.Fatalf("runListQueries: %v", err)
	}
	if !strings.Contains(out.String(), "cleanup") {
		t.Errorf("list missing query: %q", out.String())
	}
}

func TestRunSaveRequiresName(t *testing.T) {
	withTempQueriesFile(t)
	var out bytes.Buffer
	if err := runSave([]string{"--repo", "o/r"}, strings.NewReader(""), &out); err == nil {
		t.Error("expected error when name is missing")
	}
}

func TestRunSaveAuthorFilters(t *testing.T) {
	withTempQueriesFile(t)
	var out bytes.Buffer
	if err := runSave([]string{"triage", "--author", "alice"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("runSave: %v", err)
	}
	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("loadQueries: %v", err)
	}
	if len(queries) != 1 || queries[0].Author != "alice" {
		t.Fatalf("saved queries = %+v", queries)
	}
	out.Reset()
	if err := runListQueries(nil, &out); err != nil {
		t.Fatalf("runListQueries: %v", err)
	}
	if !strings.Contains(out.String(), "  author=alice\n") {
		t.Errorf("list output missing author: %s", out.String())
	}
}

func TestRunSaveRejectsAll(t *testing.T) {
	withTempQueriesFile(t)
	err := runSave(
		[]string{"cleanup", "--all"},
		strings.NewReader(""),
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "flag provided but not defined: -all") {
		t.Fatalf("runSave error = %v, want unsupported --all error", err)
	}
	if _, statErr := os.Stat(queriesFilePath()); !os.IsNotExist(statErr) {
		t.Errorf("rejected save created the saved queries file; stat error = %v", statErr)
	}
}

func TestRunSaveOverwritesLegacyAll(t *testing.T) {
	withTempQueriesFile(t)
	if err := saveQueries([]savedQuery{{Name: "legacy", All: true}}); err != nil {
		t.Fatalf("saveQueries: %v", err)
	}

	if err := runSave(
		[]string{"legacy", "--repo", "o/r", "--yes"},
		strings.NewReader(""),
		io.Discard,
	); err != nil {
		t.Fatalf("runSave: %v", err)
	}

	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("loadQueries: %v", err)
	}
	if len(queries) != 1 || queries[0].All || queries[0].Repo != "o/r" {
		t.Fatalf("updated query = %+v, want legacy all removed", queries)
	}
}

func TestRunSaveRejectsUnquotedMultiwordFilter(t *testing.T) {
	withTempQueriesFile(t)
	var out bytes.Buffer
	err := runSave(
		[]string{
			"no-increment-versions",
			"-f", "increment", "versions", "for",
			"--all", "--unsubscribe", "--tag", "cleanup",
		},
		strings.NewReader(""),
		&out,
	)
	if err == nil {
		t.Fatal("expected an unquoted multiword filter to be rejected")
	}
	for _, want := range []string{
		`unexpected arguments "versions for --all --unsubscribe --tag cleanup"`,
		`--filter "increment versions for"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
	if _, statErr := os.Stat(queriesFilePath()); !os.IsNotExist(statErr) {
		t.Errorf("rejected save created the saved queries file; stat error = %v", statErr)
	}
}

func TestRunSaveAcceptsQuotedMultiwordFilter(t *testing.T) {
	withTempQueriesFile(t)
	var out bytes.Buffer
	err := runSave(
		[]string{
			"no-increment-versions",
			"-f", "increment versions for",
			"--unsubscribe", "--tag", "cleanup",
		},
		strings.NewReader(""),
		&out,
	)
	if err != nil {
		t.Fatalf("runSave: %v", err)
	}

	queries, err := loadQueries()
	if err != nil {
		t.Fatalf("loadQueries: %v", err)
	}
	if len(queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(queries))
	}
	q := queries[0]
	if q.Filter != "increment versions for" || q.Scope != "" || q.All || q.Action != "unsubscribe" ||
		len(q.Tags) != 1 || q.Tags[0] != "cleanup" {
		t.Errorf("saved query = %+v", q)
	}
}

func TestRunListQueriesFormatsMultiline(t *testing.T) {
	withTempQueriesFile(t)
	queries := []savedQuery{
		{
			Name:   "no-increment-versions",
			Filter: "increment versions for",
			All:    true,
			Author: "dependabot",
			Action: "unsubscribe",
			Tags:   []string{"cleanup"},
		},
		{Name: "triage", Repo: "octo-org/docs", State: "open", Author: "alice", Scope: savedQueryScopeUnread},
	}
	if err := saveQueries(queries); err != nil {
		t.Fatalf("saveQueries: %v", err)
	}

	var out bytes.Buffer
	if err := runListQueries(nil, &out); err != nil {
		t.Fatalf("runListQueries: %v", err)
	}
	const want = `no-increment-versions
  all: true (unsupported; remove before running)
  filter="increment versions for"
  author=dependabot
  action=unsubscribe
  tags=cleanup

triage
  repo=octo-org/docs
  state=open
  author=alice
`
	if out.String() != want {
		t.Errorf("list output:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestRunSaveOverwriteConfirmed(t *testing.T) {
	withTempQueriesFile(t)
	if err := saveQueries([]savedQuery{{Name: "dup", Repo: "old/repo"}}); err != nil {
		t.Fatalf("saveQueries: %v", err)
	}
	var out bytes.Buffer
	if err := runSave([]string{"dup", "--repo", "new/repo"}, strings.NewReader("y\n"), &out); err != nil {
		t.Fatalf("runSave: %v", err)
	}
	if !strings.Contains(out.String(), "already exists") || !strings.Contains(out.String(), "Updated query") {
		t.Errorf("expected overwrite prompt and update, got %q", out.String())
	}
	queries, _ := loadQueries()
	if len(queries) != 1 || queries[0].Repo != "new/repo" {
		t.Errorf("expected query overwritten, got %+v", queries)
	}
}

func TestRunSaveOverwriteAborted(t *testing.T) {
	withTempQueriesFile(t)
	if err := saveQueries([]savedQuery{{Name: "dup", Repo: "old/repo"}}); err != nil {
		t.Fatalf("saveQueries: %v", err)
	}
	var out bytes.Buffer
	if err := runSave([]string{"dup", "--repo", "new/repo"}, strings.NewReader("n\n"), &out); err != nil {
		t.Fatalf("runSave: %v", err)
	}
	if !strings.Contains(out.String(), "Aborted") {
		t.Errorf("expected abort message, got %q", out.String())
	}
	queries, _ := loadQueries()
	if len(queries) != 1 || queries[0].Repo != "old/repo" {
		t.Errorf("expected query unchanged after abort, got %+v", queries)
	}
}

func TestRunSaveOverwriteAssumeYesSkipsPrompt(t *testing.T) {
	withTempQueriesFile(t)
	if err := saveQueries([]savedQuery{{Name: "dup", Repo: "old/repo"}}); err != nil {
		t.Fatalf("saveQueries: %v", err)
	}
	var out bytes.Buffer
	// Empty stdin proves no prompt is read when --yes is set.
	if err := runSave([]string{"dup", "--repo", "new/repo", "--yes"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("runSave: %v", err)
	}
	if strings.Contains(out.String(), "already exists") {
		t.Errorf("expected no overwrite prompt with --yes, got %q", out.String())
	}
	queries, _ := loadQueries()
	if queries[0].Repo != "new/repo" {
		t.Errorf("expected query overwritten, got %+v", queries)
	}
}

func TestRunListEmpty(t *testing.T) {
	withTempQueriesFile(t)
	var out bytes.Buffer
	if err := runListQueries(nil, &out); err != nil {
		t.Fatalf("runListQueries: %v", err)
	}
	if !strings.Contains(out.String(), "No saved queries") {
		t.Errorf("unexpected output %q", out.String())
	}
}

func TestRunDeleteQuery(t *testing.T) {
	withTempQueriesFile(t)
	if err := saveQueries([]savedQuery{{Name: "gone"}}); err != nil {
		t.Fatalf("saveQueries: %v", err)
	}
	var out bytes.Buffer
	if err := runDeleteQuery([]string{"gone"}, &out); err != nil {
		t.Fatalf("runDeleteQuery: %v", err)
	}
	if !strings.Contains(out.String(), "Deleted query \"gone\"") {
		t.Errorf("unexpected output %q", out.String())
	}
	queries, _ := loadQueries()
	if len(queries) != 0 {
		t.Errorf("expected query removed, got %+v", queries)
	}

	if err := runDeleteQuery([]string{"missing"}, &out); err == nil {
		t.Error("expected error deleting missing query")
	}
	if err := runDeleteQuery(nil, &out); err == nil {
		t.Error("expected error when name is missing")
	}
}

func TestParseRunArgs(t *testing.T) {
	name, rf, err := parseRunArgs([]string{"cleanup", "--dry-run", "--yes"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if name != "cleanup" || !rf.dryRun || !rf.assumeYes || rf.interactive {
		t.Errorf("unexpected parse: name=%q rf=%+v", name, rf)
	}

	name, rf, err = parseRunArgs([]string{"--tag", "cleanup", "--tag", "daily"})
	if err != nil {
		t.Fatalf("parseRunArgs tags: %v", err)
	}
	if name != "" || len(rf.tags) != 2 {
		t.Errorf("unexpected tags parse: name=%q rf=%+v", name, rf)
	}

	name, rf, err = parseRunArgs([]string{"q", "-i"})
	if err != nil {
		t.Fatalf("parseRunArgs interactive: %v", err)
	}
	if name != "q" || !rf.interactive {
		t.Errorf("unexpected interactive parse: name=%q rf=%+v", name, rf)
	}
}

func TestRunSavedQueryValidation(t *testing.T) {
	withTempQueriesFile(t)
	var out bytes.Buffer
	in := strings.NewReader("")

	if err := runSavedQuery(nil, in, &out); err == nil {
		t.Error("expected error when neither name nor tag given")
	}
	if err := runSavedQuery([]string{"name", "--tag", "x"}, in, &out); err == nil {
		t.Error("expected error combining name with tag")
	}
	if err := runSavedQuery([]string{"--tag", "x", "--interactive"}, in, &out); err == nil {
		t.Error("expected error combining --interactive with --tag")
	}
	if err := runSavedQuery([]string{"nonexistent"}, in, &out); err == nil {
		t.Error("expected error for unknown query name")
	}
}

func TestRunSavedQueryTagNoMatches(t *testing.T) {
	withTempQueriesFile(t)
	if err := saveQueries([]savedQuery{{Name: "a", Tags: []string{"other"}}}); err != nil {
		t.Fatalf("saveQueries: %v", err)
	}
	var out bytes.Buffer
	if err := runSavedQuery([]string{"--tag", "missing"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("runSavedQuery: %v", err)
	}
	if !strings.Contains(out.String(), "No saved queries match") {
		t.Errorf("unexpected output %q", out.String())
	}
}

func TestRunSavedQueryRejectsLegacyAll(t *testing.T) {
	for _, query := range []savedQuery{
		{Name: "legacy", All: true},
		{Name: "legacy", Scope: savedQueryScopeAll},
	} {
		t.Run(query.Scope+fmt.Sprint(query.All), func(t *testing.T) {
			withTempQueriesFile(t)
			if err := saveQueries([]savedQuery{query}); err != nil {
				t.Fatalf("saveQueries: %v", err)
			}

			err := runSavedQuery([]string{"legacy"}, strings.NewReader(""), io.Discard)
			if err == nil || !strings.Contains(err.Error(), `saved query "legacy" contains legacy`) {
				t.Fatalf("runSavedQuery error = %v, want legacy all error", err)
			}
		})
	}
}

func TestExecuteSavedQueryRejectsLegacyAll(t *testing.T) {
	err := executeSavedQuery(
		nil,
		savedQuery{Name: "cleanup", All: true},
		runFlags{},
		strings.NewReader(""),
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), `saved query "cleanup" contains legacy all: true`) {
		t.Fatalf("executeSavedQuery error = %v, want legacy all error", err)
	}
}

func TestExecuteSavedQueryAuthorFilters(t *testing.T) {
	boom := errors.New("boom")
	for _, tc := range []struct {
		name   string
		query  savedQuery
		flags  runFlags
		gqlErr error
		want   []string
	}{
		{"include bot", savedQuery{Author: "dependabot"}, runFlags{assumeYes: true}, nil, []string{"DELETE notifications/threads/1"}},
		{"bot suffix", savedQuery{Author: "dependabot[bot]"}, runFlags{assumeYes: true}, nil, []string{"DELETE notifications/threads/1"}},
		{"include human", savedQuery{Author: "alice"}, runFlags{assumeYes: true}, nil, []string{"DELETE notifications/threads/2"}},
		{"dry run", savedQuery{Author: "alice"}, runFlags{dryRun: true, assumeYes: true}, nil, nil},
		{"aborted", savedQuery{Author: "alice"}, runFlags{}, nil, nil},
		{"lookup error", savedQuery{Author: "alice"}, runFlags{assumeYes: true}, boom, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeGQL{
				items: map[int]itemDetail{
					1: {state: "open", author: "dependabot[bot]"},
					2: {state: "open", author: "Alice"},
				},
				err: tc.gqlErr,
			}
			withFakeGraphQLClient(t, fake, nil)
			doer := &recordingDoer{body: `[
				{"id":"1","subject":{"type":"PullRequest","url":"https://api.github.com/repos/o/r/pulls/1"}},
				{"id":"2","subject":{"type":"PullRequest","url":"https://api.github.com/repos/o/r/pulls/2"}},
				{"id":"3","subject":{"type":"Release"}}
			]`}
			q := tc.query
			q.Name, q.Type, q.Action = "triage", "pr", "done"
			var out bytes.Buffer
			err := executeSavedQuery(newRateLimitTracker(doer), q, tc.flags, strings.NewReader("n\n"), &out)
			if !errors.Is(err, tc.gqlErr) {
				t.Fatalf("executeSavedQuery error = %v, want %v", err, tc.gqlErr)
			}
			want := append([]string{"GET notifications?per_page=50"}, tc.want...)
			if !slices.Equal(doer.calls, want) {
				t.Errorf("REST calls = %v, want %v", doer.calls, want)
			}
			if fake.calls != 1 {
				t.Errorf("GraphQL calls = %d, want 1", fake.calls)
			}
		})
	}
}

func TestEditorCommand(t *testing.T) {
	cmd, err := editorCommand("code --wait", "/tmp/notifications.yml")
	if err != nil {
		t.Fatalf("editorCommand: %v", err)
	}
	// Args[0] is the program name; the rest are editor args plus the path.
	if len(cmd.Args) != 3 || cmd.Args[1] != "--wait" || cmd.Args[2] != "/tmp/notifications.yml" {
		t.Errorf("unexpected args: %v", cmd.Args)
	}

	if _, err := editorCommand("", "/tmp/x"); err == nil {
		t.Error("expected error for empty editor")
	}
	if _, err := editorCommand("   ", "/tmp/x"); err == nil {
		t.Error("expected error for whitespace-only editor")
	}
}

func TestRunEditQueriesOpensFile(t *testing.T) {
	withTempQueriesFile(t)
	t.Setenv("GH_EDITOR", "fake-editor")

	var gotPath string
	prev := editorRunner
	editorRunner = func(cmd *exec.Cmd) error {
		gotPath = cmd.Args[len(cmd.Args)-1]
		return nil
	}
	t.Cleanup(func() { editorRunner = prev })

	var out bytes.Buffer
	if err := runEditQueries(nil, &out); err != nil {
		t.Fatalf("runEditQueries: %v", err)
	}
	if gotPath != queriesFilePath() {
		t.Errorf("editor opened %q, want %q", gotPath, queriesFilePath())
	}
	// The file should have been created so the editor has something to open.
	if _, err := loadQueries(); err != nil {
		t.Errorf("queries file not readable after edit: %v", err)
	}
	if !strings.Contains(out.String(), "Opening") {
		t.Errorf("unexpected output %q", out.String())
	}
}
