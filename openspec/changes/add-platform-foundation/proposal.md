# Proposal

## Why

The repository is empty, and every later domain (reference data, patients, scheduling, clinical, billing) needs the same base: a runnable local stack, a way to identify which practice a request belongs to, users who can sign in with the right permissions, an audit trail for health-information access, and consistent API conventions. Building this once, first, stops each domain from inventing its own version and makes tenant isolation a property of the platform rather than of each handler.

## What Changes

- Scaffold the monorepo: Go backend, React + Vite frontend, `api/openapi.yaml` as the contract, SQL migrations, and a Docker Compose stack (Postgres, S3-compatible object store, Mailpit) runnable on localhost with one command.
- Introduce **organisations** (a dental business, the tenant) and **locations** (its clinics), each location with its own timezone and contact details.
- Enforce tenant isolation in the database with Postgres row-level security, in addition to application checks.
- Add **user accounts** that can belong to one or more organisations, email + password sign-in, server-side sessions, sign-out, password reset, and invite-by-email onboarding.
- Add **role-based access control** with a fixed initial role set (owner, admin, practitioner, front desk) and per-location access.
- Add an append-only **audit log** that records security events and changes to tenant data, viewable by owners and admins.
- Establish **API conventions** used by every later endpoint: versioned base path, standard error format, cursor pagination, filtering, `updated_since` delta sync, request IDs, and health checks.
- Frontend shell: sign-in, password reset, accept-invite, an app layout with organisation and location switching, and admin screens for locations, users and the audit log.
- A seed command that creates a demo organisation, location and owner for local testing.

Out of scope: self-service sign-up and billing for the SaaS itself, multi-factor authentication, single sign-on, public API keys for third-party integrators, custom roles, and any dental domain data (patients, appointments, etc.).

## Capabilities

### New Capabilities

- `tenancy`: Organisations and their locations, tenant context resolution for every request, and guaranteed data isolation between organisations.
- `user-auth`: User identities, organisation membership, sign-in/sign-out, sessions, password reset, and invitations.
- `access-control`: Roles, permissions and location scoping that decide what an authenticated member may do.
- `audit-log`: Append-only record of security events and tenant data changes, and how authorised users can view it.
- `api-conventions`: Cross-cutting HTTP API behaviour: versioning, error format, pagination, filtering, delta sync, request tracing and health endpoints.

### Modified Capabilities

None. This is the first change in the project.

## Impact

- **New code:** `backend/` (Go module), `frontend/` (React app), `api/openapi.yaml`, `db/migrations/`, `docker-compose.yml`, `Taskfile.yml`, and a root README for local setup.
- **New dependencies:** Go: pgx, sqlc (codegen), goose, oapi-codegen, argon2 (golang.org/x/crypto), testcontainers-go. Frontend: React, Vite, TanStack Query/Router, Tailwind, shadcn/ui, openapi-typescript. Containers: postgres, seaweedfs (S3-compatible), mailpit.
- **Developer machine requirements:** Go 1.26+, Node 22+, Docker.
- Every later change builds on these conventions; changing them afterwards would be a cross-cutting migration.
