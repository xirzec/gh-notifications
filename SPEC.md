# gh-notifications Spec

A GitHub CLI extension for managing GitHub notifications from the terminal.

## Overview

`gh notifications` provides a streamlined interface to view, triage, and act on GitHub notifications without leaving the terminal.

### API Quota Reporting

On exit, the program prints the remaining GitHub API quota to stderr, based on the most recent
request of each kind:

- **REST** quota comes from the rate-limit headers (`X-RateLimit-Remaining`, `X-RateLimit-Limit`,
  `X-RateLimit-Reset`) of the last REST request
- **GraphQL** quota comes from a `rateLimit { limit remaining resetAt }` field added to the
  batched state query, and is only reported when `--state` triggers a GraphQL call (REST and
  GraphQL have separate quotas)

```
GitHub REST API quota: 4593/5000 remaining, resets at 16:43:22
GitHub GraphQL API quota: 4998/5000 remaining, resets at 16:41:55
```

- Written to stderr so it never interferes with piped/redirected stdout
- Each line is omitted if no request of that kind was made

## Features

### List Notifications

Display the user's unread notifications.

```
gh notifications
```

- Shows unread notifications for the authenticated user (`GET /notifications`)
- Displays a table with columns: `REPOSITORY`, `TYPE`, `TITLE`, `AGE`
- `TYPE` is the subject type (`PullRequest` is shown as `PR`)
- The `REASON` column is hidden by default; pass `--show-reason` to include it
- `AGE` is a short relative time (`now`, `5m`, `3h`, `2d`) based on the thread's last update
- Ordered by most recently updated first (API default)
- Automatically pages through all results (API returns at most 50 per page) so the full list is shown
- Prints `No unread notifications` when there are none

#### Including read notifications

```
gh notifications --all
gh notifications -a
```

- By default only unread notifications are shown; `--all` includes ones already marked as read (adds `all=true` to the request)
- Composable with `--repo` and the other filters

#### Filtering by repository

```
gh notifications --repo OWNER/REPO
gh notifications -R OWNER/REPO
```

- Limits results to a single repository via `GET /repos/{owner}/{repo}/notifications`
- Repository must be in `OWNER/REPO` format; otherwise the command exits with an error
- Output and columns are identical to the unfiltered listing

#### Filtering by title

```
gh notifications --filter TEXT
gh notifications -f TEXT
```

- Keeps only notifications whose title contains `TEXT`, matched case-insensitively
- Applied after fetching, so it works together with `--repo` and `--interactive`
- Prints `No unread notifications` when nothing matches

#### Filtering by type

```
gh notifications --type pr
gh notifications -t issue
```

- Keeps only notifications whose subject type matches, e.g. `Issue`, `PullRequest`, `Commit`, `Release`, `Discussion`
- Accepts friendly aliases (case-insensitive): `pr`/`pull`/`pull-request` → `PullRequest`, `issue` → `Issue`; other values match the API type directly
- Composable with `--repo`, `--filter`, and `--interactive`

#### Filtering by state

```
gh notifications --state open
gh notifications --state merged
gh notifications --state not-planned
```

- Keeps only issues/PRs in the given state: `open`, `closed`, `merged`, `not-planned`, or `completed`
- `merged` and `closed` are **distinct, non-overlapping** states: a merged pull request reports its state as `merged`, not `closed`. This means `--state closed` matches only closed-unmerged items (closed issues and closed-without-merging PRs) and **does not include merged PRs** — use `--state merged` for those. This differs from the REST `state` field and parts of GitHub's UI, which treat merged as a kind of closed; the split is intentional so the two can be filtered separately.
- `not-planned` and `completed` further require the issue's close reason (`stateReason`), so they only match closed issues
- Notifications without an issue/PR state (commits, releases, discussions, etc.) are excluded when this filter is active
- States are fetched with batched GraphQL queries (up to 50 items per request) rather than one REST call per item, keeping the lookup fast and rate-limit friendly

#### Filtering by draft status

