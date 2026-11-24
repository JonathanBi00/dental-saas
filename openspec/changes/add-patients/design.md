# Design

## Context

See proposal.md for why. This change copies the Phase 0 patterns (archived as `2026-11-20-add-platform-foundation`, decisions D1–D13):
- sqlc queries run inside `db.InTenantTx`;
- every `org_id` table has row-level security;
- `audit.Record` writes in the same transaction as the change;
- permissions are checked by name inside services;
- `api/openapi.yaml` is edited first.

`internal/tenancy` locations are the closest existing example. They have create, get, PATCH, `POST …/archive`, and a list with `include_archived`, cursor pagination and `updated_since`. Optional text columns are `NOT NULL DEFAULT ''`, so clearing a field stores an empty string.

`add-reference-data` (Phase 1) is being built at the same time and adds `00005_reference.sql`, six permissions and three settings screens. This change touches the same files in a few places (see P9) but uses none of Phase 1's tables.

Patients are the first personal health information in the system. Until now, the audit log only recorded changes. This change starts auditing reads too.

## Goals / Non-Goals

**Goals:**
- A patient resource that follows the locations pattern, so scheduling, clinical and billing can reference `patients.id` without special cases.
- Read auditing that can't be bypassed by forgetting a call in a handler.
- Search that's fast enough for one practice's patient list (tens of thousands of rows) without new infrastructure.

**Non-Goals:**
- Fuzzy, phonetic or full-text search.
- Any location-based restriction on seeing patients.
- Encrypting columns in the database. Patients have no government identifiers yet. Revisit when Medicare and DVA numbers arrive in Phase 5.
- Multi-factor authentication. It's a separate `add-mfa` change and has to be done before real data is used.

## Decisions

### P1. One `internal/patients` package
It has its own `Service`, with the same shape as `tenancy.Service`. Patients will gain related tables later, such as contacts and alerts, so they get their own package from the start rather than sharing `internal/reference`.

### P2. Tables
Migration `00006_patients.sql` creates two tables.

**`patients`** has these columns:
- `id`, `org_id`, `patient_number int`;
- `title`, `first_name`, `middle_names`, `last_name`, `preferred_name`;
- `date_of_birth date`, `sex`;
- `mobile_phone`, `home_phone`, `email`;
- `address_line1`, `address_line2`, `suburb`, `state`, `postcode`;
- `home_location_id NULL REFERENCES locations`;
- `archived_at`, `created_at`, `updated_at`.

It has these constraints:
- Optional text columns are `NOT NULL DEFAULT ''`, as in locations.
- `sex` defaults to `'not_stated'` and has a CHECK constraint.
- CHECKs on `last_name` (not blank), `state`, `postcode` and the date-of-birth range, mirroring the spec. The service validates first, so these are a backstop.
- A unique index on `(org_id, patient_number)`, not partial, so numbers are never reused.

It has these indexes:
- **For listing:** `(org_id, last_name, first_name, id)`.
- **For delta sync:** `(org_id, updated_at, id)`.
- **For search** (see P5): `(org_id, lower(last_name) text_pattern_ops)`, `(org_id, lower(first_name) text_pattern_ops)`, `(org_id, lower(preferred_name) text_pattern_ops)` and `(org_id, date_of_birth)`.
- **For the duplicate check:** `(org_id, lower(btrim(first_name)), lower(btrim(last_name)), date_of_birth) WHERE archived_at IS NULL`. It's not unique, because a confirmed duplicate is allowed.

**`patient_number_counters`** has `(org_id PRIMARY KEY, last_number int NOT NULL)`.

Both tables get `ENABLE` + `FORCE ROW LEVEL SECURITY` and the standard `org_id = app_org_id()` policy. `TestRLSGuard` covers them automatically.

### P3. Patient numbers from a counter row
Inside the create transaction:

```sql
INSERT INTO patient_number_counters (org_id, last_number) VALUES ($1, 1001)
ON CONFLICT (org_id) DO UPDATE SET last_number = patient_number_counters.last_number + 1
RETURNING last_number
```

The row lock serialises concurrent creates within one organisation, and a rolled-back create rolls the counter back too, so the numbers have no gaps from failures.

*Alternatives:*
- **`MAX(patient_number) + 1`:** it races under concurrent creates and would reuse numbers if rows were ever removed.
- **A Postgres sequence per organisation:** it needs DDL at organisation creation and leaves gaps on rollback.

### P4. Duplicate check in the service
Before inserting, the service runs a query using the partial index from P2, in the same transaction. If any matches come back and `confirm_not_duplicate` isn't true, it returns a 409 problem with `type` `…/possible-duplicate` and a `matches` array of `{id, patient_number, first_name, last_name, date_of_birth}`. That's the minimum the UI needs to say "did you mean…?".

Two people creating the same patient at the same instant can both get through. That's acceptable: the check is a warning, not a constraint.

