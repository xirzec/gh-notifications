package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/term"
)

// run dispatches the command line to a saved-query subcommand or, when no known
// subcommand is given, to the default listing/filtering flow. The default flow
// is unchanged so existing invocations keep working.
func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "save":
			return runSave(args[1:], os.Stdin, os.Stdout)
		case "list":
			return runListQueries(args[1:], os.Stdout)
		case "run":
			return runSavedQuery(args[1:], os.Stdin, os.Stdout)
		case "delete":
			return runDeleteQuery(args[1:], os.Stdout)
		case "edit":
			return runEditQueries(args[1:], os.Stdout)
		}
	}
	opts, err := parseArgs(args)
	if err != nil {
		return err
	}
	return runNotifications(opts)
}

// extractName treats the first argument as a positional name when it does not
// look like a flag, returning it along with the remaining arguments.
func extractName(args []string) (name string, rest []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

// runSave persists the given filters (and optional action/tags) under a name.
// When a query with the same name already exists, it asks for confirmation
// before overwriting, unless --yes is given.
func runSave(args []string, in io.Reader, out io.Writer) error {
	name, rest := extractName(args)
	if name == "" {
		return fmt.Errorf("save requires a query name: gh notifications save <name> [flags]")
	}

	var opts options
	fs, tags := newFlagSet("gh-notifications save", &opts)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	opts.tags = *tags
	if err := validateOptions(opts); err != nil {
		return err
	}

	queries, err := loadQueries()
	if err != nil {
		return err
	}
	_, existed := findQuery(queries, name)
	if existed && !opts.assumeYes {
		ok, err := confirm(in, out, fmt.Sprintf("A query named %q already exists. Overwrite it?", name))
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "Aborted; the saved query was not changed")
			return nil
		}
	}
	queries = upsertQuery(queries, optionsToSavedQuery(name, opts))
	if err := saveQueries(queries); err != nil {
		return err
	}

	verb := "Saved"
	if existed {
		verb = "Updated"
	}
	fmt.Fprintf(out, "%s query %q\n", verb, name)
	return nil
}

