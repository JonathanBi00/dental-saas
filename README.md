# Dental SaaS

Practice management software for Australian dental practices, run as one shared cloud service that many practices sign up to. One Go program holds all the logic. A React app calls it through a typed API generated from a single OpenAPI file, and Postgres row-level security keeps each practice's data separate even if our code has a bug.

> **Status: early development.** Phase 0, the platform foundation, is finished:
> - practices and locations;
> - sign-in, invitations and password reset;
> - roles and permissions;
> - an audit log.
>
> No dental features exist yet. Phase 1 (reference data) is next. Nothing is deployed. See [`PROGRESS.md`](PROGRESS.md) for detail and the roadmap.

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

Invitation and reset emails land in Mailpit at http://localhost:8025. The API's own contract is served at http://localhost:8080/api/v1/openapi.yaml.

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
| `task lint` | Go formatting, `go vet` and ESLint |

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
openspec/               specs for how the system behaves, and changes in progress
```

## Adding a feature

Every feature touches the same five or six files, in this order:
1. **Plan it** as an OpenSpec change: `/openspec-propose`.
2. **Table:** add a migration with `org_id` and row-level security, plus an isolation test. `TestRLSGuard` fails without them.
3. **SQL:** add the queries in `backend/queries/`.
4. **Endpoint:** add it to `api/openapi.yaml`, then run `task gen`. The build fails until the handler exists.
5. **Handler:** add a thin one in `internal/api/server.go`.
6. **Service:** `access.Require` first, then validation, then `db.InTenantTx`, with `audit.Record` in the same transaction.
7. **Screen:** add a route under `src/routes/_app/` using `useQuery` and `useMutation`, gated by permission.

Each change has one owner, one branch (`feat/<change-name>`) and one PR, and is archived as the last commit of that PR. The full rules, including dependent and breaking changes, are in [`PROGRESS.md`](PROGRESS.md#how-we-work-with-openspec-changes).

## Troubleshooting

- **Postgres is on port 5433**, not 5432, to avoid clashing with a local install.
- **`task` not found after installing it:** open a new terminal.
- **Docker errors mentioning `dockerDesktopLinuxEngine`:** start Docker Desktop.
- **Port 5173 or 8080 in use:** an earlier `task dev` is still running. Stop it, or end the leftover `node` or `dentalctl` process.
- **Type errors after changing the API:** run `task gen`.