### P5. Search
The list endpoint takes `q`. The service trims it and builds one query:
- **Date:** if `q` parses as `YYYY-MM-DD`, it matches `date_of_birth = q`.
- **All digits:** if `q` is digits only (after dropping spaces, brackets, hyphens and a leading `+`), it matches `patient_number = q`. With 6 or more digits, it also matches phone numbers by their digits.
- **Anything else:** it matches the start of `lower(first_name)`, `lower(last_name)` or `lower(preferred_name)` with `LIKE lower(q) || '%'`, after escaping `%`, `_` and `\`.

Phone digits are kept in two generated columns, `mobile_digits` and `home_digits` (`regexp_replace(mobile_phone, '\D', '', 'g')`), so search doesn't re-parse every row. Substring matching on these columns isn't indexed. That's fine at practice scale because `org_id` narrows it first, and we can revisit with `pg_trgm` if it gets slow.

The results use the normal list ordering of last name, first name and id, with cursor pagination. `q` can't be combined with `updated_since`, which returns 422, because the two need different orderings.

### P6. Read auditing inside the service
`Get` and `List` in `internal/patients` call `audit.Record` inside the same `InTenantTx` that reads the rows. Handlers never read patients any other way, and the store queries are only called from this package.
- **Get** records `patient.viewed` with the target patient.
- **List** records `patient.listed` with target type `patient`, no target id, and details `{q, include_archived, updated_since, home_location_id, limit, returned}`.

If the audit insert fails, the transaction rolls back and the handler returns 500 with no data, as the spec requires.

*Alternative:* HTTP middleware that audits by route. Rejected because it can't know the result count, and an error after the response has started could leak data without an audit row.

Change auditing uses the existing `audit.Changes` diff, so `patient.updated` records changed fields with old and new values. These values are personal information, but the audit log is only readable with `audit:read` (owner and admin), who can already read patients. `audit.Sanitize` needs no change because no secret-named fields are involved.

### P7. Permissions and visibility
`internal/access` gains `PermPatientsRead = "patients:read"` for every role and `PermPatientsManage = "patients:manage"` for owner, admin and front_desk.

The service ignores location restrictions for patients, which the spec requires. That's consistent with the "Location scoping" requirement, because patients aren't location-scoped data. The home location filter on the list (`home_location_id`) is a plain filter, not a permission check.

### P8. API
- `GET /patients` takes `cursor`, `limit`, `updated_since`, `include_archived`, `home_location_id` and `q`.
- `POST /patients` takes the patient fields plus `confirm_not_duplicate`.
- `GET /patients/{id}`.
- `PATCH /patients/{id}`: absent means unchanged, and `null` clears the field. Required fields and `patient_number` are rejected.
- `POST /patients/{id}/archive` and `POST /patients/{id}/restore`.

Restoring a patient that isn't archived, or archiving one that already is, returns the patient unchanged with 200, so retries are safe. `date_of_birth` is a `format: date` string, not a timestamp, so it never shifts across timezones.

### P9. Sharing files with Phase 1
- **Migration number:** the new migration is `00006_patients.sql`. If `add-reference-data` is renumbered or dropped, renumber this one before merging. goose needs no gap in versions.
- **Shared files:** `internal/access/access.go`, the `access` tests, `internal/seed/seed.go`, the frontend nav and `api/openapi.yaml`. Both changes only add new entries to these, so a rebase should only need to keep both sides.
- **Merge and archive order:** `add-reference-data` merges and archives first, and this change rebases onto it before its own archive commit.

### P10. Frontend
- **List:** a top-level `Patients` nav item, outside Settings, shown with `patients:read`.
- **`/patients`:** a debounced search box, a table showing patient number, name (with the preferred name), date of birth with age, mobile and home location, plus a "Show archived" toggle and a "New patient" button shown with `patients:manage`.
- **Create:** a dialog with React Hook Form and Zod. A 409 `possible-duplicate` response shows the matches with links, and a "This is a different person" button that resubmits with `confirm_not_duplicate`.
- **`/patients/$patientId`:** a read view of the details and a sticky header with number, name and age. With `patients:manage`, it also has an edit form and archive or restore.
- **Helpers:** age is calculated from the date of birth in the browser, using a shared helper that's unit-tested across birthdays and leap days.

Opening a patient page and loading a search page each create an audit row. TanStack Query's default refetch on window focus would multiply those rows, so patient queries set `refetchOnWindowFocus: false` and a `staleTime` of 30 seconds.

### P11. Seed
Twenty patients with fixed ids, numbers 1001 to 1020, and plainly fictional names and details:
- dates of birth from 1940 to 2020;
- mobile numbers taken only from ACMA's published list of numbers reserved for fiction (for example `0491 570 006`), never made-up `04xx` numbers that could belong to a real person;
- emails `@example.com`;
- suburbs near each demo location.

One pair shares first name, last name and date of birth (the second created with confirmation) so the duplicate UI can be demonstrated, and one patient is archived. Running the seed again leaves the counter at 1020.

## Risks / Trade-offs

- **[No MFA yet]** A stolen password exposes every patient. → Use demo data only. `add-mfa` must be merged before real patient data, and PROGRESS.md keeps this as an open item.
- **[Audit volume]** Every search keystroke could write an audit row. → The UI debounces search (300 ms) and doesn't refetch on focus. An audit row per settled search is acceptable and is useful for spotting people browsing records they shouldn't.
- **[Search quality]** Prefix-only name search misses typos and middle-of-name matches. → It's documented as MVP. `pg_trgm` or phonetic matching can be added later without changing the API.
- **[Duplicate race]** Two simultaneous creates of the same person both succeed (P4). → Merging duplicates is a planned follow-up.
- **[Organisation-wide visibility]** A member restricted to one clinic can see patients of every clinic. → This is deliberate (see proposal), and every view is audited. Revisit if a customer runs clinics as separate businesses. Those should be separate organisations anyway.
- **[Clash with Phase 1]** Edits to the same files and migration numbering (P9). → Archive in order and rebase. Neither change modifies a requirement the other modifies.

## Migration Plan

This is additive only: one new migration with two new tables and no changes to existing ones. Rollback is `task migrate -- down` to version 00005, which drops both tables. Sessions pick up the new permissions on their next request, because permissions come from the role at request time.
