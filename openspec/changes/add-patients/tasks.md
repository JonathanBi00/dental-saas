# Tasks

## 0. Before starting

- [ ] 0.1 Check whether `add-reference-data` has merged. If it has, rebase this branch onto `main`. If it hasn't, keep `00006` as the migration number and confirm `00005` is still Phase 1's (design P9). Verify that `task migrate` runs all migrations in order on a fresh database.

## 1. Schema and permissions

- [ ] 1.1 Add migration `00006_patients.sql` (design P2). It contains:
  - the `patients` and `patient_number_counters` tables;
  - the CHECK constraints, generated `mobile_digits` and `home_digits` columns, indexes and the partial duplicate-check index;
  - the `updated_at` trigger and RLS policies.

  Verify that `task migrate`, then `task migrate -- down`, then `task migrate` all succeed, and that `TestRLSGuard` passes.
- [ ] 1.2 Add `patients:read` (every role) and `patients:manage` (owner, admin, front_desk) to `internal/access` (design P7). Verify that the `access` tests assert the matrix, including that practitioners lack `patients:manage`, and that the session endpoint test lists both permissions for a front_desk member.
- [ ] 1.3 Add tenant isolation tests for `patients` and `patient_number_counters`. Verify that organisation B can't read, insert or update A's rows through the app role, and that the tests pass.

## 2. Patient service

- [ ] 2.1 Add the patient endpoints to `api/openapi.yaml` first (design P8):
  - list with `q`, `home_location_id`, `include_archived`, `cursor`, `limit` and `updated_since`;
  - create with `confirm_not_duplicate`;
  - get, PATCH, archive and restore;
  - the `possible-duplicate` problem with its `matches`.

  Then add the sqlc queries and run `task gen`. Verify that generation compiles and the served OpenAPI document includes `/patients`.
- [ ] 2.2 Write patient validation as pure functions:
  - required last name and date of birth;
  - the date-of-birth range;
  - sex values, email, phone characters, state and postcode;
  - trimming, and PATCH rules (absent keeps, null clears, required fields and `patient_number` rejected).

  Verify with table-driven unit tests covering each rule in `specs/patients/spec.md` under "Patient field validation" and "Updating patients".
- [ ] 2.3 Implement create in `internal/patients`, covering home location validation, patient number assignment through the counter (design P3), the duplicate check returning 409 with matches unless confirmed (design P4), and a `patient.created` audit entry. Verify with service tests for sequential and per-organisation numbers, concurrent creates getting distinct numbers, a duplicate rejected then accepted with confirmation, an archived match not counting, and a home location from another organisation or archived being rejected.
- [ ] 2.4 Implement get, PATCH, archive and restore with `patient.viewed`, `patient.updated` (field diffs), `patient.archived` and `patient.restored` audit entries written in the same transaction (design P6). Archive and restore must be safe to retry. Verify with service tests for each scenario under "Updating patients" and "Archive and restore", and in the audit-log delta, including that a failed audit write rolls back the read.
- [ ] 2.5 Implement list and search (design P5):
  - ordering and cursor pagination;
  - `include_archived` and `home_location_id`;
  - `updated_since`, which is rejected with 422 when combined with `q`;
  - date, number, phone-digit and name-prefix matching with `LIKE` escaping;
  - one `patient.listed` audit entry with the result count.

  Verify with service tests for every scenario under "Listing and searching patients", including that a 4-digit term doesn't match phones, and that `%` and `_` in a search are treated literally.
- [ ] 2.6 Wire the handlers. Add API tests for:
  - permissions: a practitioner can read but gets 403 on create, PATCH, archive and restore, and front desk can create;
  - a location-restricted member finding a patient with another home location;
  - pagination with `updated_since` after an archive;
  - 404 across organisations for get, PATCH, archive and restore;
  - the 409 problem body.

  Verify that `go test ./internal/api/...` passes.

## 3. Seed

- [ ] 3.1 Extend `dentalctl seed` with the 20 fictional patients from design P11: fixed ids, numbers 1001 to 1020, mobiles only from ACMA's published fiction list, `@example.com` emails, one confirmed duplicate pair and one archived patient. Verify that running `task seed` twice creates no duplicates, leaves the counter at 1020, and that the owner can search the patients through the API.

## 4. Frontend

- [ ] 4.1 Run `task gen` for the TypeScript types. Add the top-level Patients nav item shown with `patients:read`, and an age-from-date-of-birth helper. Verify with a Vitest test that every role sees the nav item, and with helper tests across birthdays and 29 February.
- [ ] 4.2 Build `/patients`:
  - a debounced (300 ms) search, and the table from design P10;
  - a "Show archived" toggle, and cursor-based "Load more";
  - `refetchOnWindowFocus: false`;
  - a "New patient" button only with `patients:manage`.

  Verify with a Vitest test that a practitioner sees no "New patient" button, and by hand against the dev stack that searching by name, number, phone and date of birth works.
- [ ] 4.3 Build the create dialog with Zod validation matching the API, and the duplicate flow: show matches with links on 409, then "This is a different person" resubmits with confirmation. Verify with a Vitest test of the form validation and the duplicate flow using a mocked 409, and by hand by creating a duplicate of a seeded patient.
- [ ] 4.4 Build `/patients/$patientId`: a header with number, name and age, the detail view, and an edit form, archive and restore with `patients:manage`. Verify with a Vitest test that a practitioner sees no edit controls, and by hand that editing, archiving and restoring all work and appear in Settings → Audit log.
- [ ] 4.5 Update `README.md` and `PROGRESS.md`:
  - the Patients screens, permissions and seed patients;
  - read auditing;
  - the reminder that `add-mfa` must land before real patient data.

  Verify that the README's demo steps mention the Patients screen and match what the app shows.

## 5. End-to-end check

- [ ] 5.1 Run these steps:
  1. From a clean dev database, run migrate, then seed, then dev.
  2. As `frontdesk@demo.local` (restricted to Parramatta), search for a seeded patient whose home location is the other clinic, open them, and change their mobile.
  3. Create a new patient, then try to create them again, and confirm the duplicate warning appears.
  4. As owner, archive and restore that patient.
  5. In Settings → Audit log, confirm `patient.listed`, `patient.viewed`, `patient.updated`, `patient.created`, `patient.archived` and `patient.restored` entries for both users.

  Then confirm that `task test` and `task lint` pass.

## Workflow follow-up

- Propose `add-mfa` as its own change. It must merge before any real patient data is stored.
- After `add-reference-data` is archived, rebase this branch, then run `openspec archive add-patients` as the last commit of this change's PR.
