# Dental SaaS

Practice management software for Australian dental practices, run as one shared cloud service that many practices sign up to. One Go program holds all the logic. A React app calls it through a typed API generated from a single OpenAPI file, and Postgres row-level security keeps each practice's data separate even if our code has a bug.

> **Status: early development.** Phase 0, the platform foundation, is finished:
> - practices and locations;
> - sign-in, invitations and password reset;
> - roles and permissions;
> - an audit log.
>
> No dental features exist yet. Phase 1 (reference data) is next, and nothing is deployed. See [Status and roadmap](#status-and-roadmap).

## How it fits together

```
Browser ──> Vite :5173 ──/api──> Go API :8080 ──> Postgres :5433
            (dev server)          middleware → handler → service → db
                                  Mailpit :8025 catches outgoing email
```

| Part | Where | Built with |
|---|---|---|
| API contract | `api/openapi.yaml` | OpenAPI 3. `task gen` turns it into Go server code and TypeScript types, so the two sides can't drift apart. |
| Backend | `backend/` | Go standard library HTTP, pgx + sqlc (plain SQL, typed Go), goose migrations, `log/slog` |
| Web app | `frontend/` | React 19, Vite, TypeScript, TanStack Router + Query, Tailwind + shadcn/ui, React Hook Form + Zod |
| Database | Docker | PostgreSQL 17 |
| Local services | `docker-compose.yml` | Postgres (dev and test), SeaweedFS (S3-compatible files, unused until Phase 6), Mailpit |
| Plans and specs | `openspec/` | [OpenSpec](https://openspec.dev/): every change is planned before it's coded |

**A request** goes from the browser to Vite, which forwards anything under `/api` to Go. That keeps the session cookie same-origin, so there's no CORS. Inside Go, the request passes through middleware (request ID, logging, panic recovery, same-origin check), then a generated router and a thin handler. Finally it reaches a service, which holds the rules and is the only layer that touches the database. In production, a web server such as Caddy replaces Vite, serving the built files and forwarding `/api`.

**Who can do what** is checked at three layers:
1. **Role permissions**, checked by name (for example `locations:manage`), inside the service.
2. **Clinic scope:** a member limited to some clinics gets 404 for the others.
3. **Row-level security in Postgres.** Every query runs in a transaction tagged with the active practice, so another practice's rows are invisible.

Sign-in uses server-side sessions in an HttpOnly cookie, not JWTs, so deactivating someone takes effect on their next request. Every change to practice data writes an append-only audit entry in the same transaction, which the Australian Privacy Act requires for health information.

## Run it locally

**Prerequisites:**
- [Go](https://go.dev/dl/) 1.26+
- [Node.js](https://nodejs.org/) 22+
- [Docker Desktop](https://www.docker.com/products/docker-desktop/), running
- [Task](https://taskfile.dev/installation/): `winget install Task.Task` on Windows, or `brew install go-task` on macOS
- The [OpenSpec](https://openspec.dev/) CLI, for planning changes: `npm i -g @fission-ai/openspec`

```bash
cp .env.example .env   # the defaults work as-is
task up                # Postgres (dev + test), file storage and Mailpit in Docker
task migrate           # create the tables
task seed              # demo data (safe to re-run)
task dev               # API on :8080 + web app on :5173; Ctrl+C stops both
```

Open http://localhost:5173 and sign in with password `demo-password-123`:
- **`owner@demo.local`** can do everything.
- **`frontdesk@demo.local`** is limited to the Parramatta clinic and sees no Settings.

Invitation and reset emails land in Mailpit at http://localhost:8025. The API's own contract is served at http://localhost:8080/api/v1/openapi.yaml, and its health check at `/healthz`. To check the API directly:

```bash
curl -c cookies.txt -H 'Content-Type: application/json' \
  -d '{"email":"owner@demo.local","password":"demo-password-123"}' \
  http://localhost:8080/api/v1/auth/login
curl -b cookies.txt http://localhost:8080/api/v1/locations   # front desk would see only Parramatta
```

<details>
<summary>Running the Docker services on another machine (for example a home lab)</summary>

On the server, clone the repo and run `docker compose up -d --wait`. Add a `docker-compose.override.yml` that publishes the ports on `127.0.0.1` only:

```yaml
services:
  postgres:      { ports: !override ["127.0.0.1:5433:5432"] }
  postgres-test: { ports: !override ["127.0.0.1:5434:5432"] }
  objectstore:   { ports: !override ["127.0.0.1:8333:8333", "127.0.0.1:9333:9333"] }
  mailpit:       { ports: !override ["127.0.0.1:1025:1025", "127.0.0.1:8025:8025"] }
```

On your PC, skip `task up` and leave this tunnel running in its own terminal. Everything then appears on `localhost` and `.env.example` works unchanged:

```bash
ssh -N -L 5433:127.0.0.1:5433 -L 5434:127.0.0.1:5434 -L 1025:127.0.0.1:1025 -L 8025:127.0.0.1:8025 \
       -L 8333:127.0.0.1:8333 -L 9333:127.0.0.1:9333 you@your-server
```
</details>

## Commands

| Command | What it does |
|---|---|
| `task up` / `task down` | Start / stop the Docker services (data is kept) |
| `task migrate` | Apply migrations. `task migrate -- status` shows where you are, `-- reset` wipes the tables |
| `task seed` | Create the demo practice, clinics and users |
| `task dev` | Run the API and the web app |
| `task gen` | Regenerate code after changing `api/openapi.yaml` or `backend/queries/*.sql` |
| `task test` | Go tests against a throwaway test database on :5434, then Vitest |
| `task lint` | Go formatting, `go vet`, golangci-lint (if installed) and ESLint |
| `task smoke` | End-to-end check: migrations up, down and up, seed twice, start the API and sign in. Resets the dev database |

## CI

Every pull request and every push to `main` runs [`.github/workflows/ci.yml`](.github/workflows/ci.yml). The `CI passed` job only goes green when all of these do, so it's the one check to require in branch protection.

| Job | What it checks |
|---|---|
| Backend lint | `go mod tidy` leaves nothing to commit, `go vet`, golangci-lint (config in `backend/.golangci.yml`) |
| Backend tests | `go test -race -shuffle=on` against the `postgres-test` service, with a coverage summary on the run page |
| Go vulnerabilities | `govulncheck` on the code and the Go toolchain pinned in `go.mod` |
| Generated code | `task gen` changes nothing, so sqlc, oapi-codegen and openapi-typescript output is committed and current |
| Frontend | ESLint with no warnings, `tsc` + Vite build, Vitest, `npm audit` on runtime dependencies |
| Migrations and smoke test | `scripts/smoke.sh`: migrations up, fully down and up again, seed twice, then sign in and check location scoping, 401s and the cross-origin block |
| OpenSpec | `openspec validate --all --strict` |
| Secret scan | gitleaks over the full git history |

Also: CodeQL (Go, TypeScript and the workflows themselves) on every PR and weekly, dependency review on PRs (blocks high-severity vulnerabilities and GPL/AGPL licences), and Dependabot for Go, npm, Actions and Docker images. Actions are pinned to commit SHAs; Dependabot bumps them.

To run the lint locally, install golangci-lint v2 (`brew install golangci-lint`, or `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0`); `task lint` picks it up.

## Repo layout

```
api/openapi.yaml        API contract: change this first, then `task gen`
backend/
  cmd/dentalctl/        the one backend program: serve | migrate | seed | org create
  migrations/           numbered SQL files, embedded in the binary
  queries/              hand-written SQL that sqlc turns into Go
  internal/
    api/                router, middleware wiring and thin handlers
    tenancy/ identity/ access/ auditlog/   services: where the rules live
    platform/           plumbing: transactions + RLS, errors, middleware, audit writing, email
    store/, api/gen/    generated: don't edit
frontend/src/
  api/                  typed client (client.ts) + generated types (schema.gen.ts)
  routes/               one file per screen; signed-in screens under _app/
  lib/                  session, query setup (401 → sign in), form rules
  components/           shadcn/ui blocks and the app shell
db/init/                creates the three database roles
scripts/smoke.sh        end-to-end smoke test used by CI (`task smoke`)
.github/                CI, CodeQL, dependency review and Dependabot
openspec/               specs for how the system behaves, and changes in progress
```

## Status and roadmap

Each phase is one or more OpenSpec changes. Delivery is MVP first: ship a thin working version, and list extras as follow-ups.

| Phase | Covers | State |
|---|---|---|
| 0 | Foundation: practices, clinics, sign-in, roles, audit log | Done, archived 2026-11-20 |
| 1 | Reference data: practitioners, procedure codes, appointment books | Proposed (`add-reference-data`) |
| 2 | Patients | Proposed (`add-patients`) |
| 3 | Scheduling and finding free appointment slots | |
| 4 | Clinical: charting, treatment plans, notes | |
| 5 | Billing: invoices, payments, balances, health funds | |
| 6 | Files and imaging | |
| 7 | Public API for outside integrators, plus FHIR | |

Later: claims through HICAPS and Medicare, SMS reminders, online booking.

**Before any real patient data:** multi-factor authentication must be added.

**Known gaps from Phase 0:**
- Lists load at most 200 rows with no paging.
- Audit log filters aren't kept in the URL.
- The settings screens have no Vitest tests yet.

**Needed before staging:**
- Move database passwords out of `db/init/01-roles.sql` into per-environment settings.
- Add per-environment `.env` files.
- Package the app as containers.

**To understand the system, read:**
1. `openspec/config.yaml`: the project context and the rules every proposal follows.
2. `openspec/specs/*/spec.md`: how the system behaves today, as testable scenarios.
3. `openspec/changes/archive/2026-11-20-add-platform-foundation/design.md`: Phase 0's technical decisions (D1–D13) and why.
4. `openspec/changes/`: changes in progress. Each change's `tasks.md` is the source of truth for its progress.

## Adding a feature

Every feature touches the same five or six files, in this order:
1. **Plan it** as an OpenSpec change: `/openspec-propose`.
2. **Table:** add a migration with `org_id` and row-level security, plus an isolation test. `TestRLSGuard` fails without them.
3. **SQL:** add the queries in `backend/queries/`.
4. **Endpoint:** add it to `api/openapi.yaml`, then run `task gen`. The build fails until the handler exists.
5. **Handler:** add a thin one in `internal/api/server.go`.
6. **Service:** `access.Require` first, then validation, then `db.InTenantTx`, with `audit.Record` in the same transaction.
7. **Screen:** add a route under `src/routes/_app/` using `useQuery` and `useMutation`, gated by permission.

### Rules for code

1. **Every query goes through `db.InTenantTx` or `db.InAuthTx`.** They tell Postgres which practice and user the transaction belongs to, so row-level security applies. The raw connection pool isn't exported, on purpose.
2. **Every table with an `org_id` needs `ENABLE` and `FORCE ROW LEVEL SECURITY` and a policy.** `TestRLSGuard` fails if one is missing.
3. **Every change to practice data writes an audit entry** with `audit.Record(ctx, tx, …)`, in the same transaction as the change.
4. **Check permissions by name** (`access.Require(ctx, access.PermX)`), never by role, inside the service rather than the handler.
5. **New endpoints start in `api/openapi.yaml`**, then you run `task gen`.
6. **Data conventions:**
   - UUIDv7 ids, from `ids.New()`;
   - money as whole cents;
   - timestamps stored and returned in UTC;
   - soft deletes, so rows are archived rather than removed;
   - clinical records are never edited in place, and corrections are added as amendments.

### Working together with OpenSpec

These rules let several people work at once without fighting over the same files.

**One change per piece of work**
1. **Each change has one owner, one branch and one PR.** Name the branch `feat/<change-name>`, for example `feat/add-patients` for `openspec/changes/add-patients/`. Start a new change for each feature rather than adding to someone else's.
2. **Keep changes small**, days not weeks. The longer a change stays open, the more likely it clashes with another.
3. **Only a change's owner ticks its `tasks.md`.** If two people really must share a change, each takes whole task sections.
4. **Never edit `openspec/specs/` by hand.** A change describes what it adds or alters in its own `specs/` folder, as `ADDED`, `MODIFIED` or `REMOVED` requirements. Archiving merges them in.

**Archiving**

5. **Archive as the last commit of the change's PR**, with `openspec archive <name>`. Code and specs then land together.
6. **If a change spans several PRs**, archive it in the last one.
7. **If your archive conflicts with one already merged**, rebase and fix your own delta.

**Changes that depend on each other**

8. **Agree and merge the spec before the code.** Once the contract is settled, the code can be written in parallel.
9. **When change B builds on change A:**
   - merge A's spec first;
   - branch B from A, or from `main` once A's spec is in;
   - write "Depends on: `<A>`" in B's `proposal.md`;
   - archive A before B.
10. **When two changes need a shared interface**, such as an API shape or a table, put just that interface in a small change. Merge it first, then build both against it.
11. **If you keep editing each other's requirements, it's one change.** Give it one owner and split the work by task section.

**Breaking changes** (a `MODIFIED` or `REMOVED` delta on an existing spec)

12. **Say what breaks in `proposal.md`:** what changes, who's affected and how to migrate. Put the data migration and rollout order in `design.md`.
13. **Expand, then contract:**
    - first, ADD the new behaviour beside the old;
    - then move the callers over;
    - remove the old behaviour in a later change.
14. **Before archiving, check who else touches that capability.** Run `openspec list` and look in `openspec/changes/*/specs/<capability>/`, then tell those owners, because they'll need to rebase.

## Troubleshooting and gotchas

- **Postgres is on port 5433**, not 5432, to avoid clashing with a local install.
- **`task` not found after installing it:** open a new terminal.
- **Docker errors mentioning `dockerDesktopLinuxEngine`:** start Docker Desktop.
- **Port 5173 or 8080 in use:** an earlier `task dev` is still running. Stop it, or end the leftover `node` or `dentalctl` process.
- **Type errors after changing the API:** run `task gen`.
- **sqlc runs in Docker**, because its parser doesn't build on Windows without a C compiler. `task gen` handles this.
- **Generated files:**
  - `backend/internal/api/openapi.yaml` is a copy of `api/openapi.yaml` that the API serves. `task gen` refreshes it.
  - `frontend/src/routeTree.gen.ts` is regenerated whenever Vite runs. Commit it, but don't edit it.
- **TypeScript is pinned to 5.9**, because `openapi-typescript` doesn't support TypeScript 6 yet.
- **Test containers:**
  - Ryuk, testcontainers' cleanup container, is disabled because it won't start on Docker Desktop for Windows. Each test package calls `testdb.Main(m)` in `TestMain` to remove its own container.
  - Tests use the `postgres-test` service through `TESTDB_ADMIN_URL` in `.env`, and fall back to testcontainers without it.
- **SeaweedFS replaced MinIO**, which no longer publishes public Docker images.
- **The OpenSpec slash commands** (`/openspec-propose`, `/openspec-apply-change`, …) live in `.claude/skills/`. Start Claude Code from the repo root to get them.
