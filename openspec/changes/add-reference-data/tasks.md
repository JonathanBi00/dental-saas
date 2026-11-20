# Tasks

## 1. Schema and permissions

- [ ] 1.1 Add migration `00005_reference.sql` with the practitioners, practitioner_locations, procedure_codes and appointment_books tables, CHECK constraints, partial unique indexes, `updated_at` triggers and RLS policies (design R2); verify `task migrate` then `task migrate -- down` then `task migrate` succeeds and `TestRLSGuard` passes
- [ ] 1.2 Add the six permissions to `internal/access` and the role matrix (design R6); verify `access` tests assert every role has the three read permissions and only owner and admin have the manage permissions, and that the session endpoint test lists them for an owner

## 2. Practitioners

- [ ] 2.1 Add the practitioner endpoints (list, create, get, PATCH, archive) to `api/openapi.yaml` first, then sqlc queries, and run `task gen`; verify generation compiles
- [ ] 2.2 Implement practitioners in `internal/reference` with validation (name, profession, AHPRA number, colour), user linking to active members, location set replacement with provider numbers, scope-filtered assignments, archive and audit entries with location diffs; verify service tests cover each scenario in `specs/practitioners/spec.md`, including 409 for a doubly linked user
- [ ] 2.3 Wire the handlers and add API tests for permissions (front desk can read, practitioner gets 403 on create), the location filter, pagination with `updated_since` after a location change, and cross-organisation 404; verify `go test ./internal/api/...` passes
- [ ] 2.4 Add tenant isolation tests for `practitioners` and `practitioner_locations` (organisation B cannot read, insert or update A's rows through the app role); verify they pass

## 3. Procedure codes

- [ ] 3.1 Add the procedure code endpoints (list with `q`, create, get, PATCH, archive) to `api/openapi.yaml` first, then sqlc queries, and run `task gen`; verify generation compiles
- [ ] 3.2 Implement procedure codes with item number, fee and duration validation, 409 on a duplicate active item number, reuse after archive, item-number ordering, search and audit entries; verify service tests cover each scenario in `specs/procedure-codes/spec.md`
- [ ] 3.3 Wire the handlers and add API tests for permissions (practitioner gets 403 on PATCH), search, pagination and cross-organisation 404; verify `go test ./internal/api/...` passes
- [ ] 3.4 Add a tenant isolation test for `procedure_codes`, including that two organisations can both have item number "114"; verify it passes

## 4. Appointment books

- [ ] 4.1 Add the appointment book endpoints (list with `location_id`, create, get, PATCH, archive) and the weekly hours schema to `api/openapi.yaml` first, then sqlc queries, and run `task gen`; verify generation compiles
- [ ] 4.2 Implement opening hours parsing and validation (seven days, HH:MM, 5-minute steps, start before end, no overlap) as a pure function; verify table-driven unit tests cover split days, overlaps, reversed ranges and bad formats
- [ ] 4.3 Implement appointment books with location checks, unique names per location (409), default practitioner validation, location scoping (404 outside a restricted member's locations), ordering, archive and audit entries including hours diffs; verify service tests cover each scenario in `specs/appointment-books/spec.md`
- [ ] 4.4 Wire the handlers and add API tests for permissions, a restricted member's list and 404, the location filter and pagination; verify `go test ./internal/api/...` passes
- [ ] 4.5 Add a tenant isolation test for `appointment_books`; verify it passes

## 5. Seed

- [ ] 5.1 Extend `dentalctl seed` with the demo practitioners, about ten procedure codes in our own words and two books per location (design R8); verify running `task seed` twice creates no duplicates and the owner can list all three through the API

## 6. Frontend

- [ ] 6.1 Run `task gen` for TypeScript types and add Settings nav entries for Practitioners, Procedure codes and Appointment books, shown by read permission; verify a Vitest test that front desk sees the entries but no create buttons, and the owner sees both
- [ ] 6.2 Build Settings → Practitioners (list with location filter, create/edit dialog with profession, colour, linked member and per-location provider numbers, archive); verify by hand against the dev stack and with a Vitest test of the form's validation
- [ ] 6.3 Build Settings → Procedure codes (searchable list, create/edit dialog with fee entered in dollars, archive) with a dollars-to-cents helper; verify a Vitest test of the helper (e.g. "155.50" → 15550, "abc" rejected) and by hand against the dev stack
- [ ] 6.4 Build Settings → Appointment books (list grouped by location, create/edit dialog with default practitioner and the weekly hours editor, archive); verify a Vitest test of the hours editor's validation and by hand against the dev stack
- [ ] 6.5 Update `README.md` and `PROGRESS.md` for the new screens, permissions and seed data; verify the README's demo steps mention them and match what the app shows

## 7. End-to-end check

- [ ] 7.1 From a clean dev database run migrate → seed → dev, then as owner create a practitioner at Parramatta with a provider number, a procedure code and a book at Parramatta with split hours and that default practitioner; sign in as `frontdesk@demo.local` and confirm they see Parramatta's books only, can view all three lists and have no edit controls; confirm the audit log shows each change; then confirm `task test` and `task lint` pass

## Workflow follow-up

- Archive `add-reference-data` once reviewed, so its specs merge into `openspec/specs/`.
