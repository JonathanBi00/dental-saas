# Design

## Context

See proposal.md for why. Phase 0 established the patterns this change copies (archived as `2026-11-20-add-platform-foundation`, decisions D1–D13): sqlc queries run inside `db.InTenantTx`, RLS on every `org_id` table, `audit.Record` in the same transaction, permission checks by name inside services, and `api/openapi.yaml` first. Locations (`internal/tenancy`) are the closest existing example: create, get, PATCH, `POST …/archive`, list with `include_archived`, cursor pagination and `updated_since`. Location scoping is enforced in services (`getAccessible` returns not found outside the member's scope); RLS enforces the organisation only.

## Goals / Non-Goals

**Goals:**
- Three resources that follow the locations pattern exactly, so Phase 2 and 3 code can rely on them without special cases.
- Data shapes that scheduling (Phase 3) can use directly: book hours in location-local time, default durations on procedure codes.

**Non-Goals:**
- Any availability calculation (slots, rosters, holidays). Hours are stored and validated only.
- Linking procedure codes to fee schedules, health funds or tooth/surface rules.
- Bulk import or export of reference data.

## Decisions

### R1. One `internal/reference` package
Practitioners, procedure codes and appointment books live in one package with a file each (`practitioners.go`, `procedures.go`, `books.go`) and one `Service`. They are small, share validation helpers (location checks, archive handling) and books depend on practitioners.
*Alternative:* three packages. Rejected for now: more wiring for little separation. Easy to split later because each file only touches its own tables.

### R2. Tables
One migration, `00005_reference.sql`:
- `practitioners (id, org_id, user_id NULL, name, profession, ahpra_number, colour, archived_at, created_at, updated_at)`. `profession` has a CHECK constraint with the six values. Partial unique index on `(org_id, user_id) WHERE user_id IS NOT NULL AND archived_at IS NULL`.
- `practitioner_locations (org_id, practitioner_id, location_id, provider_number, PRIMARY KEY (practitioner_id, location_id))`.
- `procedure_codes (id, org_id, item_number, description, fee_cents bigint, duration_minutes int NULL, archived_at, …)`. Partial unique index on `(org_id, item_number) WHERE archived_at IS NULL`. CHECKs mirror the spec rules (regex, `fee_cents >= 0`, duration range and step).
- `appointment_books (id, org_id, location_id, name, display_order int, default_practitioner_id NULL, hours jsonb, archived_at, …)`. Partial unique index on `(location_id, lower(name)) WHERE archived_at IS NULL`.

All get `ENABLE` + `FORCE ROW LEVEL SECURITY` and the standard `org_id = app_org_id()` policy, and are therefore picked up by `TestRLSGuard`. Uniqueness violations map to 409 in the service, as memberships already do.

### R3. Opening hours as one JSON column
`hours` is stored on the book as `{"monday": [{"start": "08:00", "end": "12:30"}, …], …, "sunday": []}` with all seven keys present. The API uses the same shape. Hours are always read, replaced, validated and audited as one unit, so a child table would add joins, another RLS policy and diffing for no benefit. Validation (format, 5-minute steps, start < end, no overlap) happens in Go before writing.
*Alternative:* `appointment_book_hours (book_id, weekday, start_minute, end_minute)`. Better for SQL queries over hours, which Phase 3 can add later if slot search needs it; converting the JSON is a simple migration.
Times are wall-clock in the location's timezone (no UTC conversion), because "open 8 to 5" stays 8 to 5 across daylight saving changes.

### R4. Practitioner locations are replaced as a set
Create and PATCH accept `locations: [{location_id, provider_number}]`. A PATCH that includes `locations` replaces the whole set; the service diffs old and new for the audit entry and touches the practitioner's `updated_at` so delta sync sees location changes. Restricted members see only the assignments within their scope (the practitioner itself stays visible, since practitioners are organisation-wide).
*Alternative:* sub-resource endpoints per assignment. Rejected: more endpoints, and the settings form edits them together anyway.

### R5. Validation of cross-references in the service
"Location exists and is not archived", "user is an active member", and "default practitioner works at the book's location" are checked in the service inside the same transaction, returning 422 with the field name. Foreign keys still exist for integrity, but their errors are not the user-facing path.

### R6. Permissions
Six new permissions in `internal/access`: `practitioners:read|manage`, `procedure_codes:read|manage`, `appointment_books:read|manage`. Every role gets the three `read` permissions; owner and admin also get the three `manage` permissions. The frontend shows nav items and edit controls from the session's permission list, as Settings does today.

### R7. API
Per resource, mirroring locations: `GET /x` (list with `cursor`, `limit`, `updated_since`, `include_archived`, plus `location_id` for practitioners and books and `q` for procedure codes), `POST /x`, `GET /x/{id}`, `PATCH /x/{id}`, `POST /x/{id}/archive`. Paths: `/practitioners`, `/procedure-codes`, `/appointment-books`. Procedure codes list in item-number order; the `q` search uses `item_number LIKE q || '%' OR description ILIKE '%' || q || '%'`, which is fine at the size of one practice's list.

### R8. Seed
`dentalctl seed` adds, with fixed ids so re-runs change nothing: two practitioners (a dentist linked to `owner@demo.local` at both locations, a hygienist at Parramatta), about ten common procedure codes with descriptions in our own words and made-up demo fees, and "Chair 1" and "Chair 2" at each location open weekdays 08:00–17:00.

### R9. Frontend
Three routes under `_app/settings/` (practitioners, procedure-codes, appointment-books) following `locations.tsx`: list table, create/edit dialog with React Hook Form + Zod, archive action. The book form has a small weekly hours editor (one row per weekday, add/remove ranges). Money is entered in dollars and converted to cents in one helper.

## Risks / Trade-offs

- [ADA licensing] Item numbers and descriptions come from the ADA's copyrighted schedule. → Ship no schedule; seed only a small demo set in our own words, and check licensing before any default code list is offered to real practices.
- [Stale links] Archiving a location or practitioner does not clean up assignments or book defaults that point at it. → Lists and forms show the archived item as archived; scheduling (Phase 3) must ignore archived practitioners and books at archived locations. Revisit if it confuses users.
- [Deactivated members] A practitioner stays linked to a user whose membership is later deactivated. → Harmless for now (the link grants no access); validation only applies when the link is set.
- [JSON hours] Hours can't be queried efficiently in SQL. → Acceptable until slot search needs it (R3).

## Migration Plan

Additive only: one new migration with new tables and no changes to existing ones. Rollback is `task migrate -- down` (drops the new tables). Existing sessions pick up the new permissions on their next request because permissions are derived from the role at request time.
