# Dental SaaS

Practice management software for Australian dental practices, run as one shared cloud service ("multi-tenant") that many practices sign up to.

It will cover the day-to-day running of a practice: patients, appointments, clinical records, billing and health funds, files and imaging. We build that logic ourselves instead of relying on a third-party practice management system's API. The web app and a future public API for integrators share the same backend.

> **Status: early development.** We're on Phase 0, the platform foundation: practices and locations, user accounts and sign-in, roles and permissions, and an audit log. No dental features exist yet. The web app has sign-in, password reset, invitations, and settings screens for locations, users and the audit log. We're aiming for a small first release (MVP) rather than building every feature before shipping. See [`PROGRESS.md`](PROGRESS.md) for where things are up to.

## How it fits together

| Part | Where | Built with |
|---|---|---|
| API contract | `api/openapi.yaml` | OpenAPI 3. The single source of truth for every endpoint. |
| Backend | `backend/` | Go, standard library HTTP server, PostgreSQL via pgx + sqlc |
| Web app | `frontend/` | React, Vite, TypeScript, TanStack Router + Query, Tailwind, shadcn/ui |
| Database | Docker | PostgreSQL 17 |
| Local services | `docker-compose.yml` | Postgres, SeaweedFS (S3-compatible file storage), Mailpit (catches outgoing email) |
| Plans and specs | `openspec/` | [OpenSpec](https://openspec.dev/): every piece of work starts as a written change |

Each practice's data is kept separate **inside the database** using PostgreSQL row-level security, so one practice can never see another's data even if application code has a bug. Every change to practice data is written to an append-only audit log, as required for health information under the Australian Privacy Act.

## Run it locally

### 1. Install the prerequisites

- [Go](https://go.dev/dl/) 1.26 or newer
- [Node.js](https://nodejs.org/) 22 or newer
- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (must be running)
- [Task](https://taskfile.dev/installation/), which runs the project's commands on Windows, macOS and Linux:
  - Windows: `winget install Task.Task`
  - macOS: `brew install go-task`

### 2. Start everything

From the repo root:

```bash
cp .env.example .env   # local settings; the defaults work as-is
task up                # start Postgres, file storage and Mailpit in Docker
task migrate           # create the database tables
task seed              # add demo data (safe to run again)
task dev               # start the API and the web app; Ctrl+C stops both
```

The first `task dev` installs the web app's packages, so it takes a minute.

### 3. Try it

| What | Where |
|---|---|
| Web app | http://localhost:5173 |
| API | http://localhost:8080 (health check: `/healthz`) |
| API contract | http://localhost:8080/api/v1/openapi.yaml |
| Mailpit inbox (invites, password resets) | http://localhost:8025 |
| File storage (S3 API, not used yet) | http://localhost:8333 |

The seed creates a practice called **Demo Dental**, with locations **Sydney CBD** and **Parramatta**. Both sign-ins use the password `demo-password-123`:

| Email | Role |
|---|---|
| `owner@demo.local` | Owner: can do everything |
| `frontdesk@demo.local` | Front desk: limited to the Parramatta location |

Open http://localhost:5173 and sign in. The owner sees **Settings** (Locations, Users, Audit log). Front desk sees only Parramatta and no settings. Invitation and password-reset emails land in Mailpit (http://localhost:8025); their links open the web app.

To try the API directly instead (Git Bash, macOS or Linux):

```bash
# Sign in and save the session cookie
curl -c cookies.txt -H 'Content-Type: application/json' \
  -d '{"email":"owner@demo.local","password":"demo-password-123"}' \
  http://localhost:8080/api/v1/auth/login

# List the practice's locations
curl -b cookies.txt http://localhost:8080/api/v1/locations
```

Sign in as `frontdesk@demo.local` instead and the list only contains Parramatta.

### 4. Run the tests and checks

```bash
task test   # backend tests (each package starts its own throwaway Postgres in Docker), then web app tests (Vitest)
task lint   # Go formatting, go vet, and ESLint for the web app
```

### Stopping

`Ctrl+C` stops `task dev`. `task down` stops the Docker containers and keeps your data. To wipe the database and start fresh, run `docker compose down -v`, then repeat step 2.

## All commands

| Command | What it does |
|---|---|
| `task up` / `task down` | Start / stop the Docker services |
| `task migrate` | Apply database migrations. `task migrate -- status` shows where you are. |
| `task seed` | Create the demo practice and users |
| `task dev` | Run the API (:8080) and web app (:5173) |
| `task gen` | Regenerate code after changing `api/openapi.yaml` or `backend/queries/*.sql` |
| `task test` | Run the backend and web app tests |
| `task lint` | Run formatting and lint checks |

## Repo layout

```
api/openapi.yaml        API contract: change this first, then run `task gen`
backend/
  cmd/dentalctl/        the one backend program: serve | migrate | seed | org create
  migrations/           database tables, as numbered SQL files
  queries/              hand-written SQL, turned into Go code by sqlc
  internal/
    api/                HTTP handlers (thin; they only convert request and response types)
    tenancy/            practices and locations
    identity/           users, sign-in sessions, invitations, password reset
    access/             roles and permissions
    auditlog/           reading the audit log
    platform/           shared plumbing: database, HTTP errors, middleware, email, audit writing
    store/, api/gen/    generated code; don't edit
frontend/
  src/api/              typed API client (client.ts) + generated types (schema.gen.ts; don't edit)
  src/routes/           pages: signed-out screens at the top, signed-in screens under _app/
  src/components/ui/    shadcn/ui components
  src/lib/              session, Query setup (401 → sign in), form validation, roles
db/init/                creates the database roles when the Postgres container is first created
openspec/               proposals, designs, specs and task lists for each change
```

## Contributing

Before writing code, read the rules in [`PROGRESS.md`](PROGRESS.md#rules-to-follow-when-adding-code). The short version:

1. **New endpoints start in `api/openapi.yaml`.** Run `task gen` afterwards.
2. **All database access goes through `db.InTenantTx`**, so row-level security applies.
3. **Every new table with an `org_id` needs row-level security.** A test fails if it's missing.
4. **Every change to practice data writes an audit entry** in the same transaction.
5. **Check permissions by name** (for example `locations:manage`) inside the service, never by role.

New work starts as an OpenSpec change in `openspec/changes/`: propose, review, implement, then archive.

## Troubleshooting

- **Postgres runs on port 5433**, not 5432, so it doesn't clash with a Postgres already installed on your machine.
- **`task` isn't found right after installing it:** open a new terminal so your PATH updates.
- **Docker errors mentioning `dockerDesktopLinuxEngine`:** Docker Desktop isn't running. Start it and try again.
- **Port 5173 or 8080 already in use:** an earlier `task dev` is still running. Stop it, or close the leftover `node` or `dentalctl` process.
