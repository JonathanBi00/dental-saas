# tenancy Specification

## Purpose

Defines organisations (the tenant: a dental business) and their locations (clinics), how each request is bound to exactly one organisation, and the guarantee that one organisation's data is never visible to or modifiable by another.

## Requirements

### Requirement: Organisation records
The system SHALL store each organisation with a unique identifier, a display name, an optional ABN, and a status of active or suspended. Organisations are created by platform operators (via the seed/admin command), not through self-service sign-up.

#### Scenario: Create an organisation via the admin command
- **WHEN** an operator runs the organisation-create command with a name and an owner email
- **THEN** the system creates an active organisation and sends the owner an invitation to join it as owner

#### Scenario: Suspended organisation
- **GIVEN** an organisation whose status is suspended
- **WHEN** any of its members makes an authenticated API request scoped to it
- **THEN** the system rejects the request with a 403 error identifying the organisation as suspended

### Requirement: Location records
The system SHALL allow an organisation to have one or more locations, each with a name, address, phone number, email, and an IANA timezone. Locations SHALL be archivable rather than deleted.

#### Scenario: Create a location
- **WHEN** an owner or admin creates a location with a name and timezone "Australia/Sydney"
- **THEN** the location is stored under their organisation and returned with its identifier and timezone

#### Scenario: Invalid timezone
- **WHEN** a location is created or updated with a timezone that is not a valid IANA zone name
- **THEN** the system rejects the request with a 422 validation error on the timezone field

#### Scenario: Archive a location
- **WHEN** an owner or admin archives a location
- **THEN** the location no longer appears in default location lists but remains retrievable by identifier and in lists that request archived items

### Requirement: Active organisation per request
Every authenticated API request that touches tenant data SHALL be bound to exactly one active organisation, taken from the user's session, and the user MUST be an active member of that organisation.

#### Scenario: Switch active organisation
- **GIVEN** a user who is a member of organisations A and B, with A active
- **WHEN** the user switches the active organisation to B
- **THEN** subsequent requests return only organisation B's data

#### Scenario: Switch to an organisation the user does not belong to
- **WHEN** a user attempts to switch to an organisation where they have no active membership
- **THEN** the system rejects the request with a 403 error and the active organisation is unchanged

### Requirement: Tenant data isolation
Data belonging to one organisation SHALL NOT be readable, creatable, modifiable or deletable through any request bound to a different organisation. Isolation MUST be enforced by the database itself, so that a missing application-level filter cannot leak data.

#### Scenario: Read another organisation's record by identifier
- **GIVEN** a location that belongs to organisation A
- **WHEN** a user whose active organisation is B requests that location by its identifier
- **THEN** the system responds with 404 Not Found, without revealing that the record exists

#### Scenario: List endpoints only return the active organisation's records
- **GIVEN** organisations A and B each have locations
- **WHEN** a user whose active organisation is A lists locations
- **THEN** only organisation A's locations are returned

#### Scenario: Query without tenant context
- **WHEN** application code queries a tenant-owned table without an organisation set on the database session
- **THEN** the database returns no rows and rejects inserts, rather than returning data from any organisation
