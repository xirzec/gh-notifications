# Copilot Instructions

## Project Overview

This is a GitHub CLI (`gh`) extension written in Go. It extends the `gh` command with `gh notifications` functionality. It uses the [go-gh](https://github.com/cli/go-gh) library for GitHub API access and CLI integration.

## Build, Test, and Lint

```bash
make build        # compile the binary
make test         # run all tests (go test ./...)
make fmt          # format the code (gofmt -w .)
make lint         # check formatting (gofmt -l .) and vet the code (go vet ./...)
go test -run TestName ./...  # run a single test
gh notifications  # run as an installed gh extension
```

Run `make fmt` before committing — `make lint` fails if any file is not gofmt-clean.

## Spec

`SPEC.md` is the source of truth for intended behavior and features. Consult it before implementing new functionality, and update it when adding or changing features.

## Architecture

- Single-binary CLI extension following the `gh` extension pattern
- Uses `github.com/cli/go-gh/v2` for authenticated REST/GraphQL API clients
- Released via `cli/gh-extension-precompile` GitHub Action on version tags (`v*`)

## Code layout

- `notifications.go` — `options`/`parseArgs`, notification fetch with Link-header pagination,
  the filter functions, table rendering, and `runNotifications` (the top-level flow)
- `mutate.go` — write actions: `confirm`, the `threadActions` table, and the `runMark*` flows
- `picker.go` — the Bubble Tea interactive picker (`pickerModel`)
- `ratelimit.go` — `rateLimitTracker` that records REST/GraphQL quota and prints it on exit
- Tests live alongside each file (`*_test.go`)

## Testing against the live API

The extension runs against the signed-in user's **real** GitHub notifications. To avoid
accidentally destroying notification state, follow these rules:

- Only run the built binary against the live API for **read-only** commands (listing and
  filtering). These never mutate server state.
- **Never** run mutating commands (e.g. mark-as-read, mark-as-done, unsubscribe) against the
  live API during development. Marking notifications read/done is effectively irreversible and
  can lose the working set used for testing.
- `--dry-run` makes no API calls, so it is safe for end-to-end checks of a mutating command.
- Test mutating behavior with unit tests and fake clients via the `requestDoer` and
  `graphQLDoer` interfaces — do not exercise it with a live `api.DefaultRESTClient()`.
- If a mutating command must be tried end-to-end without `--dry-run`, use a throwaway test
  account, not the developer's primary account.

## Conventions

- Extension binary is named `gh-notifications.exe` (matches repo name without `gh-` prefix for `gh` dispatch)
- Use `api.DefaultRESTClient()` / `api.DefaultGraphQLClient()` for authenticated calls (inherits `gh` auth)
- API access is abstracted behind the `requestDoer` (REST) and `graphQLDoer` (GraphQL)
  interfaces so logic can be unit tested with fakes instead of live calls
- CLI flags are registered in `parseArgs` (both long and short forms) into the `options` struct;
  validation also lives there
- All REST calls flow through the `rateLimitTracker` (a `requestDoer` wrapper) so the quota is
  captured; pass it, not the raw client, into fetch/mutate/picker code
- Line endings are normalized to LF via `.gitattributes` (`* text=auto eol=lf`); keep all files
  LF. Code must be gofmt-clean — run `make fmt` (or `gofmt -w .`) before committing

### Filtering pipeline

- `runNotifications` fetches once, then applies filters in order. `filterByTitle` and
  `filterByType` are pure functions over the fetched slice.
- `--state`, `--draft`, and the issue close-reason states (`not-planned`/`completed`) all rely on
  per-item attributes fetched via **one batched GraphQL call** (`fetchItemDetails`, returning
  `itemDetail{state, stateReason, isDraft}`); the filters (`filterByState`, `filterByDraft`) are
  pure over that map. Add new GraphQL-derived filters by extending `itemDetail` and the query —
  do not add per-item REST round-trips (avoid N+1).
- The GraphQL query also requests the `rateLimit` field so the GraphQL quota is reported.

### Adding a mutating action

Mutating actions (read/done/unsubscribe) are data-driven by the `threadActions` map in
`mutate.go` (each entry has an `apply` func plus message templates). To add one: add a map entry,
a flag + `runMark*` wrapper, and a picker key in `actionForKey` (lowercase = selected item,
uppercase = all visible). Every mutating command must support `--dry-run` and an interactive
`[y/N]` confirmation.

### Interactive picker (Bubble Tea)

- Built on `bubbletea` + `bubbles/list` with the alt screen; side effects (open in browser, mark)
  run as `tea.Cmd`s that return messages (`openedMsg`/`markedMsg`), so the UI never blocks
- Browser launcher output goes to `io.Discard` so it cannot corrupt the alt-screen UI
- Confirmation is handled in-model (`confirming`/`confirmTargets`); repo focus swaps the list via
  `SetItems` while `pickerModel.all` stays the source of truth

## Testing conventions

- Prefer unit tests with fakes over live calls: `recordingDoer`/`fakeDoer` for REST
  (`requestDoer`), `fakeGQL` for GraphQL (`graphQLDoer`)
- Mutation tests assert that `--dry-run` and aborted confirmations make **zero** API calls
- Test the Bubble Tea model by feeding `tea.Msg` values into `Update` and inspecting model state;
  use `drainCmd` to execute batched `tea.Cmd`s when verifying their side effects
