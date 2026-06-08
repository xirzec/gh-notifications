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

## Conventions

- Extension binary is named `gh-notifications.exe` (matches repo name without `gh-` prefix for `gh` dispatch)
- Use `api.DefaultRESTClient()` for authenticated GitHub API calls (inherits `gh` auth)
