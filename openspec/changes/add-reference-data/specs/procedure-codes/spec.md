# Spec Delta

## Purpose

Holds each organisation's own list of procedures it performs, identified by ADA-style item numbers, with a default fee and duration that appointments, treatment plans and invoices will start from.

## ADDED Requirements

### Requirement: Procedure code records
The system SHALL let an organisation store procedure codes, each with an item number, a description, a default fee in whole cents (AUD) and an optional default duration in minutes. Procedure codes SHALL be archivable rather than deleted.

#### Scenario: Create a procedure code
- **WHEN** an owner or admin creates item number "114" described as "Removal of calculus" with a default fee of 15500 cents and a duration of 30 minutes
- **THEN** the code is stored under their organisation and returned with its identifier, fee 15500 and duration 30

#### Scenario: Archive a procedure code
- **WHEN** an owner or admin archives a procedure code
- **THEN** the code no longer appears in default lists but remains retrievable by identifier and in lists that request archived items

### Requirement: Procedure code validation
The item number SHALL be one to five digits, optionally followed by one uppercase letter (for example "011", "521" or "613A"). The description SHALL be non-empty. The default fee SHALL be zero or more whole cents. The duration, when given, SHALL be between 5 and 480 minutes in steps of 5.

#### Scenario: Invalid item number
- **WHEN** a code is created with item number "11-4" or an empty item number
- **THEN** the system rejects the request with a 422 validation error on the item number field

#### Scenario: Negative or fractional fee
- **WHEN** a code is created with a fee of -100 or 155.5
- **THEN** the system rejects the request with a 422 validation error on the fee field

#### Scenario: Duration not a multiple of five
- **WHEN** a code is created with a duration of 7 minutes
- **THEN** the system rejects the request with a 422 validation error on the duration field

### Requirement: Item numbers are unique among active codes
An item number SHALL be used by at most one non-archived procedure code in an organisation. Archived codes do not block reuse, and different organisations may use the same item numbers.

#### Scenario: Duplicate active item number
- **GIVEN** the organisation has an active code with item number "114"
- **WHEN** another code with item number "114" is created
- **THEN** the system rejects the request with 409

#### Scenario: Reuse after archiving
- **GIVEN** the organisation's code "114" is archived
- **WHEN** a new code with item number "114" is created
- **THEN** the new code is created

### Requirement: Listing procedure codes
Every member of the organisation SHALL be able to list and view its procedure codes, ordered by item number, optionally filtered by a search term matching the item number prefix or words in the description. Lists SHALL follow the standard pagination and delta-sync conventions.

#### Scenario: Search by description
- **GIVEN** codes "114 Removal of calculus" and "011 Comprehensive oral examination"
- **WHEN** a member lists codes with the search term "calculus"
- **THEN** only code "114" is returned

#### Scenario: Code from another organisation
- **WHEN** a member requests a procedure code that belongs to a different organisation
- **THEN** the system responds with 404

### Requirement: Procedure code changes are audited
Creating, updating and archiving a procedure code SHALL each write an audit entry with the changed fields.

#### Scenario: Fee change is audited
- **WHEN** an admin changes code "114"'s default fee from 15500 to 16000 cents
- **THEN** an audit entry records action "procedure_code.updated" with the old and new fee