```
gh notifications --draft
gh notifications --repo OWNER/REPO --draft
```

- Keeps only draft pull requests
- Issues and non-draft PRs are excluded
- Uses the same batched GraphQL lookup as `--state` (the `isDraft` field); when both `--draft` and `--state` are given, they share a single set of GraphQL requests

### Open a Notification in the Browser

Interactively pick a notification and open it in the default web browser.

```
gh notifications --interactive
gh notifications -i
```

- Presents a full-screen, scrollable list (Bubble Tea) of the fetched notifications
- Each entry shows the title with `OWNER/REPO  [reason]  <age> ago` beneath it
- Navigation: arrow keys or `j`/`k` to move, `/` to filter, `enter` to open the highlighted entry
- Press `r` to mark the highlighted notification as read; an in-list `y/N` confirmation is shown first, and on success the entry is removed from the list
- Press `d` to mark the highlighted notification as done (removing it from the inbox); same `y/N` confirmation and removal behavior
- Press `u` to unsubscribe from the highlighted notification thread (also marks it done); same `y/N` confirmation and removal behavior
- Bulk actions apply to **all currently visible items** (respecting an active `/` filter): `R` (read), `D` (done), `U` (unsubscribe). A single `y/N` confirmation shows the count, and each successfully processed item is removed from the list
- Bulk actions report a single aggregate status once every item has been processed, e.g. `Marked 8/10 as done (2 failed)`. Successful items are removed from the list while failed ones remain, so the leftover list is exactly the set that still needs attention and can be retried. (Single-item `r`/`d`/`u` actions still report per item.)
- Press `f` to focus the list on the selected item's repository — the list is scoped to just that `OWNER/REPO`, the title shows the repo, and bulk actions then apply only within it. Press `esc` to clear the focus and return to all notifications. This lets you start from everything, drill into one repo to open/triage/bulk-action it, then pop back to the others
- After opening a notification, returns to the list so several can be opened in one session
- Exit with `q` or `Ctrl+C`
- Opens the selected thread's web page, resolved from the subject's `html_url`
- Falls back to the repository page when the subject has no web URL (e.g. discussions or security alerts)
- Combines with `--repo` to pick from a single repository's notifications
- Uses the browser configured via `GH_BROWSER`, the `gh` config, or `BROWSER`

### Mark Notifications as Read

Mark the matching notifications as read.

```
gh notifications --mark-read
gh notifications --repo OWNER/REPO --state open --mark-read
gh notifications --mark-read --dry-run
```

- Operates on the notifications left after all filters (`--repo`, `--filter`, `--type`, `--state`) are applied
- Marks each thread as read via `PATCH /notifications/threads/{thread_id}`
- Lists the affected notifications and asks for an interactive `[y/N]` confirmation before making any change; anything other than `y`/`yes` aborts without calling the API
- `--dry-run` reports exactly what would be marked and makes **no** API calls — use it to safely test filters before mutating
- Prints `No notifications to mark as read` when nothing matches

> Safety: marking notifications read changes real server state. During development, only run
> mutating commands with `--dry-run`, or against a throwaway test account. See
> `.github/copilot-instructions.md`.

### Mark Notifications as Done

Mark the matching notifications as done, removing them from the inbox.

```
gh notifications --mark-done
gh notifications --repo OWNER/REPO --state merged --mark-done
gh notifications --mark-done --dry-run
```

- Operates on the notifications left after all filters are applied
- Marks each thread as done via `DELETE /notifications/threads/{thread_id}`, removing it from the inbox entirely (unlike `--mark-read`, which only clears the unread flag)
- Same confirmation and `--dry-run` safety behavior as `--mark-read`
- `--mark-read` and `--mark-done` cannot be combined

### Unsubscribe from Notifications

Unsubscribe from the matching notification threads (and clear them from the inbox).

```
gh notifications --unsubscribe
gh notifications --repo OWNER/REPO --state closed --unsubscribe
gh notifications --unsubscribe --dry-run
```

