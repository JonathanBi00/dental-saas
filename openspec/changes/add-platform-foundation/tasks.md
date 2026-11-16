# Tasks

## 1. Repository and local stack

- [x] 1.1 Create the monorepo layout from design D1, plus `.gitignore`, `.gitattributes` (LF for go/sql/sh, generated dirs marked `linguist-generated`) and `.env.example`; verify the expected directories exist
- [x] 1.2 Add `docker-compose.yml` with Postgres 17 (init script creating `dental_owner` and `dental_app` roles and the database), an S3-compatible object store (SeaweedFS) and Mailpit; verify `docker compose up -d` reports all three healthy and Mailpit's UI loads on localhost:8025
- [x] 1.3 Initialise the Go module in `backend/` with `cmd/dentalctl` (subcommands `serve`, `migrate`, `seed`, `org create`) and env-based config loading; verify `go build ./...` succeeds and `dentalctl --help` lists the subcommands
- [x] 1.4 Scaffold `frontend/` with Vite + React + TypeScript, Tailwind, shadcn/ui, TanStack Router and Query, with `/api` proxied to localhost:8080; verify `npm run build` succeeds and the dev server serves a placeholder page on localhost:5173
- [x] 1.5 Add `Taskfile.yml` with `up`, `down`, `gen`, `migrate`, `seed`, `dev`, `test` and `lint`; verify `task up` and `task dev` start the stack on Windows

## 2. Platform core (backend)

- [x] 2.1 Add the `platform/db` package: pgx pool as `dental_app`, `InTenantTx` setting `app.org_id`/`app.user_id` transaction-locally, and `InAuthTx` for auth functions, with no exported raw pool; verify with a unit test that settings don't leak between pooled transactions
- [x] 2.2 Add goose migration 0001: extensions, the `set_updated_at()` trigger function, and default privileges for `dental_app`; verify `dentalctl migrate up` then `down` then `up` succeeds against compose Postgres
- [x] 2.3 Add a testcontainers-go helper that starts Postgres, creates roles, runs migrations and returns an app-role pool; verify a smoke test passes with `go test ./...`
- [x] 2.4 Add the RLS guard test that fails for any table with an `org_id` column lacking FORCE RLS and a policy; verify it fails on a deliberately unprotected temp table and passes otherwise
- [x] 2.5 Add `api/openapi.yaml` skeleton (servers, cookie security scheme, Problem schema, pagination parameters and envelope, `updated_since`/`sync_until`) and wire oapi-codegen (strict, std-http) and sqlc into `task gen`; verify generation produces compiling code
- [x] 2.6 Implement `httpx`: problem+json error mapping, request-ID middleware, slog access logging, panic recovery, cursor encode/decode and limit validation; verify unit tests cover 422 field errors, generic 500s, a client-supplied request ID, and limit > 200 rejected
- [x] 2.7 Implement `/healthz`, `/readyz` and serving `/api/v1/openapi.yaml` (spec first); verify with an HTTP test that `/readyz` returns 503 when the database is stopped while `/healthz` returns 200

## 3. Tenancy: organisations and locations

- [x] 3.1 Migration: `organisations` (name, abn, status) and `locations` (org_id, name, address fields, phone, email, timezone, archived_at), with RLS and FORCE RLS; verify the guard test passes and an isolation test shows org B cannot select, update or insert org A's locations
- [x] 3.2 Add sqlc queries and the `tenancy` service for location create, update, archive, get and list (pagination, `include_archived`, `updated_since`) with IANA timezone validation; verify service tests for an invalid timezone (422), archive hidden from default list, and delta sync returning the archived row
- [x] 3.3 Define the location endpoints in `api/openapi.yaml`, then implement handlers (`GET/POST /api/v1/locations`, `GET/PATCH /api/v1/locations/{id}`, `POST /api/v1/locations/{id}/archive`); verify HTTP tests including 404 for another organisation's location
- [x] 3.4 Implement `dentalctl org create --name --owner-email`, which creates the organisation and an owner invitation; verify the invitation email appears in Mailpit

## 4. Identity: users, sessions, invitations, password reset

