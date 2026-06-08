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
- Displays a table with columns: `REPOSITORY`, `REASON`, `TITLE`, `AGE`
- `AGE` is a short relative time (`now`, `5m`, `3h`, `2d`) based on the thread's last update
- Ordered by most recently updated first (API default)
- Prints `No unread notifications` when there are none

#### Filtering by repository

```
gh notifications --repo OWNER/REPO
gh notifications -R OWNER/REPO
```

- Limits results to a single repository via `GET /repos/{owner}/{repo}/notifications`
- Repository must be in `OWNER/REPO` format; otherwise the command exits with an error
- Output and columns are identical to the unfiltered listing

<!-- Add new features below as they are specified -->
