# Spec Delta

## ADDED Requirements

### Requirement: Patient permissions
Every role SHALL hold the permission to view patients. Owner, admin and front_desk SHALL also hold the permission to manage patients: create, update, archive and restore. Practitioners SHALL NOT be able to manage patients in this change.

#### Scenario: Front desk registers a patient
- **WHEN** a front_desk member creates a patient
- **THEN** the request succeeds

#### Scenario: Practitioner edits patient details
- **WHEN** a practitioner tries to update a patient's address
- **THEN** the system responds with 403 because practitioners lack the patient management permission

#### Scenario: Practitioner views a patient
- **WHEN** a practitioner requests a patient by identifier
- **THEN** the request succeeds

#### Scenario: Session lists patient permissions
- **WHEN** a front_desk member requests the current session
- **THEN** the permission names include the patient read and patient manage permissions
