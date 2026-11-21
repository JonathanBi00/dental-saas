# Spec Delta

## MODIFIED Requirements

### Requirement: Foundation permission matrix
In this change, owner and admin SHALL hold the permissions to manage locations, manage members and invitations, view the audit log, and manage practitioners, procedure codes and appointment books. Every role SHALL be able to view their organisation's locations, practitioners, procedure codes and appointment books (within their location scope), and their own profile.

#### Scenario: Admin manages members
- **WHEN** an admin changes a front_desk member's role to practitioner
- **THEN** the change succeeds and is recorded in the audit log

#### Scenario: Practitioner views audit log
- **WHEN** a practitioner requests the audit log
- **THEN** the system responds with 403

#### Scenario: Front desk views reference data
- **WHEN** a front_desk member lists practitioners, procedure codes or appointment books
- **THEN** the request succeeds

#### Scenario: Practitioner edits a fee
- **WHEN** a practitioner tries to change a procedure code's fee
- **THEN** the system responds with 403 because practitioners lack the procedure code management permission

#### Scenario: Session lists new permissions
- **WHEN** an owner requests the current session
- **THEN** the permission names include the read and manage permissions for practitioners, procedure codes and appointment books