// runListQueries prints all saved queries.
func runListQueries(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("gh-notifications list", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	queries, err := loadQueries()
	if err != nil {
		return err
	}
	if len(queries) == 0 {
		fmt.Fprintln(out, "No saved queries")
		return nil
	}
	for _, q := range queries {
		fmt.Fprintf(out, "%s\t%s", q.Name, q.describe())
		if len(q.Tags) > 0 {
			fmt.Fprintf(out, "  tags=%s", strings.Join(q.Tags, ","))
		}
		fmt.Fprintln(out)
	}
	return nil
}

// runDeleteQuery removes a saved query by name.
func runDeleteQuery(args []string, out io.Writer) error {
	name, rest := extractName(args)
	if name == "" {
		return fmt.Errorf("delete requires a query name: gh notifications delete <name>")
	}
	fs := flag.NewFlagSet("gh-notifications delete", flag.ContinueOnError)
	if err := fs.Parse(rest); err != nil {
		return err
	}

	queries, err := loadQueries()
	if err != nil {
		return err
	}
	queries, removed := deleteQueryByName(queries, name)
	if !removed {
		return fmt.Errorf("no saved query named %q", name)
	}
	if err := saveQueries(queries); err != nil {
		return err
	}
	fmt.Fprintf(out, "Deleted query %q\n", name)
	return nil
}

// editorRunner runs an editor command; it is a variable so tests can substitute
// a fake instead of launching a real editor.
var editorRunner = func(cmd *exec.Cmd) error {
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runEditQueries opens the saved-queries file in the user's editor, creating an
// empty file first if none exists yet.
func runEditQueries(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("gh-notifications edit", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Ensure the file exists so the editor has something to open. loadQueries
	// tolerates a missing file; re-saving the (possibly empty) set creates it.
	queries, err := loadQueries()
	if err != nil {
		return err
	}
	path := queriesFilePath()
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if err := saveQueries(queries); err != nil {
			return err
		}
	}

	cmd, err := editorCommand(resolveEditor(), path)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Opening %s\n", path)
	if err := editorRunner(cmd); err != nil {
		return fmt.Errorf("running editor: %w", err)
	}
	return nil
}

// runFlags holds the per-invocation flags accepted by the `run` subcommand.
// These override the saved query's behavior but not its filters.
type runFlags struct {
	dryRun      bool
	assumeYes   bool
	interactive bool
	tags        []string
}

// parseRunArgs parses the `run` subcommand arguments into an optional query name
// and the run-level flags.
func parseRunArgs(args []string) (string, runFlags, error) {
	name, rest := extractName(args)
	var rf runFlags
	fs := flag.NewFlagSet("gh-notifications run", flag.ContinueOnError)
	fs.BoolVar(&rf.dryRun, "dry-run", false, "Show what would happen without calling the API")
	fs.BoolVar(&rf.assumeYes, "yes", false, "Skip the confirmation prompt for mutating actions")
	fs.BoolVar(&rf.assumeYes, "y", false, "Skip the confirmation prompt for mutating actions (shorthand)")
	fs.BoolVar(&rf.interactive, "interactive", false, "Open the saved query's results in the interactive picker")
	fs.BoolVar(&rf.interactive, "i", false, "Open the saved query's results in the interactive picker (shorthand)")
	var tags stringSliceFlag
	fs.Var(&tags, "tag", "Run every saved query carrying this tag (repeatable)")
	if err := fs.Parse(rest); err != nil {
		return "", runFlags{}, err
	}
	rf.tags = tags
	return name, rf, nil
}

// runSavedQuery executes a saved query by name, or every query matching the
// given tag(s).
func runSavedQuery(args []string, in io.Reader, out io.Writer) error {
	name, rf, err := parseRunArgs(args)
	if err != nil {
		return err
	}

	switch {
	case name == "" && len(rf.tags) == 0:
		return fmt.Errorf("run requires a query name or --tag: gh notifications run <name> | --tag TAG")
	case name != "" && len(rf.tags) > 0:
		return fmt.Errorf("cannot combine a query name with --tag")
	case rf.interactive && len(rf.tags) > 0:
		return fmt.Errorf("--interactive cannot be combined with --tag")
	}

	queries, err := loadQueries()
	if err != nil {
		return err
	}

	var targets []savedQuery
	if name != "" {
		q, ok := findQuery(queries, name)
		if !ok {
			return fmt.Errorf("no saved query named %q", name)
		}
		targets = []savedQuery{q}
	} else {
		seen := make(map[string]bool)
		for _, tag := range rf.tags {
			for _, q := range queriesByTag(queries, tag) {
				if !seen[q.Name] {
					seen[q.Name] = true
					targets = append(targets, q)
				}
			}
		}
		if len(targets) == 0 {
			fmt.Fprintf(out, "No saved queries match tag(s) %s\n", strings.Join(rf.tags, ", "))
			return nil
		}
	}

	client, err := api.DefaultRESTClient()
	if err != nil {
		return err
	}
	tracker := newRateLimitTracker(client)
	defer tracker.report(os.Stderr)

	for _, q := range targets {
		if len(targets) > 1 {
			fmt.Fprintf(out, "==> %s\n", q.Name)
		}
		if err := executeSavedQuery(tracker, q, rf, in, out); err != nil {
			return fmt.Errorf("running query %q: %w", q.Name, err)
		}
	}
	return nil
}

// executeSavedQuery fetches and filters a saved query's notifications, then runs
// its action, opens the picker (when --interactive), or prints the table.
func executeSavedQuery(tracker *rateLimitTracker, q savedQuery, rf runFlags, in io.Reader, out io.Writer) error {
	opts := q.toOptions()
	notifications, err := loadFilteredNotifications(tracker, opts)
	if err != nil {
		return err
	}

	// --interactive takes precedence over any saved action: the user drives the
	// triage in the picker rather than auto-applying the action.
	if rf.interactive {
		return selectAndOpen(tracker, notifications)
	}

	switch q.Action {
	case "read":
		return runMarkRead(tracker, notifications, rf.dryRun, rf.assumeYes, in, out)
	case "done":
		return runMarkDone(tracker, notifications, rf.dryRun, rf.assumeYes, in, out)
	case "unsubscribe":
		return runUnsubscribe(tracker, notifications, rf.dryRun, rf.assumeYes, in, out)
	default:
		terminal := term.FromEnv()
		width, _, _ := terminal.Size()
		return renderNotifications(terminal.Out(), notifications, terminal.IsTerminalOutput(), width, opts.showReason)
	}
}
