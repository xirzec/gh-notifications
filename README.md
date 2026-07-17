# gh-notifications

A [GitHub CLI](https://cli.github.com/) extension to view, filter, and triage your GitHub notifications without leaving the terminal.

List your notifications as a table, narrow them down with rich filters, then act on them — open in the browser, mark as read/done, or unsubscribe — either in bulk from the command line or via an interactive picker.

## Installation

```bash
gh extension install xirzec/gh-notifications
```

To upgrade later:

```bash
gh extension upgrade notifications
```

### Build from source

```bash
git clone https://github.com/xirzec/gh-notifications.git
cd gh-notifications
make build
gh extension install .
```

## Usage

```bash
gh notifications [flags]
gh notifications <command> [args]
```

Run `gh notifications --help` to see all flags and saved-query commands. Run with no flags to
print your unread notifications:

```bash
gh notifications
```

```
REPOSITORY              TYPE   TITLE                                   AGE
octocat/hello-world     PR     Fix the flaky integration test          5m
octocat/hello-world     Issue  Crash when config file is missing       3h
octo-org/docs           PR     Update the contributing guide           2d
```

The table columns are `REPOSITORY`, `TYPE`, `TITLE`, and `AGE` (a short relative time). `PullRequest` is shown as `PR`. Results page through everything (the API caps each page at 50). On exit, the remaining GitHub API quota is printed to stderr.

## Flags

| Flag | Description |
| --- | --- |
| `-a`, `--all` | Include notifications already marked as read (default: unread only) |
| `-R`, `--repo OWNER/REPO` | Limit to a single repository |
| `-f`, `--filter TEXT` | Keep only notifications whose title contains `TEXT` (case-insensitive) |
| `-t`, `--type TYPE` | Keep only a subject type: `issue`, `pr`, `commit`, `release`, `discussion`, … (friendly aliases accepted) |
| `--state STATE` | Keep only issues/PRs in a state: `open`, `closed`, `merged`, `not-planned`, `completed`. `closed` and `merged` are distinct: `closed` excludes merged PRs — use `merged` for those |
| `--draft` | Keep only draft pull requests |
| `--show-reason` | Include the `REASON` column in the table |
| `-i`, `--interactive` | Open the interactive picker (see below) |
| `--mark-read` | Mark the matching notifications as read |
| `--mark-done` | Mark the matching notifications as done (removes them from the inbox) |
| `--unsubscribe` | Unsubscribe from the matching threads (also marks them done) |
| `--dry-run` | Show what a mutating command would do without calling the API |
| `-y`, `--yes` | Skip the confirmation prompt for mutating commands (for unattended runs) |
| `-h`, `--help` | Show command help |

Filters compose, so you can combine them freely.

## Examples

```bash
# Unread PRs in one repo
gh notifications --repo cli/cli --type pr

# Open issues mentioning "auth"
gh notifications --type issue --state open --filter auth

# Draft PRs across everything
gh notifications --draft

# Issues that were closed as "not planned"
gh notifications --type issue --state not-planned

# Closed PRs that were NOT merged (merged PRs are excluded by --state closed)
gh notifications --type pr --state closed

# Preview what marking merged PRs as done would do (no changes made)
gh notifications --type pr --state merged --mark-done --dry-run

# Actually mark them done (asks for confirmation first)
gh notifications --type pr --state merged --mark-done
```

## Triaging notifications

All mutating commands operate on the notifications left after your filters are applied, list what
they will affect, and ask for a `[y/N]` confirmation before changing anything. Pair any of them
with `--dry-run` to preview without making API calls.

- **`--mark-read`** — `PATCH /notifications/threads/{id}`; clears the unread flag but keeps the
  thread in your inbox.
- **`--mark-done`** — `DELETE /notifications/threads/{id}`; removes the thread from your inbox.
- **`--unsubscribe`** — deletes the thread subscription and marks it done, matching the GitHub web
  "Unsubscribe" behavior.

Only one mutating action may be used at a time.

## Saved queries

Save a set of filters (and an optional action and tags) under a name, then re-run it later —
interactively or unattended. Handy for recurring chores like "always unsubscribe from PRs in this
repo".

```bash
# Save a reusable query (filters + an optional action + tags)
gh notifications save cleanup-bot-prs --repo octo-org/noisy --type pr --unsubscribe --tag cleanup

# List saved queries
gh notifications list

# Run one by name (applies its filters, then its action)
gh notifications run cleanup-bot-prs

# Run it, but drop into the interactive picker instead of auto-applying the action
gh notifications run cleanup-bot-prs --interactive

# Preview without changing anything, then run unattended (no confirmation prompt)
gh notifications run cleanup-bot-prs --dry-run
gh notifications run cleanup-bot-prs --yes

# Run every saved query carrying a tag (great for a single scheduled job)
gh notifications run --tag cleanup --yes

# Delete a saved query
gh notifications delete cleanup-bot-prs

# Open the saved-queries file in your editor
gh notifications edit
```

Notes:

- Queries are stored as YAML in your `gh` config directory (`notifications.yml`): on Windows
  `%AppData%\GitHub CLI\`, on macOS/Linux `~/.config/gh/`. The file is human-editable — use
  `gh notifications edit` to open it in your editor (resolved via `GH_EDITOR`/`EDITOR`, the `gh`
  `editor` config, or git's `core.editor`).
- Saving over an existing name asks for confirmation first; pass `--yes` to overwrite without
  prompting.
- `run --interactive` always opens the picker (it takes precedence over any saved action), so you
  can review before triaging by hand.
- `--yes` enables unattended mutation — validate with `--dry-run` first, and prefer a throwaway
  account for automated/scheduled runs. Scheduling itself (cron, Task Scheduler) is left to you.

Only one mutating action may be saved per query.

## Interactive mode

```bash
gh notifications --interactive   # or -i
```

A full-screen, scrollable list of your notifications. Keys:

| Key | Action |
| --- | --- |
| `↑`/`↓` or `j`/`k` | Move |
| `/` | Filter by text |
| `enter` | Open the highlighted notification in your browser |
| `r` / `d` / `u` | Mark the selected item read / done / unsubscribe (with confirmation) |
| `R` / `D` / `U` | Same actions applied to **all currently visible** items |
| `f` | Focus the list on the selected item's repository |
| `esc` | Clear the repository focus (back to all) |
| `q` or `Ctrl+C` | Quit |

A typical workflow: start with everything, press `f` to drill into a noisy repository, open and
bulk-action its notifications, then `esc` to pop back out and move on to the next.

The browser is chosen via `GH_BROWSER`, your `gh` config, or `BROWSER`.

## Development

```bash
make build        # compile the binary
make test         # run all tests
make fmt          # format the code (gofmt -w .)
make lint         # check formatting + go vet
go test -run TestName ./...   # run a single test
```

Line endings are normalized to LF via `.gitattributes`. Run `make fmt` before committing —
`make lint` fails if anything is not gofmt-clean.

### Building on Windows

The build commands use [GNU Make](https://www.gnu.org/software/make/). Install it with:

```powershell
winget install GnuWin32.Make
```

GnuWin32 does **not** update your `PATH` automatically — add its `bin` directory so `make` is
found:

```
C:\Program Files (x86)\GnuWin32\bin\
```

(Add it via *System Properties → Environment Variables*, or for the current session:
`$env:PATH += ';C:\Program Files (x86)\GnuWin32\bin\'`.) Then `make build`, `make test`, etc. work
in PowerShell as shown above.

`SPEC.md` is the source of truth for intended behavior — consult and update it when changing
features.

## License

[MIT](LICENSE) © xirzec
