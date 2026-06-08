# gh-notifications Spec

A GitHub CLI extension for managing GitHub notifications from the terminal.

## Overview

`gh notifications` provides a streamlined interface to view, triage, and act on GitHub notifications without leaving the terminal.

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

### Open a Notification in the Browser

Interactively pick a notification and open it in the default web browser.

```
gh notifications --interactive
gh notifications -i
```

- Presents a full-screen, scrollable list (Bubble Tea) of the fetched notifications
- Each entry shows the title with `OWNER/REPO  [reason]  <age> ago` beneath it
- Navigation: arrow keys or `j`/`k` to move, `/` to filter, `enter` to open the highlighted entry
- After opening a notification, returns to the list so several can be opened in one session
- Exit with `q` or `Ctrl+C`
- Opens the selected thread's web page, resolved from the subject's `html_url`
- Falls back to the repository page when the subject has no web URL (e.g. discussions or security alerts)
- Combines with `--repo` to pick from a single repository's notifications
- Uses the browser configured via `GH_BROWSER`, the `gh` config, or `BROWSER`

<!-- Add new features below as they are specified -->
