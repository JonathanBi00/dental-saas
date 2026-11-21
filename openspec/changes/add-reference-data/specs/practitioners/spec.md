# Spec Delta

## Purpose

Records the clinicians who treat a practice's patients, which locations they work at, and the provider number they use at each, so appointments and billing can name a responsible practitioner.

## ADDED Requirements

### Requirement: Practitioner records
The system SHALL let an organisation store practitioners, each with a display name, a profession (dentist, specialist, hygienist, oral_health_therapist, dental_therapist or prosthetist), an optional AHPRA registration number and an optional diary colour. Practitioners SHALL be archivable rather than deleted.

#### Scenario: Create a practitioner
- **WHEN** an owner or admin creates a practitioner named "Dr Priya Shah" with profession dentist
- **THEN** the practitioner is stored under their organisation and returned with its identifier

#### Scenario: Missing or unknown profession
- **WHEN** a practitioner is created or updated with an empty name or a profession that is not in the allowed list
- **THEN** the system rejects the request with a 422 validation error on that field

#### Scenario: Invalid diary colour
- **WHEN** a practitioner is created or updated with a diary colour that is not a six-digit hex colour such as "#3A7BD5"
- **THEN** the system rejects the request with a 422 validation error on the colour field

#### Scenario: Archive a practitioner
- **WHEN** an owner or admin archives a practitioner
- **THEN** the practitioner no longer appears in default practitioner lists but remains retrievable by identifier and in lists that request archived items

### Requirement: Practitioner linked to a user account
A practitioner SHALL optionally be linked to the user account of an active member of the same organisation. A user SHALL be linked to at most one non-archived practitioner per organisation.

#### Scenario: Link a practitioner to a member
- **WHEN** an owner or admin links a practitioner to the user account of an active member
- **THEN** the practitioner is returned with that user's identifier

#### Scenario: Link to a non-member
- **WHEN** a practitioner is linked to a user who is not an active member of the organisation
- **THEN** the system rejects the request with a 422 validation error on the user field

#### Scenario: User already linked
- **GIVEN** a user already linked to a non-archived practitioner in the organisation
- **WHEN** another practitioner in that organisation is linked to the same user
- **THEN** the system rejects the request with 409

### Requirement: Practitioner locations and provider numbers
A practitioner SHALL be assigned to one or more of the organisation's non-archived locations. Each assignment SHALL carry an optional provider number, because provider numbers are issued per practice location.

#### Scenario: Assign locations with provider numbers
- **WHEN** an owner or admin sets a practitioner's locations to Sydney CBD with provider number "2345678A" and Parramatta with no provider number
- **THEN** the practitioner is returned with both locations and their provider numbers

#### Scenario: No locations
- **WHEN** a practitioner is created or updated with an empty location list
- **THEN** the system rejects the request with a 422 validation error on the locations field

#### Scenario: Unknown or archived location
- **WHEN** a practitioner is assigned to a location that does not exist in the organisation or is archived
- **THEN** the system rejects the request with a 422 validation error on the locations field

### Requirement: Listing practitioners
Every member of the organisation SHALL be able to list and view its practitioners, optionally filtered to those who work at a given location. Lists SHALL follow the standard pagination and delta-sync conventions.

#### Scenario: Filter by location
- **GIVEN** practitioner A works at Parramatta and practitioner B works only at Sydney CBD
- **WHEN** a member lists practitioners filtered to Parramatta
- **THEN** only practitioner A is returned

#### Scenario: Restricted member lists practitioners
- **GIVEN** a front_desk member restricted to Parramatta
- **WHEN** they list practitioners without a filter
- **THEN** every non-archived practitioner of the organisation is returned, each showing only the location assignments the member can access

#### Scenario: Practitioner from another organisation
- **WHEN** a member requests a practitioner that belongs to a different organisation
- **THEN** the system responds with 404

### Requirement: Practitioner changes are audited
Creating, updating, archiving and changing the locations of a practitioner SHALL each write an audit entry with the changed fields.

#### Scenario: Provider number change is audited
- **WHEN** an admin changes a practitioner's provider number at Sydney CBD
- **THEN** an audit entry records action "practitioner.updated" with the old and new provider number for that location
