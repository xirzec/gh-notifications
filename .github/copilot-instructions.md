# Copilot Instructions

## Project Overview

This is a GitHub CLI (`gh`) extension written in Go. It extends the `gh` command with `gh notifications` functionality. It uses the [go-gh](https://github.com/cli/go-gh) library for GitHub API access and CLI integration.

## Build, Test, and Lint

```bash
make build        # compile the binary
make test         # run all tests (go test ./...)
make lint         # vet the code (go vet ./...)
go test -run TestName ./...  # run a single test
gh notifications  # run as an installed gh extension
```

## Spec

`SPEC.md` is the source of truth for intended behavior and features. Consult it before implementing new functionality, and update it when adding or changing features.

## Architecture

- Single-binary CLI extension following the `gh` extension pattern
- Uses `github.com/cli/go-gh/v2` for authenticated REST/GraphQL API clients
- Released via `cli/gh-extension-precompile` GitHub Action on version tags (`v*`)

## Testing against the live API

The extension runs against the signed-in user's **real** GitHub notifications. To avoid
accidentally destroying notification state, follow these rules:

- Only run the built binary against the live API for **read-only** commands (listing and
  filtering). These never mutate server state.
- **Never** run mutating commands (e.g. mark-as-read, mark-as-done, unsubscribe) against the
  live API during development. Marking notifications read/done is effectively irreversible and
  can lose the working set used for testing.
- Test mutating behavior with unit tests and fake clients via the `requestDoer` and
  `graphQLDoer` interfaces — do not exercise it with a live `api.DefaultRESTClient()`.
- If a mutating command must be tried end-to-end, use a throwaway test account, not the
  developer's primary account.

## Conventions

- Extension binary is named `gh-notifications.exe` (matches repo name without `gh-` prefix for `gh` dispatch)
- Use `api.DefaultRESTClient()` for authenticated GitHub API calls (inherits `gh` auth)
- API access is abstracted behind the `requestDoer` (REST) and `graphQLDoer` (GraphQL)
  interfaces so logic can be unit tested with fakes instead of live calls