- [x] 4.1 Migration: `users` (citext email), `memberships` (org_id, user_id, role, status), `membership_locations`, `sessions`, `login_attempts`, `invitations`, `password_resets`, plus SECURITY DEFINER auth lookup functions with a pinned search_path; verify isolation tests for memberships/invitations, and that `dental_app` cannot select `users` across tenants except through the functions
- [x] 4.2 Implement argon2id hashing (PHC format) and the password policy (≥12 characters, not equal to email); verify unit tests for hash/verify round-trip and policy rejections
- [x] 4.3 Implement the session service: token generation, hashed storage, idle (60 min) and absolute (12 h) expiry, throttled `last_seen_at`, and revocation; verify tests using a fake clock cover idle expiry, sliding extension and the absolute cap
- [x] 4.4 Implement the auth middleware: cookie → session → active membership and organisation status re-check → tenant context in `context.Context`; verify tests for 401 on expired/revoked sessions, 403 for deactivated membership and 403 for a suspended organisation
- [x] 4.5 Implement the Origin / Sec-Fetch-Site check for unsafe methods; verify a POST with a foreign Origin returns 403 and makes no change
- [x] 4.6 Add the mail package (SMTP to Mailpit, plain-text and HTML templates for invite and reset); verify a test sends to a fake SMTP server and Mailpit receives mail in manual dev
- [x] 4.7 Define the auth endpoints in OpenAPI, then implement `POST /auth/login`, `POST /auth/logout` and `GET /auth/session` (user, memberships, active org/location, role, permissions, accessible locations) with dummy-hash timing and lockout after 5 failures; verify HTTP tests for identical 401s for an unknown email and a wrong password, a 429 on the sixth attempt, and auto-selection of a single organisation
- [x] 4.8 Define and implement `POST /auth/session/organisation` and `POST /auth/session/location` (active switching with membership and location checks); verify tests that switching to a non-member organisation or a restricted location returns 403 and leaves state unchanged
- [x] 4.9 Define and implement the password reset endpoints (request, complete) with single-use 60-minute tokens that revoke all of the user's sessions; verify tests for an identical response for unknown emails, token reuse rejected and other sessions revoked
- [x] 4.10 Define and implement the invitation endpoints (create, list pending, revoke, look up by token, accept) with 7-day single-use tokens and case-insensitive email matching to existing accounts; verify tests for new-user accept, existing-user accept (no duplicate account) and rejection of expired/used tokens

## 5. Access control

- [x] 5.1 Implement the `access` package: role→permission map (`locations:read`, `locations:manage`, `members:manage`, `audit:read`), a handler permission requirement, and accessible-location resolution; verify unit tests of the matrix for all four roles
- [x] 5.2 Apply permissions and location scoping to the location endpoints; verify HTTP tests that front_desk cannot create (403) and a restricted practitioner lists only their locations
- [x] 5.3 Define and implement the member endpoints (list members, change role, set location restrictions, deactivate/reactivate) with the owner-protection rules; verify tests that an admin cannot grant owner (403) and removing the last owner returns 409

## 6. Audit log

- [x] 6.1 Migration: `audit_events` with nullable org_id, INSERT/SELECT-only grants, a trigger rejecting UPDATE/DELETE and an RLS select policy; verify tests that `dental_app` UPDATE and DELETE fail and org B cannot read org A's events
- [x] 6.2 Implement `audit.Record(tx, event)` using request metadata from context, with field diffs that exclude secrets; verify a unit test that password hashes and tokens never appear in an entry
- [x] 6.3 Add audit calls to every auth event and mutation from groups 3–5, plus an `assertAudited` test helper; verify each existing mutating endpoint test asserts its audit action and that a rolled-back change leaves no audit row
- [x] 6.4 Define and implement `GET /api/v1/audit-events` (filters: date range, actor, action, target; newest first; paginated; `audit:read` required); verify HTTP tests for the actor filter and 403 for a practitioner

## 7. Frontend

- [ ] 7.1 Generate the typed API client with openapi-typescript + openapi-fetch, plus a TanStack Query setup that redirects to `/login` on 401; verify `task gen` produces the client and a Vitest test covers the 401 redirect
- [ ] 7.2 Build the sign-in, forgot-password, reset-password and accept-invite screens with form validation; verify manually against the dev stack (sign in as the seeded owner, complete a reset using the Mailpit link) and add Vitest tests for client-side validation
- [ ] 7.3 Build the authenticated app shell with organisation and location switchers and permission-gated navigation; verify a Vitest test that hides admin navigation for front_desk, and a manual switch between seeded locations
- [ ] 7.4 Build the Settings → Locations screen (list, create, edit, archive, show archived); verify manually that changes persist and appear in the audit log
- [ ] 7.5 Build the Settings → Users screen (members, pending invitations, invite, change role, restrict locations, deactivate); verify manually by inviting a user, accepting via Mailpit and signing in as them
- [ ] 7.6 Build the Settings → Audit log screen with filters and pagination; verify manually that the actions from 7.4 and 7.5 appear with the correct actor

## 8. Seed, docs and end-to-end check

- [x] 8.1 Implement idempotent `dentalctl seed` (Demo Dental, Sydney CBD and Parramatta, `owner@demo.local` with a printed password, plus one front_desk user restricted to Parramatta); verify running it twice creates no duplicates
- [ ] 8.2 Write the root README: prerequisites, `task up`/`migrate`/`seed`/`dev`, URLs (app, API, Mailpit, MinIO), seeded credentials and the module/RLS conventions for new domains; verify by following it from a clean clone
- [ ] 8.3 End-to-end check: from a clean database run up → migrate → seed → dev, sign in as owner, create a location, invite and onboard a user, sign in as the restricted user and confirm they see only Parramatta and no admin menus; then confirm `task test` and `task lint` pass

## Workflow follow-up

- Run `openspec validate add-platform-foundation --strict` before applying.
- Archive the change once implemented and reviewed (`/openspec-archive-change`), which creates the main specs for tenancy, user-auth, access-control, audit-log and api-conventions.
