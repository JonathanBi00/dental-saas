# Spec Delta

## Purpose

Records who a practice's patients are and how to contact them, so appointments, clinical notes and invoices can be attached to the right person, while keeping that personal health information protected and retained.

## ADDED Requirements

### Requirement: Patient records
The system SHALL let an organisation store patients, each with a title, first name, middle names, last name, preferred name, date of birth, sex, mobile phone, home phone, email, address and an optional home location. Last name and date of birth SHALL be required; every other field is optional.

#### Scenario: Register a patient
- **WHEN** a front desk member creates a patient with last name "Nguyen", first name "Linh" and date of birth 1988-03-14
- **THEN** the patient is stored under their organisation and returned with its identifier, patient number and all supplied fields

#### Scenario: Missing required fields
- **WHEN** a patient is created with an empty last name or no date of birth
- **THEN** the system rejects the request with a 422 validation error naming each missing field

#### Scenario: Patient with a single name
- **WHEN** a patient is created with last name "Madonna" and no first name
- **THEN** the patient is stored and displayed by that single name

### Requirement: Patient field validation
The system SHALL reject a patient whose date of birth is in the future or before 1900-01-01, whose sex is not one of female, male, other or not_stated, whose email is not a valid address, whose phone numbers contain anything other than digits, spaces, brackets, hyphens and a leading plus sign, or whose address uses a state other than NSW, VIC, QLD, WA, SA, TAS, ACT or NT or a postcode that is not four digits.

#### Scenario: Date of birth in the future
- **WHEN** a patient is created or updated with a date of birth after today
- **THEN** the system rejects the request with a 422 validation error on the date of birth field

#### Scenario: Invalid postcode
- **WHEN** a patient is created with postcode "20000"
- **THEN** the system rejects the request with a 422 validation error on the postcode field

#### Scenario: Unknown sex value
- **WHEN** a patient is created with sex "unknown"
- **THEN** the system rejects the request with a 422 validation error on the sex field

### Requirement: Home location
A patient's home location, when set, SHALL be a non-archived location of the same organisation. It is informational only and SHALL NOT limit which members can see the patient.

#### Scenario: Home location from another organisation
- **WHEN** a patient is created with a home location that belongs to a different organisation, or that does not exist
- **THEN** the system rejects the request with a 422 validation error on the home location field

#### Scenario: Archived home location
- **WHEN** a patient is created or updated with a home location that is archived
- **THEN** the system rejects the request with a 422 validation error on the home location field

### Requirement: Patient numbers
Each new patient SHALL receive a patient number that is unique within its organisation, assigned in increasing order starting at 1001. A patient number SHALL never change and SHALL never be reused, including after the patient is archived.

#### Scenario: Sequential numbers
- **GIVEN** an organisation whose most recent patient has number 1041
- **WHEN** a new patient is created
- **THEN** the new patient's number is 1042

#### Scenario: Numbers are per organisation
- **WHEN** organisations A and B each create their first patient
- **THEN** both patients have number 1001

#### Scenario: Number cannot be changed
- **WHEN** a client sends a PATCH that includes a patient number
- **THEN** the system rejects the request with a 422 validation error on the patient number field

#### Scenario: Concurrent creates
- **WHEN** two patients are created in the same organisation at the same moment
- **THEN** they receive different patient numbers

### Requirement: Possible duplicate detection
When a patient is created with the same first name, last name (ignoring case and surrounding spaces) and date of birth as an existing non-archived patient, the system SHALL reject it with 409 and list the matching patients, unless the request explicitly confirms the patient is not a duplicate.

#### Scenario: Duplicate warning
- **GIVEN** an active patient "Linh Nguyen" born 1988-03-14
- **WHEN** a patient "linh nguyen " born 1988-03-14 is created without confirmation
- **THEN** the system responds with 409 and the problem body lists the existing patient's identifier, patient number and name

#### Scenario: Confirmed different person
- **GIVEN** the same existing patient
- **WHEN** the create request is repeated with the confirmation flag set
- **THEN** the new patient is created with its own patient number

#### Scenario: Archived match is not a duplicate
- **GIVEN** an archived patient "Linh Nguyen" born 1988-03-14
- **WHEN** a patient with the same name and date of birth is created without confirmation
- **THEN** the new patient is created

### Requirement: Updating patients
Members with the patient management permission SHALL be able to change any patient field except the patient number by PATCH, where omitted fields are left unchanged and fields sent as null are cleared. Required fields MUST NOT be cleared.

#### Scenario: Change a phone number
- **WHEN** a front desk member updates a patient's mobile phone
- **THEN** only the mobile phone changes, and the response shows the new value and a later updated time

#### Scenario: Clear a required field
- **WHEN** a PATCH sets a patient's last name to null or an empty string
- **THEN** the system rejects the request with a 422 validation error on the last name field

### Requirement: Archive and restore
Patients SHALL be archived rather than deleted, and an archived patient SHALL be restorable. The system MUST NOT offer any way to permanently delete a patient.

#### Scenario: Archive a patient
- **WHEN** an owner, admin or front desk member archives a patient
- **THEN** the patient no longer appears in default patient lists or searches but remains retrievable by identifier and in lists that request archived patients

#### Scenario: Restore a patient
- **GIVEN** an archived patient
- **WHEN** an owner, admin or front desk member restores them
- **THEN** the patient appears in default lists again with the same identifier and patient number

#### Scenario: Restore creates a duplicate
- **GIVEN** an archived patient and an active patient with the same first name, last name and date of birth
- **WHEN** the archived patient is restored
- **THEN** the patient is restored, because the duplicate check only applies to creating patients

### Requirement: Listing and searching patients
The system SHALL list an organisation's patients ordered by last name, first name and identifier, and accept a single search term that matches a patient number exactly, a date of birth in YYYY-MM-DD form exactly, the mobile or home phone when the term has at least six digits and they appear in the phone number's digits, or the start of the first, last or preferred name, ignoring case.

#### Scenario: Search by name prefix
- **GIVEN** patients "Linh Nguyen" and "Lindsay Ng"
- **WHEN** a member searches for "ng"
- **THEN** both patients are returned

#### Scenario: Search by patient number
- **WHEN** a member searches for "1042"
- **THEN** only the patient with number 1042 is returned, because four digits are too few to match phone numbers

#### Scenario: Search by phone digits
- **GIVEN** a patient with mobile phone "0412 345 678"
- **WHEN** a member searches for "0412345678" or "345678"
- **THEN** that patient is returned

#### Scenario: Search by date of birth
- **WHEN** a member searches for "1988-03-14"
- **THEN** every non-archived patient born on that date is returned

### Requirement: Patients are organisation-wide
Every member with the patient read permission SHALL be able to see all of their organisation's patients, whatever locations their membership is restricted to.

#### Scenario: Restricted member finds a patient from another location
- **GIVEN** a front desk member restricted to "Parramatta" and a patient whose home location is "Chatswood"
- **WHEN** the member searches for that patient
- **THEN** the patient is returned

### Requirement: Patient identifiers are not guessable across organisations
A patient that belongs to another organisation, or does not exist, SHALL be reported as not found, whether requested by identifier or acted on by update, archive or restore.

#### Scenario: Read another organisation's patient
- **GIVEN** a patient that belongs to organisation A
- **WHEN** a member whose active organisation is B requests, updates or archives that patient by identifier
- **THEN** the system responds with 404 Not Found
