# Dental SaaS: instructions for Claude Code

Multi-tenant practice management software for Australian dental practices. Go API, React web app, Postgres with row-level security. Read `README.md` for the architecture, setup and the full rules. This file covers what an agent most needs and most often gets wrong.

## Commands

Run from the repo root. Docker must be running.

| Task | Command |
|---|---|
| Start services | `task up` (Postgres :5433, test Postgres :5434, Mailpit :8025) |
| Migrate / seed | `task migrate`, `task seed` (demo password `demo-password-123`) |
| Run the app | `task dev` (API :8080, web :5173) |
| Regenerate code | `task gen` after any change to `api/openapi.yaml` or `backend/queries/*.sql` |
| Test | `task test`. One Go package: `cd backend && go test ./internal/tenancy/...` |
| Lint | `task lint` |
| End-to-end check | `task smoke` (resets the dev database) |
| Validate specs | `openspec validate --all --strict` |

Before you say you're done, run `task gen`, `task lint` and `task test`, and check `git status` shows no stray generated changes. CI fails on any of these.

## Never break these

1. **Tenant isolation.** Every query goes through `db.InTenantTx` or `db.InAuthTx`. Never add a way round them, and never export the raw pool.
2. **Every new table with `org_id`** gets `ENABLE` and `FORCE ROW LEVEL SECURITY`, a policy, and a tenant isolation test showing organisation B can't read, insert or update A's rows. `TestRLSGuard` fails without the policy.
3. **Every change to practice data** calls `audit.Record(ctx, tx, …)` in the same transaction. Reads of patient data are audited too (Phase 2 onward).
4. **Permissions are checked by name** (`access.Require(ctx, access.PermX)`) inside the service, never by role and never in the handler.
5. **Contract first.** A new endpoint goes into `api/openapi.yaml` first, then `task gen`, then a thin handler in `internal/api/server.go`, then the service.
6. **Data conventions:** UUIDv7 from `ids.New()`, money as integer cents, UTC timestamps, soft deletes, and clinical records are amended rather than edited.

## Don't edit generated files

- `backend/internal/store/*.go` (sqlc)
- `backend/internal/api/gen/` (oapi-codegen)
- `backend/internal/api/openapi.yaml` (a copy of `api/openapi.yaml`)
- `frontend/src/api/schema.gen.ts` (openapi-typescript)
- `frontend/src/routeTree.gen.ts` (TanStack Router)

Change the source, then run `task gen`.

## Workflow

- **Plan with OpenSpec first.** Product changes start as `openspec/changes/<name>/`, using `/openspec-propose`. Implement with `/openspec-apply-change`. Follow the rules in `openspec/config.yaml`.
- **Only tick tasks in your own change's `tasks.md`.** Never edit `openspec/specs/` by hand: archiving merges the deltas in.
- **One change, one branch, one PR.** The branch is `feat/<change-name>`. Archive the change as the PR's last commit.
- **Update `CHANGELOG.md`** under Unreleased in every PR that changes behaviour, tooling or dependencies.
- **Ship the MVP slice.** List extras as follow-ups in the change instead of building them.
- **Commit messages** are plain imperative sentences, such as "Add patient search endpoint".

## Gotchas

- Postgres is on **5433**, not 5432. Tests use **5434** through `TESTDB_ADMIN_URL`, or testcontainers if that's unset.
- sqlc runs in Docker (`task gen` handles it). TypeScript is pinned to 5.9 for openapi-typescript.
- Go builds with toolchain 1.26.11 from `go.mod`. `GOTOOLCHAIN=auto` downloads it.
- Every Go test package that touches the database needs `func TestMain(m *testing.M) { testdb.Main(m) }`.
- Multi-factor authentication is a hard prerequisite before any real patient data.