- Operates on the notifications left after all filters are applied
- For each thread, deletes the subscription via `DELETE /notifications/threads/{thread_id}/subscription` and then marks it done via `DELETE /notifications/threads/{thread_id}` — matching the GitHub web "Unsubscribe" action, which also removes the thread from the inbox
- Same confirmation and `--dry-run` safety behavior as the mark commands
- Only one of `--mark-read`, `--mark-done`, or `--unsubscribe` may be used at a time

### Saved Queries

Persist a named set of filters (and an optional action and tags) so they can be re-run later,
interactively or unattended.

A saved query is **unified**: it stores the same filters as the listing command plus an optional
mutating action (`read`, `done`, or `unsubscribe`) and any number of free-form tags. Queries are
managed with subcommands; the bare `gh notifications [flags]` listing behavior is unchanged.

#### Storage

- Queries are stored as YAML in the `gh` CLI config directory (`config.ConfigDir()`), in a file
  named `notifications.yml` — separate from `gh`'s own `config.yml`/`hosts.yml`
  - Windows: `%AppData%\GitHub CLI\notifications.yml`
  - macOS/Linux: `~/.config/gh/notifications.yml` (honoring `GH_CONFIG_DIR`/`XDG_CONFIG_HOME`)
- The file is human-editable; schema:

```yaml
queries:
  - name: cleanup-bot-prs
    repo: owner/repo
    type: pr
    action: unsubscribe   # "" | read | done | unsubscribe
    tags: [cleanup]
```

#### Saving a query

```
gh notifications save <name> [--repo R] [--filter T] [--type T] [--state S] \
    [--draft] [--all] [--mark-read|--mark-done|--unsubscribe] [--tag t1 --tag t2]
```

- Captures the given filter flags and an optional action under `<name>`
- `<name>` must be the first argument (`save <name> [flags]`)
- `--tag` is repeatable; tags let related queries be run together (see below)
- Saving over an existing name asks for a `[y/N]` confirmation before overwriting (reports
  `Updated query` instead of `Saved query`); pass `--yes`/`-y` to skip the prompt

#### Listing saved queries

```
gh notifications list
```

- Prints each saved query's name, a summary of its filters/action, and its tags
- Prints `No saved queries` when none are stored

#### Running a saved query

```
gh notifications run <name> [--dry-run] [--yes] [--interactive]
gh notifications run --tag cleanup [--dry-run] [--yes]
```

- Applies the saved query's filters (reusing the same fetch + batched GraphQL pipeline as the
  listing command), then:
  - With `--interactive`/`-i`: opens the interactive picker pre-loaded with the results. This
    **takes precedence over any saved action** — the user triages by hand (open/`r`/`d`/`u`/bulk)
    rather than the action being auto-applied. This is the "run a saved query, then drop into
    interactive mode" flow
  - Else if the query has an action: runs it, asking for the usual `[y/N]` confirmation unless
    `--yes`/`-y` is given
  - Else (no action): prints the table, like the default listing
- `--tag NAME` runs **every** saved query carrying that tag (repeatable; queries are de-duplicated
  by name). `--dry-run`/`--yes` apply to the whole batch; `--interactive` cannot be combined with
  `--tag`. Each query prints a `==> <name>` header
- `--dry-run` makes **no** API calls; `--yes` skips the confirmation for unattended runs

> Safety: `--yes` enables unattended mutation. Validate a saved action with `--dry-run` before
> running it with `--yes`, and prefer a throwaway account for automated runs. See
> `.github/copilot-instructions.md`.

#### Deleting a saved query

```
gh notifications delete <name>
```

- Removes the named query; errors if no such query exists

#### Editing the queries file

```
gh notifications edit
```

- Opens the saved-queries YAML file in your editor (creating it first if it does not yet exist)
- The editor is resolved the same way as `gh`: `GH_EDITOR`, the `editor` config option, git's
  `core.editor`, `VISUAL`, `EDITOR`, then a platform default (Notepad on Windows, `vi` elsewhere)

<!-- Add new features below as they are specified -->

