# Spec Delta

## ADDED Requirements

### Requirement: Access to patient records is audited
Every request that returns patient records SHALL write an audit entry: viewing one patient records action "patient.viewed" with that patient as the target, and every list or search records action "patient.listed" with the search term, filters and number of patients returned. The entry MUST be committed with the read, so a read whose audit entry cannot be written fails.

#### Scenario: Viewing a patient is audited
- **WHEN** a practitioner opens a patient's record
- **THEN** an audit entry records the practitioner, the organisation, action "patient.viewed" and the patient's identifier

#### Scenario: Searching patients is audited
- **WHEN** a front desk member searches patients for "nguyen" and five patients are returned
- **THEN** an audit entry records the member, action "patient.listed", the search term "nguyen" and a result count of 5

#### Scenario: Audit write fails
- **WHEN** the audit entry for a patient view cannot be written
- **THEN** the request fails with a 500 error and no patient data is returned

### Requirement: Changes to patient records are audited
Creating, updating, archiving and restoring a patient SHALL each write an audit entry with actions "patient.created", "patient.updated", "patient.archived" and "patient.restored", including the changed fields with old and new values, in the same transaction as the change.

#### Scenario: Address change is audited
- **WHEN** a front desk member changes a patient's suburb from "Parramatta" to "Westmead"
- **THEN** an audit entry records action "patient.updated", the patient's identifier, and suburb with old value "Parramatta" and new value "Westmead"

#### Scenario: Restore is audited
- **WHEN** an admin restores an archived patient
- **THEN** an audit entry records action "patient.restored" and the patient's identifier
