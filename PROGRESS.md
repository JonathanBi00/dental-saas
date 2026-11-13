# Progress: where we're at

_Last updated: 2026-11-13_

## TL;DR

- We're building a multi-tenant dental practice management SaaS for Australian practices.
- We build the practice-management logic ourselves (patients, appointments, billing, etc.) instead of calling a third-party practice-management API like Dental4Web.
- We follow **[OpenSpec](https://openspec.dev/)**: **propose → review → apply → archive**. Every piece of work starts as a change in `openspec/changes/`.
- The first change is **`add-platform-foundation`** (Phase 0). Its tasks are **3 of 42 complete**.
- A lot of later-task code already exists and mostly works. It's **not ticked off yet** because each task's verification still has to be finished; see [Status by task group](#status-by-task-group).
- Nothing is committed to git yet.

## Read these first (in order)

1. `openspec/config.yaml`: project context and rules that every proposal must follow.
2. `openspec/changes/add-platform-foundation/proposal.md`: what Phase 0 delivers and why.
3. `openspec/changes/add-platform-foundation/design.md`: technical decisions (D1–D13) with reasoning.
4. `openspec/changes/add-platform-foundation/tasks.md`: the checklist. **This is the source of truth for progress.**
5. `openspec/changes/add-platform-foundation/specs/*/spec.md`: the behaviour each area must have, written as testable scenarios.

## Stack

| Layer | Choice |
|---|---|
| Backend | Go 1.26, standard library `net/http`, pgx + sqlc, goose migrations, `log/slog` |
| API contract | `api/openapi.yaml` is the source of truth. Go server code is generated with oapi-codegen. |
| Frontend | React + Vite + TypeScript, TanStack Query/Router, Tailwind + shadcn/ui _(not started)_ |
| Database | PostgreSQL 17. Practices are kept apart by **row-level security** inside the database. |
| Local services | Docker Compose: Postgres, SeaweedFS (S3-compatible storage), Mailpit (catches outgoing email) |

## Get running (about 5 minutes)

Prerequisites: Go 1.26+, Node 22+, Docker Desktop, and the OpenSpec CLI (`npm i -g @fission-ai/openspec`).

```bash
cp .env.example .env                 # .env is gitignored
docker compose up -d --wait          # postgres, objectstore, mailpit

cd backend
go run ./cmd/dentalctl migrate up    # create the schema
go run ./cmd/dentalctl seed          # demo data (safe to re-run)
go run ./cmd/dentalctl serve         # API on http://localhost:8080
```

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
- **Tests** (need Docker; each package starts its own throwaway Postgres): `cd backend && go test ./...`

## Gotchas

- **Postgres is on port 5433**, not 5432, to avoid clashing with a local Postgres install.
- **MinIO was replaced by SeaweedFS.** MinIO no longer publishes public Docker images. File storage isn't used until Phase 6.
- **sqlc runs through Docker** because its parser doesn't build on Windows without a C compiler:
  `cd backend && MSYS_NO_PATHCONV=1 docker run --rm -v "$(pwd -W):/src" -w /src sqlc/sqlc:1.30.0 generate`
- **Regenerating the API code:**
  `cd backend && go tool oapi-codegen -config oapi-codegen.yaml ../api/openapi.yaml && cp ../api/openapi.yaml internal/api/openapi.yaml`
  The copy is embedded and served by the API.
- **No Taskfile yet** (task 1.5), so run the commands above by hand for now.
- **Ryuk is disabled in tests.** Ryuk is the cleanup container testcontainers normally uses, and it won't start on Docker Desktop for Windows. Instead, each test package calls `testdb.Main(m)` in `TestMain` to remove its container.
- **The OpenSpec slash commands** (`/openspec-propose`, `/openspec-apply-change`, etc.) live in `.claude/skills/`. Start Claude Code from this folder to get them.

## Status by task group

| Group | State |
|---|---|
| **1. Repo and local stack** | ✅ 1.1–1.3 done. ⬜ 1.4 frontend scaffold and 1.5 Taskfile not started. |
| **2. Platform core** | 🟡 Code written. The database tests pass: no context leaks between pooled connections, the RLS guard, migrations up/down/up. Unit tests for errors and pagination pass. Still to do: wire up `task gen` (2.5) and an HTTP test for health checks (2.7). |
| **3. Locations** | 🟡 Endpoints work in manual testing, and the cross-practice isolation tests pass. The HTTP and service test cases aren't written yet. `org create` hasn't been run against Mailpit. |
| **4. Sign-in, sessions, invitations, reset** | 🟡 All code written and sign-in checked by hand. Password unit tests pass. The HTTP test cases (lockout, expiry, invites, reset) aren't written yet. |
| **5. Roles and permissions** | 🟡 Permission matrix, location restrictions and owner-protection rules written. No tests yet. |
| **6. Audit log** | 🟡 Append-only enforcement tested, and secret-stripping tested. Audit calls are in every service. The HTTP tests and list-endpoint tests aren't written yet. |
| **7. Frontend** | ⬜ Not started. |
| **8. Seed, docs, end-to-end check** | 🟡 Seed works and is safe to re-run. Proper README and end-to-end check not done. |

**Next step:** finish the API test suite. `backend/internal/api/harness_test.go` is written but no test cases use it yet. After that, task 1.4 (frontend scaffold) and 1.5 (Taskfile), then tick off groups 2–6 as their checks pass.

## How the code is organised

```
api/openapi.yaml                  API contract (edit this first, then regenerate)
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
