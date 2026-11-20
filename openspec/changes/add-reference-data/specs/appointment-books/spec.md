# Spec Delta

## Purpose

Defines the bookable diary columns at each location (usually one per chair or surgery) and the weekly hours each is open, which scheduling will use to place and find appointments.

## ADDED Requirements

### Requirement: Appointment book records
The system SHALL let an organisation store appointment books, each belonging to exactly one non-archived location, with a name and a display order. Book names SHALL be unique among a location's non-archived books. Books SHALL be archivable rather than deleted.

#### Scenario: Create a book
- **WHEN** an owner or admin creates a book named "Chair 1" at Parramatta
- **THEN** the book is stored under that location and returned with its identifier, location and display order

#### Scenario: Duplicate name at the same location
- **GIVEN** Parramatta has a non-archived book named "Chair 1"
- **WHEN** another book named "Chair 1" is created at Parramatta
- **THEN** the system rejects the request with 409

#### Scenario: Archived location
- **WHEN** a book is created at a location that is archived or belongs to another organisation
- **THEN** the system rejects the request with a 422 validation error on the location field

#### Scenario: Archive a book
- **WHEN** an owner or admin archives a book
- **THEN** the book no longer appears in default book lists but remains retrievable by identifier and in lists that request archived items

### Requirement: Default practitioner
A book SHALL optionally name a default practitioner, who MUST be a non-archived practitioner assigned to the book's location.

#### Scenario: Practitioner works elsewhere
- **GIVEN** practitioner B works only at Sydney CBD
- **WHEN** B is set as the default practitioner of a Parramatta book
- **THEN** the system rejects the request with a 422 validation error on the default practitioner field

### Requirement: Weekly opening hours
A book SHALL have weekly opening hours: for each weekday, zero or more time ranges in the location's local time. Each range MUST start before it ends, use whole minutes in steps of 5, and not overlap another range on the same day. A weekday with no ranges is closed.

#### Scenario: Split day
- **WHEN** an owner or admin sets Monday's hours to 08:00–12:30 and 13:30–17:00 and leaves Sunday empty
- **THEN** the book is returned with those two Monday ranges and Sunday closed

#### Scenario: Overlapping ranges
- **WHEN** a day's hours are set to 08:00–12:00 and 11:30–17:00
- **THEN** the system rejects the request with a 422 validation error on the hours field

#### Scenario: Range ends before it starts
- **WHEN** a range is set to 17:00–08:00
- **THEN** the system rejects the request with a 422 validation error on the hours field

#### Scenario: Hours follow the location's timezone
- **GIVEN** a book at a location with timezone "Australia/Perth"
- **WHEN** its Monday hours are set to 08:00–17:00
- **THEN** the hours are stored and returned as 08:00–17:00 local time, without conversion to UTC

### Requirement: Books are scoped to locations
Members SHALL be able to list and view books, optionally filtered to one location, ordered by location then display order. A member restricted to certain locations MUST only see books at those locations; any other book MUST be treated as not found. Lists SHALL follow the standard pagination and delta-sync conventions.

#### Scenario: Restricted member lists books
- **GIVEN** a front_desk member restricted to Parramatta, and books at both Parramatta and Sydney CBD
- **WHEN** they list books
- **THEN** only Parramatta's books are returned

#### Scenario: Restricted member opens a book elsewhere
- **WHEN** a member restricted to Parramatta requests a Sydney CBD book by identifier
- **THEN** the system responds with 404

### Requirement: Appointment book changes are audited
Creating, updating, archiving and changing the hours of a book SHALL each write an audit entry with the changed fields.

#### Scenario: Hours change is audited
- **WHEN** an admin changes a book's Friday hours
- **THEN** an audit entry records action "appointment_book.updated" with the old and new Friday hours
