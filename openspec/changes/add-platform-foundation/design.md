# Design

## Context

The repository is empty (no commits). The developer machine has Go 1.26, Node 22 and Docker. Everything runs on localhost: no load balancer, TLS termination, domains or cloud infrastructure in this change. See proposal.md for motivation and the specs for required behaviour.

## Goals / Non-Goals

**Goals:**
- One command brings up dependencies; one command runs backend and frontend with live reload.
- Tenant isolation that holds even when a handler forgets a `WHERE org_id = ...`.
- Patterns (module layout, transaction + tenant context, audit, pagination, codegen) that later domains copy without rethinking.

**Non-Goals:**
- Production deployment, container images for the app itself, CI pipelines.
- Performance tuning beyond sensible indexes.
- Public API keys and `X-Org-Id` header authentication (Phase 7). The tenant-context mechanism is designed so it can be added without changing handlers.

## Decisions

### D1. Repository layout (monorepo)

```
api/openapi.yaml            # contract: the source of truth
backend/
  go.mod
  cmd/dentalctl/            # single binary: serve | migrate | seed | org create
  internal/
    platform/               # config, db (pool, tx, tenant ctx), httpx (errors, pagination, middleware), mail, audit, ids
    tenancy/                # organisations, locations
    identity/               # users, sessions, invitations, password reset
    access/                 # roles, permissions, membership scoping
    api/                    # oapi-codegen generated server interfaces + wiring
  sqlc.yaml, queries/*.sql
db/migrations/              # goose SQL migrations
frontend/                   # Vite + React + TS
docker-compose.yml, Taskfile.yml, .env.example, README.md
```

Domain packages own their SQL queries and service logic. HTTP handlers are thin adapters from generated interfaces to services. Future domains (`patients/`, `scheduling/`, …) sit next to `tenancy/`.

### D2. HTTP: standard library `net/http` + oapi-codegen strict server

Go 1.22+ routing covers method and path parameters, so we use the standard library. oapi-codegen generates a typed "strict" server interface from `api/openapi.yaml`, so the contract can't drift from the code: a missing handler is a compile error.
*Alternatives:* chi (fine, but adds little now); Gin/Echo (own their context types, which makes middleware less portable); code-first spec generation (spec drifts toward whatever the code does).

### D3. Data access: pgx v5 + sqlc

Hand-written SQL in `queries/*.sql`, compiled by sqlc into type-safe Go. This keeps queries visible and reviewable, which matters for RLS and later for billing logic.
*Alternatives:* GORM/ent (hides SQL and makes RLS and query review harder); raw pgx everywhere (no compile-time checking).

### D4. Migrations: goose, SQL files, run by `dentalctl migrate`

Plain SQL up/down migrations in `db/migrations`, embedded in the binary. Migrations run as the **owner** role; the server runs as the **app** role (D5). `task dev` runs migrate before serve.

### D5. Tenant isolation: Postgres RLS with a non-owner app role

