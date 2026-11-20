# access-control Specification

## Purpose

Decides what an authenticated member of an organisation is allowed to do, through a fixed set of roles mapped to named permissions and an optional restriction to specific locations.

## Requirements

### Requirement: Fixed role set
Each membership SHALL have exactly one role from: owner, admin, practitioner, front_desk. Permissions are granted per role and checked by named permission, not by role name, so later domains can add permissions without new roles.

#### Scenario: Role determines permitted actions
- **GIVEN** a member with the front_desk role
- **WHEN** they attempt to create a location
- **THEN** the system responds with 403 because front_desk lacks the location management permission

### Requirement: Foundation permission matrix
In this change, owner and admin SHALL hold the permissions to manage locations, manage members and invitations, and view the audit log. Practitioner and front_desk SHALL be able to view their organisation's locations and their own profile only.

#### Scenario: Admin manages members
- **WHEN** an admin changes a front_desk member's role to practitioner
- **THEN** the change succeeds and is recorded in the audit log

#### Scenario: Practitioner views audit log
- **WHEN** a practitioner requests the audit log
- **THEN** the system responds with 403

### Requirement: Owner protection
Only owners SHALL be able to grant or remove the owner role. An organisation MUST always keep at least one active owner.

#### Scenario: Admin attempts to create an owner
- **WHEN** an admin tries to change a member's role to owner
- **THEN** the system responds with 403

#### Scenario: Removing the last owner
- **GIVEN** an organisation with exactly one active owner
- **WHEN** that owner tries to change their own role, or anyone tries to deactivate them
- **THEN** the system rejects the request with 409 explaining that an organisation needs an owner

### Requirement: Location scoping
A membership SHALL be either unrestricted (all locations) or restricted to a listed set of the organisation's locations. Owners and admins are always unrestricted. Location-scoped data requested outside a restricted member's locations MUST be treated as not found.

#### Scenario: Restricted member lists locations
- **GIVEN** a practitioner restricted to location "Parramatta"
- **WHEN** they list locations
- **THEN** only "Parramatta" is returned

#### Scenario: Restricted member switches active location
- **GIVEN** a front_desk member restricted to locations X and Y
- **WHEN** they try to set location Z as their active location
- **THEN** the system responds with 403

### Requirement: Current permissions are discoverable
The system SHALL expose the current user's role, permissions and accessible locations for the active organisation, so the interface can hide actions the user cannot take. The server MUST still enforce every permission.

#### Scenario: Interface reads permissions
- **WHEN** the client requests the current session
- **THEN** the response includes the active organisation, role, permission names, and accessible location identifiers
