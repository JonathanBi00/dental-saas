# Changelog

Notable changes to Dental SaaS, newest first. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). There are no tagged releases yet, so entries are grouped by the date they merged.

Every PR adds a line under **Unreleased**, in the matching group: Added, Changed, Fixed, Removed or Security. When we cut a release, Unreleased becomes a version heading.

## Unreleased

### Added
- CI on every PR and push to `main`: golangci-lint, race-enabled Go tests, govulncheck, a check that generated code is current, frontend lint/build/tests/audit, a migration and API smoke test (`scripts/smoke.sh`, `task smoke`), OpenSpec validation and a gitleaks secret scan. `CI passed` is the single required check.
- CodeQL, dependency review on PRs, and Dependabot for Go, npm, GitHub Actions and Docker images.
- `CLAUDE.md` with project instructions for Claude Code, and this changelog.

### Changed
- `task lint` also runs golangci-lint when it's installed.

### Security
- Pinned the Go toolchain to 1.26.11. Go 1.26.2 had 29 known standard library vulnerabilities reachable from our code.

## 2026-11-24

### Added
- Proposed Phase 2, patients (`openspec/changes/add-patients`). Branches are now named `feat/<change-name>`.

### Changed
- Rewrote the README as a short overview and merged `PROGRESS.md` into it.

## 2026-11-20: Phase 0 complete

### Added
- Proposed Phase 1, reference data: practitioners, procedure codes and appointment books (`openspec/changes/add-reference-data`).
- Backend tests can run against the shared `postgres-test` service (`TESTDB_ADMIN_URL`) instead of testcontainers. Docs explain how to run Docker on a remote host.
- How we work together on OpenSpec changes (README).

### Changed
- Archived `add-platform-foundation` after the end-to-end check passed. Its specs now live in `openspec/specs/`.

## 2026-11-17

### Added
- Web app: sign-in, forgot and reset password, accepting an invitation, the app shell with practice and clinic switchers, and the Settings screens for locations, users and the audit log.
- Tests for identity, access control and the audit log.

## 2026-11-16

### Added
- Tests for `/healthz`, `/readyz` and serving the OpenAPI spec, for the tenancy service (timezones, archiving, delta sync, pagination, audit entries) and for location endpoints, including a 404 for another practice's location.

### Fixed
- Parallel test packages on Windows no longer fail with "rootless Docker is not supported".

## 2026-11-14

### Added
- Platform foundation backend: practices and locations, sign-in with server-side sessions, invitations, password reset, roles and permissions, an append-only audit log, and Postgres row-level security on every tenant table.
- Frontend scaffold (React, Vite, TanStack Router and Query, Tailwind, shadcn/ui), a Taskfile and the README.
- OpenSpec setup with the project context and the MVP-first delivery rule.
