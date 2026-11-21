# Proposal

Depends on: `add-reference-data`, for ordering only. This change uses the next migration number after Phase 1's `00005_reference.sql`, and adds to the same permission list, settings nav and seed files. It does not use practitioners, procedure codes or appointment books, so it can be built in parallel and archived straight after `add-reference-data`.

## Why

A practice can't book, chart or bill without knowing who the patient is. Patients are the first records that hold personal health information, so this change also sets how that information is protected: every view and change is audited, and the data is never hard-deleted. Scheduling (Phase 3), clinical records (Phase 4) and billing (Phase 5) all attach to a patient record, so this is the next thing to build.

## What Changes

- **Patient records**: an organisation can register patients with:
  - **Name:** title, first name, middle names, last name and preferred name. The last name is required.
  - **Personal details:** date of birth (required) and sex (`female`, `male`, `other` or `not_stated`).
  - **Contact details:** mobile phone, home phone and email.
  - **Address:** street lines, suburb, Australian state and a 4-digit postcode.
  - **Home location:** an optional location in the same organisation, for information only.
- **Patient numbers**: each new patient gets a sequential patient number, unique within the organisation and starting at 1001. It never changes and is never reused.
- **Possible duplicates**: creating a patient with the same first name, last name and date of birth as an existing active patient is rejected with 409 and the matching patients, unless the request confirms it on purpose.
- **Search and list**: a single search term matches the patient number, phone number, date of birth, or the start of the first, last or preferred name. Lists follow the existing pagination and delta-sync conventions.
- **Archive and restore**: patients are archived, never deleted. An archived patient can be restored. Australian health records have to be kept for years, so the API has no delete.
- **Access to patient records is audited**: viewing a patient, and every list or search of patients, writes an audit entry, as well as every create, update, archive and restore.
- **Permissions**: `patients:read` for every role, and `patients:manage` (create, edit, archive, restore) for owner, admin and front desk. Practitioners can read but not edit patient details in this change.
- **Organisation-wide records**: patients belong to the organisation, not a location. Members restricted to some locations can still find every patient, because patients move between a practice's clinics.
- **Web app**: a new top-level Patients section with a searchable list, a create form that handles the duplicate warning, a patient detail page with edit, and archive and restore.
- **Seed**: about 20 obviously fictional demo patients spread across both demo locations.

**Not in this change.** These are follow-ups, mostly owned by later phases:
- **Billing (Phase 5):** Medicare and DVA numbers, private health fund details.
- **Clinical (Phase 4):** medical alerts and history.
- **Not yet scheduled:** emergency contacts, family and guardian links, merging duplicates, a preferred practitioner, recalls, marketing consent, importing patients from another system, and full-text or fuzzy search.

**Before real patient data:** multi-factor authentication is still an open item from Phase 0, and it must land before this is used with real patients. That will be a separate change, `add-mfa`. This change is safe to build and demo with the seeded fictional data.

## Capabilities

### New Capabilities
- `patients`: patient demographic and contact records, patient numbers, duplicate detection, search, and archive or restore.

### Modified Capabilities
- `access-control`: adds a new requirement for the patient permissions. It does not modify the existing "Foundation permission matrix" requirement, which `add-reference-data` is already changing.
- `audit-log`: adds a new requirement that every read of patient records is audited. The existing "Recorded events" requirement is left unchanged.

Both are additions, so nothing existing breaks.

## Impact

- **Database:** one new migration, `00006_patients.sql`, with a `patients` table and a per-organisation patient number counter. Both are tenant-owned, so each gets row-level security and isolation tests. There's no hard delete.
- **API:**
  - New endpoints under `/api/v1/patients`: list and search, create, get, PATCH, archive and restore.
  - All are added to `api/openapi.yaml` first.
  - The 409 for a possible duplicate returns the matching patients in the problem body.
- **Backend:**
  - A new `internal/patients` package and sqlc queries.
  - Two new permissions in `internal/access`.
  - Audit entries for every read and change, written in the same transaction, so a failed audit write fails the request.
- **Frontend:** new routes `/patients` and `/patients/$patientId`, and a Patients nav item shown with `patients:read`.
- **Seed:** `dentalctl seed` is extended and is still safe to re-run.
- **Audit volume:** auditing reads adds one audit row per patient list, search or view. That's acceptable at practice scale.