- Two roles: `dental_owner` (owns the schema, runs migrations) and `dental_app` (runtime, `NOBYPASSRLS`, not an owner, granted only DML).
- Every tenant-owned table has `org_id uuid NOT NULL`, `ENABLE` and `FORCE ROW LEVEL SECURITY`, and a policy `USING (org_id = current_setting('app.org_id', true)::uuid) WITH CHECK (same)`. When the setting is unset it is NULL, so nothing matches: fail closed.
- All tenant work goes through `db.InTenantTx(ctx, tenant, fn)`, which opens a transaction and runs `set_config('app.org_id', …, true)` and `set_config('app.user_id', …, true)` (transaction-local, so pooled connections can't leak context).
- `memberships` additionally allows `user_id = app.user_id`, so a user can list their own organisations before choosing one.
- **Before a tenant is chosen** (sign-in, invitation lookup, password reset) we need cross-tenant lookups. These go through a small set of `SECURITY DEFINER` SQL functions owned by `dental_owner` (for example `auth_find_user_by_email`, `auth_find_invitation(token_hash)`), with `search_path` pinned. They are the only way the app role reaches across tenants, and each one is reviewed.
- Global (non-tenant) tables: `users`, `sessions`, `login_attempts`, `password_resets`, `organisations` (the app role can only read the row matching `app.org_id` or the user's memberships).
- **Guard test:** a test queries `information_schema` for every table with an `org_id` column and fails if it lacks FORCE RLS and a policy. This stops a new domain table from silently skipping isolation.

*Alternatives:* schema-per-tenant (migrations multiply, cross-tenant operations get awkward); database-per-tenant (too heavy at this stage); application-only filtering (one missed clause leaks health data).

### D6. Identifiers and common columns

UUIDv7 primary keys, generated in Go (time-ordered, so indexes stay efficient and cursors stay simple). Every table has `created_at` and `updated_at` (set by a shared `set_updated_at()` trigger). Tenant data uses `deleted_at` (soft delete); locations use `archived_at` as their soft-delete marker.

### D7. Sessions and authentication

- Session token: 32 random bytes, sent as cookie `ds_session` (HttpOnly, SameSite=Lax, `Secure` when not on localhost, Path=/). Only the SHA-256 hash is stored.
- `sessions` row: user_id, active_org_id, active_location_id, created_at, last_seen_at, revoked_at, ip, user_agent. Idle and absolute expiry are checked on each request. `last_seen_at` is written at most once per minute to avoid a write on every request.
- Membership and organisation status are re-checked on every request, so deactivation and suspension apply immediately.
- Passwords: argon2id (`golang.org/x/crypto/argon2`, OWASP parameters m=19 MiB, t=2, p=1) in PHC string format, so parameters can be raised later. For unknown emails we run a dummy hash so response timing doesn't reveal whether the account exists.
- Lockout: a `login_attempts` table keyed by normalised email, holding a count and window start.
- Cross-site protection: for unsafe methods, require the `Origin` header (or `Sec-Fetch-Site: same-origin`) to match the configured app origin. Because Vite proxies `/api` in development, browser requests are same-origin and no CORS is needed.
- Invitation and reset tokens: random, stored hashed, single-use (`used_at`), with expiry.

*Alternatives:* JWTs (can't be revoked instantly, and we need immediate deactivation); an external identity provider such as Keycloak or Zitadel (adds a heavy service now; we can move to OIDC later behind the same session layer).

### D8. Authorisation

Roles and permissions are a static Go map (`access.RolePermissions`). Handlers declare a required permission, for example `locations:manage`, `members:manage` or `audit:read`. Location restriction is stored in a `membership_locations` join table (empty means unrestricted). Location-scoped queries take the accessible location set from the request's tenant context. In this change, locations are the only location-scoped data.

### D9. Audit log

- `audit_events` table, keyed by UUIDv7, with nullable `org_id` (null for pre-tenant events such as a failed sign-in for an unknown email).
- The app role has `INSERT` and `SELECT` only, plus a trigger that raises an error on `UPDATE` and `DELETE` as a second safeguard. An RLS policy limits `SELECT` to `org_id = app.org_id`.
- Entries are written explicitly by services through `audit.Record(tx, event)` inside the same transaction as the change. Field diffs are computed in the service.
- Explicit calls rather than database triggers, because triggers can't easily see request ID, IP or intent, and generic row diffs would leak columns such as password hashes.
- Request metadata (request ID, IP, user agent, actor) is carried in `context.Context`, populated by middleware.

### D10. API conventions implementation

- Errors: a `httpx.Problem` type and a single error-mapping function. Domain errors (`ErrNotFound`, `ErrForbidden`, `ValidationError`, `ErrConflict`) map to status codes in one place.
- Pagination: the cursor is base64url of `(sort_key, id)`; queries use keyset pagination (`WHERE (sort_key, id) > ($1, $2)`) to keep ordering stable.
- `updated_since`: sorts by `(updated_at, id)` and includes soft-deleted rows. The response also returns `sync_until` (server time minus a 5-second safety margin), which the client uses as its next `updated_since`. This covers transactions that commit after the timestamp they wrote (see Risks).
- The OpenAPI document is served at `/api/v1/openapi.yaml`. Health endpoints are `/healthz` (liveness) and `/readyz` (pings the database).
- Logging: `log/slog` JSON handler, request-ID middleware, and an access log per request, with no request or response bodies.

### D11. Frontend

Vite + React 19 + TypeScript; TanStack Router (file-based routes) and TanStack Query; `openapi-typescript` + `openapi-fetch` for a typed client generated from the same spec; Tailwind + shadcn/ui; React Hook Form + Zod. On a 401 the app returns to the sign-in screen. The current session endpoint provides permissions for showing or hiding UI elements. Routes: `/login`, `/forgot-password`, `/reset-password/:token`, `/invite/:token`, and an authenticated shell with `/settings/locations`, `/settings/users` and `/settings/audit`.

### D12. Local developer workflow

- `docker-compose.yml`: Postgres 17 on host port 5433 (5432 is commonly taken; an init script creates `dental_owner` and `dental_app`), SeaweedFS as the S3-compatible object store on 8333 (unused until Phase 6; MinIO was planned but no longer publishes public images), Mailpit (SMTP on 1025, web UI on 8025).
- `Taskfile.yml` (go-task, cross-platform, so Windows doesn't need `make`):

  | Command | What it does |
  |---|---|
  | `task up` / `task down` | Start / stop the containers |
  | `task gen` | Run sqlc, oapi-codegen and openapi-typescript |
  | `task migrate` | Run database migrations |
  | `task seed` | Create the demo data |
  | `task dev` | Run the API on :8080 and Vite on :5173 |
  | `task test` | Run the tests |
  | `task lint` | Run the linters |

- Configuration comes from environment variables, with a documented `.env.example`.
- Generated code is committed, so a fresh clone builds without running codegen. A `task gen` check later catches drift.
- `dentalctl seed` creates the organisation "Demo Dental", locations "Sydney CBD" and "Parramatta" (Australia/Sydney), and the owner `owner@demo.local`, with a password printed to the console. It is idempotent.

### D13. Testing

- Go tests use testcontainers-go to start Postgres once per package, run migrations, and connect as `dental_app`, so RLS is exercised exactly as in production.
- A dedicated `isolation` test suite plus the RLS guard test from D5.
- HTTP-level tests drive the generated server with `httptest` to cover cookie, Origin, problem+json and pagination behaviour.
- Frontend: Vitest + Testing Library for components with logic, such as the auth guard and the permission-gated UI. No end-to-end browser tests in this change.

## Risks / Trade-offs

- [RLS bypass through SECURITY DEFINER functions] → Keep them few and narrow (lookup by exact key only), pin `search_path`, revoke `PUBLIC` execute and grant only to `dental_app`, and test each one.
- [Developer forgets `InTenantTx` and uses the raw pool] → Not exported from `platform/db`. The only exported query paths are `InTenantTx` and `InAuthTx` (auth functions only).
- [Forgotten audit call on a new mutation] → HTTP test helper `assertAudited(action)` used in every mutating endpoint test. A review checklist item lives in the OpenSpec task rules.
- [Delta sync misses rows from long-running transactions] → The server-provided `sync_until` margin; document that clients must use it rather than their own clock.
- [Session check per request adds database load] → One indexed lookup joined with membership; acceptable at this scale. Cache later if needed.
- [Fixed roles may not fit every practice] → Permissions are checked by name, so custom roles become a data change later, not a code rewrite.
- [Committed generated code causes merge noise] → Generated files live in clearly named directories and are marked `linguist-generated` in `.gitattributes`.
- [Windows development quirks with testcontainers and line endings] → Require Docker Desktop. `.gitattributes` enforces LF for `*.sql`, `*.go` and `*.sh`.

## Migration Plan

Greenfield. No existing data or deployments. Rollback means `goose down` locally.

## Open Questions

- Multi-factor authentication: required before any real patient data is stored. Planned as its own change; the session design already allows adding a "pending MFA" step.
- Whether idle timeout should become a per-organisation setting. The default of 60 minutes stands for now.
