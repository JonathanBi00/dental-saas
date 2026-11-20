# Progress: where we're at

_Last updated: 2026-11-20_

## TL;DR

- We're building a multi-tenant dental practice management SaaS for Australian practices.
- We build the practice-management logic ourselves (patients, appointments, billing, etc.) instead of calling a third-party practice-management API like Dental4Web.
- We follow **[OpenSpec](https://openspec.dev/)**: **propose → review → apply → archive**. Every piece of work starts as a change in `openspec/changes/`.
- The first change is **`add-platform-foundation`** (Phase 0). **All 42 tasks are complete and the change was archived on 2026-11-20**; its specs are now the main specs in `openspec/specs/`.
- The frontend (group 7) is done: sign-in, password reset, invitations, the app shell and the Settings screens, verified with Vitest and by hand against the dev stack. The README and end-to-end check (8.2, 8.3) were verified on 2026-11-20 against Docker running on the home-lab server.
- Delivery is **MVP first**: ship a thin working version, and list extras as follow-ups.

## Read these first (in order)

1. `openspec/config.yaml`: project context and rules that every proposal must follow.
2. `openspec/specs/*/spec.md`: how the system behaves today (tenancy, user-auth, access-control, audit-log, api-conventions), written as testable scenarios.
3. `openspec/changes/archive/2026-11-20-add-platform-foundation/`: Phase 0's proposal, design decisions (D1–D13 in `design.md`) and completed task list.
4. `openspec/changes/`: active changes. Each one's `tasks.md` is the source of truth for its progress.

## Stack

| Layer | Choice |
|---|---|
| Backend | Go 1.26, standard library `net/http`, pgx + sqlc, goose migrations, `log/slog` |
| API contract | `api/openapi.yaml` is the source of truth. Go server code is generated with oapi-codegen. |
| Frontend | React 19 + Vite + TypeScript, TanStack Query/Router, Tailwind v4 + shadcn/ui, React Hook Form + Zod, openapi-fetch; Vitest + Testing Library |
| Database | PostgreSQL 17. Practices are kept apart by **row-level security** inside the database. |
| Local services | Docker Compose: Postgres (dev, plus an in-memory one for tests), SeaweedFS (S3-compatible storage), Mailpit (catches outgoing email). Can run on another machine through an SSH tunnel; see the README. |

## Get running (about 5 minutes)

Prerequisites: Go 1.26+, Node 22+, Docker Desktop, [Task](https://taskfile.dev) (`winget install Task.Task`), and the OpenSpec CLI (`npm i -g @fission-ai/openspec`).

```bash
cp .env.example .env    # .env is gitignored
task up                 # postgres, objectstore, mailpit
task migrate            # create the schema
task seed               # demo data (safe to re-run)
task dev                # API on :8080 + web app on http://localhost:5173 (Ctrl+C stops both)
```

Other commands: `task gen` (regenerate sqlc, oapi-codegen and TypeScript types), `task test`, `task lint`, `task down`. Run `task --list` for all of them.

- **Demo sign-ins** (password `demo-password-123`):
  - `owner@demo.local` is the owner of "Demo Dental".
  - `frontdesk@demo.local` is front desk, restricted to the Parramatta location.
- **Useful URLs:**
  - `localhost:8080/healthz`
  - `localhost:8080/api/v1/openapi.yaml`
  - Mailpit inbox: `localhost:8025`
- **Quick API check:**
  ```bash
  curl -c cj.txt -H 'Content-Type: application/json' \
    -d '{"email":"owner@demo.local","password":"demo-password-123"}' \
    localhost:8080/api/v1/auth/login
  curl -b cj.txt localhost:8080/api/v1/locations
  ```
- **Tests:** `task test`. Backend tests use the `postgres-test` service (port 5434) via `TESTDB_ADMIN_URL` in `.env`; without it they fall back to testcontainers.

## Gotchas

- **Postgres is on port 5433**, not 5432, to avoid clashing with a local Postgres install.
- **MinIO was replaced by SeaweedFS.** MinIO no longer publishes public Docker images. File storage isn't used until Phase 6.
- **sqlc runs through Docker** because its parser doesn't build on Windows without a C compiler. `task gen` handles this.
- **`backend/internal/api/openapi.yaml` is a copy** of `api/openapi.yaml`, embedded and served by the API. `task gen` refreshes it.
- **The browser only talks to Vite.** Vite forwards `/api` to the Go API, so the sign-in cookie is same-origin and there's no CORS setup.
- **`frontend/src/routeTree.gen.ts` is generated** by the TanStack Router plugin whenever Vite runs. Commit it, don't edit it.
- **TypeScript is pinned to 5.9** because `openapi-typescript` doesn't support TypeScript 6 yet.
- **Ryuk is disabled in tests.** Ryuk is the cleanup container testcontainers normally uses, and it won't start on Docker Desktop for Windows. Instead, each test package calls `testdb.Main(m)` in `TestMain` to remove its container.
- **The OpenSpec slash commands** (`/openspec-propose`, `/openspec-apply-change`, etc.) live in `.claude/skills/`. Start Claude Code from this folder to get them.

## Status by task group

| Group | State |
|---|---|
| **1. Repo and local stack** | ✅ Done. Frontend scaffold builds and serves a placeholder; `task up`/`dev`/`gen`/`test`/`lint` verified on Windows. |
| **2. Platform core** | ✅ Done. |
| **3. Locations** | ✅ Done. |
| **4. Sign-in, sessions, invitations, reset** | ✅ Done. |
| **5. Roles and permissions** | ✅ Done. |
| **6. Audit log** | ✅ Done. |
| **7. Frontend** | ✅ Done. Typed client with 401 → sign in; sign-in, forgot/reset password and accept-invite screens; app shell with organisation/location switchers and permission-gated nav; Settings → Locations, Users and Audit log. Vitest covers validation, the 401 redirect and hidden admin nav; every screen was checked by hand against the dev stack. MVP follow-ups: lists load at most 200 rows with no paging, audit filters aren't kept in the URL, and the settings screens have no Vitest tests yet. |
| **8. Seed, docs, end-to-end check** | ✅ Done. README followed from a clean clone; end-to-end flow (owner creates a location, invites a Parramatta-only front desk user who onboards via Mailpit and sees only Parramatta with no admin access); `task test` and `task lint` pass. All against the home-lab server. |

**Next step:** propose Phase 1. Follow-ups for later environments: move database passwords out of `db/init/01-roles.sql` into per-environment settings, add per-environment `.env` files, and package the app as containers for staging.

## How the code is organised

```
api/openapi.yaml                  API contract (edit this first, then `task gen`)
Taskfile.yml                      developer commands (task up | dev | gen | migrate | seed | test | lint)
db/init/01-roles.sql              creates DB roles (runs once when the Postgres volume is created)
backend/
  cmd/dentalctl/                  single binary: serve | migrate | seed | org create
  migrations/                     goose SQL migrations (embedded in the binary)
  queries/*.sql                   hand-written SQL that sqlc turns into Go code
  internal/
    store/                        GENERATED by sqlc. Don't edit.
    api/gen/                      GENERATED by oapi-codegen. Don't edit.
    api/                          handlers (thin), router, auth middleware
    tenancy/                      practices and locations
    identity/                     users, passwords, sessions, invitations, password reset
    access/                       roles, permissions, members, location restrictions
    auditlog/                     reading the audit log
    seed/                         demo data
    platform/                     config, db, httpx (errors, middleware, pagination),
                                  mail, audit (writing), ids, apperr, testdb (test helpers)
```

## Rules to follow when adding code

1. **Every database query goes through `db.InTenantTx` or `db.InAuthTx`.** These set which practice and user the transaction belongs to, so Postgres can enforce row-level security. The raw connection pool isn't exported on purpose.
2. **Every table with an `org_id` needs `ENABLE` + `FORCE ROW LEVEL SECURITY` and a policy.** A test (`TestRLSGuard`) fails if one is missing.
3. **Every change to practice data writes an audit entry** with `audit.Record(ctx, tx, …)`, using the same transaction as the change.
4. **Check permissions by name** (`access.Require(ctx, access.PermX)`), never by role, and do it inside the service rather than the handler.
5. **New endpoints start in `api/openapi.yaml`**, then you regenerate the code.
6. **Data conventions:**
   - UUIDv7 ids (`ids.New()`);
   - money as whole cents;
   - timestamps stored and returned in UTC;
   - soft deletes (mark rows deleted rather than removing them);
   - clinical records are never edited in place: corrections are added as amendments.

## Product roadmap (each phase = one or more OpenSpec changes)

| Phase | Covers |
|---|---|
| 0 | Foundation _(current)_ |
| 1 | Reference data: practitioners, procedure codes, appointment books |
| 2 | Patients |
| 3 | Scheduling and finding free appointment slots |
| 4 | Clinical: charting, treatment plans, notes |
| 5 | Billing: invoices, payments, balances, health funds |
| 6 | Files and imaging |
| 7 | Public API for outside integrators, plus FHIR |

Later: claims through HICAPS and Medicare, SMS reminders, online booking.

**Open item:** multi-factor authentication must be added before any real patient data is stored.
