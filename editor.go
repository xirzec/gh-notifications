package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/cli/go-gh/v2/pkg/config"
	"github.com/google/shlex"
)

// resolveEditor determines the editor command to use, following the same
// precedence as the `gh` CLI:
//   - GH_EDITOR environment variable;
//   - editor option from the gh configuration file;
//   - git's core.editor;
//   - VISUAL environment variable;
//   - EDITOR environment variable;
//   - a platform default (notepad on Windows, vi elsewhere).
func resolveEditor() string {
	if e := os.Getenv("GH_EDITOR"); e != "" {
		return e
	}
	if cfg, err := config.Read(nil); err == nil {
		if e, _ := cfg.Get([]string{"editor"}); e != "" {
			return e
		}
	}
	if e := gitCoreEditor(); e != "" {
		return e
	}
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

// gitCoreEditor returns git's configured core.editor, or "" if git is not
// available or no editor is configured.
func gitCoreEditor() string {
	out, err := exec.Command("git", "config", "core.editor").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// editorCommand builds the command that opens path in the given editor. The
// editor string may include arguments (e.g. `code --wait`), which are split
// shell-style. An empty editor is an error.
func editorCommand(editor, path string) (*exec.Cmd, error) {
	editor = strings.TrimSpace(editor)
	if editor == "" {
		return nil, fmt.Errorf("no editor configured; set $GH_EDITOR or $EDITOR")
	}
	parts, err := shlex.Split(editor)
	if err != nil || len(parts) == 0 {
		return nil, fmt.Errorf("invalid editor command %q", editor)
	}
	args := append(parts[1:], path)
	return exec.Command(parts[0], args...), nil
}
