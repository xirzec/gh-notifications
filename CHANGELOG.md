# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.5.0] - 2026-08-04

### Changed

- Restored unread notifications as the default for commands and saved queries.
- Limited `--all` to ad-hoc listing and interactive browsing because GitHub's API
  does not distinguish read notifications still in the inbox from done notifications.
- Removed `--all` from saved-query creation.
- Removed the redundant `--unread-only` flag now that unread is the default.

### Migration

- Saved queries containing `all: true` or `scope: all` no longer run. Remove that
  setting with `gh notifications edit`; the query will then operate on unread
  notifications.
- Legacy `scope: unread` entries remain compatible but are no longer needed.

## [0.4.0] - 2026-08-03

### Added

- Added pull request CI, dependency review, Dependabot, and security policy files.
- Added explicit `all` and `unread` scopes to saved queries.

### Changed

- Changed commands and new saved queries to include read notifications by default.
- Hardened saved-query argument parsing, validation, and listing output.

## [0.3.0] - 2026-07-31

### Changed

- Improved command help and saved-query discovery.
- Made the interactive picker exit cleanly when no notifications remain.

## [0.2.1] - 2026-07-17

### Changed

- Republished the `v0.2.0` source at a patch version; there were no source changes.

## [0.2.0] - 2026-06-18

### Added

- Added saved queries with optional actions, tags, batch execution, and editor support.
- Added aggregate results for interactive bulk actions.

### Changed

- Clarified that the `closed` state excludes merged pull requests.
- Added portable formatting checks, normalized source line endings, and documented
  GNU Make setup for Windows contributors.

## [0.1.0] - 2026-06-09

### Added

- Initial notification listing, filtering, interactive browsing, and triage actions.
- Added repository focus, bulk actions, API quota reporting, documentation, and licensing.

[Unreleased]: https://github.com/xirzec/gh-notifications/compare/v0.5.0...HEAD
[0.5.0]: https://github.com/xirzec/gh-notifications/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/xirzec/gh-notifications/releases/tag/v0.4.0
[0.3.0]: https://github.com/xirzec/gh-notifications/releases/tag/v0.3.0
[0.2.1]: https://github.com/xirzec/gh-notifications/releases/tag/v0.2.1
[0.2.0]: https://github.com/xirzec/gh-notifications/releases/tag/v0.2.0
[0.1.0]: https://github.com/xirzec/gh-notifications/releases/tag/v0.1.0
