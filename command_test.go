package main

import (
	"bytes"
	"os/exec"
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
	if q.Repo != "o/r" || q.Type != "pr" || q.Action != "unsubscribe" || len(q.Tags) != 1 || q.Tags[0] != "cleanup" {
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
