package main

import (
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
)

func addHelpFlags(fs *flag.FlagSet, help *bool) {
	fs.BoolVar(help, "help", false, "Show help for this command")
	fs.BoolVar(help, "h", false, "Show help for this command (shorthand)")
}

func setRootUsage(fs *flag.FlagSet) {
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintln(out, "Usage:")
		fmt.Fprintln(out, "  gh notifications [flags]")
		fmt.Fprintln(out, "  gh notifications <command> [args]")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Saved query commands:")
		fmt.Fprintln(out, "  save <name> [flags]       Save filters and an optional action as a named query")
		fmt.Fprintln(out, "  list                      List saved queries")
		fmt.Fprintln(out, "  run <name> [flags]        Run a saved query by name")
		fmt.Fprintln(out, "  run --tag <tag> [flags]   Run saved queries matching a tag")
		fmt.Fprintln(out, "  delete <name>             Delete a saved query")
		fmt.Fprintln(out, "  edit                      Edit the saved queries file")
		fmt.Fprintln(out)
		fmt.Fprintln(out, `Run "gh notifications help <command>" for details about a saved query command.`)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Flags:")
		printFlagDefaults(fs)
	}
}

func setCommandUsage(fs *flag.FlagSet, usageLines []string, description string) {
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintln(out, "Usage:")
		for _, line := range usageLines {
			fmt.Fprintf(out, "  %s\n", line)
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, description)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Flags:")
		printFlagDefaults(fs)
	}
}

func printFlagDefaults(fs *flag.FlagSet) {
	w := tabwriter.NewWriter(fs.Output(), 0, 4, 2, ' ', 0)
	fs.VisitAll(func(f *flag.Flag) {
		prefix := "--"
		if len(f.Name) == 1 {
			prefix = "-"
		}
		valueName, usage := flag.UnquoteUsage(f)
		fmt.Fprintf(w, "  %s%s", prefix, f.Name)
		if valueName != "" {
			fmt.Fprintf(w, " %s", valueName)
		}
		fmt.Fprintf(w, "\t%s\n", usage)
	})
	_ = w.Flush()
}

func setSaveUsage(fs *flag.FlagSet) {
	setCommandUsage(
		fs,
		[]string{"gh notifications save <name> [flags]"},
		"Save a reusable set of notification filters, an optional action, and tags.",
	)
}

func setListUsage(fs *flag.FlagSet) {
	setCommandUsage(
		fs,
		[]string{"gh notifications list"},
		"List saved queries and summarize their filters, actions, and tags.",
	)
}

func setRunUsage(fs *flag.FlagSet) {
	setCommandUsage(
		fs,
		[]string{
			"gh notifications run <name> [flags]",
			"gh notifications run --tag <tag> [flags]",
		},
		"Run a saved query by name, or run every saved query matching a tag.",
	)
}

func setDeleteUsage(fs *flag.FlagSet) {
	setCommandUsage(
		fs,
		[]string{"gh notifications delete <name>"},
		"Delete a saved query.",
	)
}

func setEditUsage(fs *flag.FlagSet) {
	setCommandUsage(
		fs,
		[]string{"gh notifications edit"},
		"Open the saved queries file in your configured editor.",
	)
}

func showFlagSetUsage(fs *flag.FlagSet, out io.Writer) {
	fs.SetOutput(out)
	fs.Usage()
}

func showRootUsage(out io.Writer) {
	var opts options
	fs := newFlagSet("gh-notifications", &opts)
	setRootUsage(fs)
	showFlagSetUsage(fs, out)
}

func showRunUsage(out io.Writer) {
	var rf runFlags
	fs, _ := newRunFlagSet(&rf)
	showFlagSetUsage(fs, out)
}

func runHelp(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "-h" || args[0] == "--help")) {
		showRootUsage(out)
		return nil
	}
	if len(args) != 1 {
		return fmt.Errorf("help accepts at most one command")
	}

	switch args[0] {
	case "save":
		return runSave([]string{"--help"}, in, out)
	case "list":
		return runListQueries([]string{"--help"}, out)
	case "run":
		return runSavedQuery([]string{"--help"}, in, out)
	case "delete":
		return runDeleteQuery([]string{"--help"}, out)
	case "edit":
		return runEditQueries([]string{"--help"}, out)
	default:
		return fmt.Errorf("unknown help topic %q", args[0])
	}
}
