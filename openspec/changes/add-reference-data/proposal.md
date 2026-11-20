# Proposal

## Why

Phase 0 gave us organisations, locations, users and permissions, but nothing a practice can book or bill against. Patients (Phase 2) and scheduling (Phase 3) both need to know who treats patients, where and when they can be booked, and what procedures the practice performs at what fee. This change adds that reference data as the thinnest slice those phases can build on.

## What Changes

- **Practitioners**: an organisation can record the clinicians who treat patients. Each has a name, profession (dentist, specialist, hygienist, oral health therapist, dental therapist, prosthetist), optional AHPRA registration number, optional diary colour, and can optionally be linked to a member's user account. A practitioner is assigned to one or more locations, with an optional provider number for each location (Medicare provider numbers are issued per location). Practitioners are archived, not deleted.
- **Procedure codes**: each organisation keeps its own list of procedure codes: an ADA-style item number (such as `011` or `114`), a description, a default fee in whole cents (AUD) and an optional default duration in minutes. Item numbers are unique among an organisation's active codes. Codes are archived, not deleted. New organisations start with an empty list; the demo seed adds a small starter set. We do not ship the full ADA Schedule of Dental Services.
- **Appointment books**: each location has one or more books, one per diary column (usually a chair or surgery). A book has a name, display order, an optional default practitioner who works at that location, and weekly opening hours: zero or more non-overlapping time ranges per weekday, in the location's local time. Books are archived, not deleted.
- **Permissions**: new named permissions to read and manage each kind of reference data. Every role can read; owners and admins can manage.
- **Web app**: Settings screens for Practitioners, Procedure codes and Appointment books (list, create, edit, archive).
- **Seed**: the demo organisation gets two practitioners, a starter set of procedure codes, and two appointment books per location.

Deferred as follow-ups (not in this change): rosters and practitioner availability, breaks and leave, public holidays, per-location or per-health-fund fee schedules, the full ADA schedule, procedure categories and tooth/surface rules, and importing reference data.

## Capabilities

### New Capabilities
- `practitioners`: clinician records, their link to user accounts, and the locations they work at with per-location provider numbers.
- `procedure-codes`: an organisation's list of billable procedure items with default fees and durations.
- `appointment-books`: bookable diary columns per location with weekly opening hours.

### Modified Capabilities
- `access-control`: the permission matrix gains read and manage permissions for practitioners, procedure codes and appointment books.

## Impact

- **Database**: one new migration with tables for practitioners, practitioner locations, procedure codes, appointment books and their opening hours. All are tenant-owned, so each gets row-level security and isolation tests.
- **API**: new endpoints under `/api/v1/practitioners`, `/api/v1/procedure-codes` and `/api/v1/appointment-books`, added to `api/openapi.yaml` first. List endpoints follow the existing pagination and delta-sync conventions.
- **Backend**: a new `internal/reference` package, sqlc queries, new permissions in `internal/access`, audit entries for every change.
- **Frontend**: three new Settings screens and nav entries, gated by the new permissions.
- **Seed**: `dentalctl seed` extended; still safe to re-run.
- **Session**: the permission list returned by the session endpoint grows; no shape change.
