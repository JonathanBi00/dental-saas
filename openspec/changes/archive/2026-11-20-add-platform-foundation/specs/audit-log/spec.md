# Spec Delta

## Purpose

Keeps a tamper-resistant record of who did what, when and where across the platform, to meet health-information obligations under the Australian Privacy Act and to support investigation of incidents.

## ADDED Requirements

### Requirement: Recorded events
The system SHALL write an audit entry for: sign-in success and failure, sign-out, password reset completion, lockouts, invitation create/accept/revoke, membership and role changes, active organisation switches, and every create, update, archive or delete of tenant data.

#### Scenario: Data change is audited
- **WHEN** an admin renames a location
- **THEN** an audit entry records the actor, organisation, action "location.updated", the location identifier, and the changed fields with old and new values

#### Scenario: Failed sign-in is audited
- **WHEN** a sign-in attempt fails
- **THEN** an audit entry records the attempted email, the client IP address, and the failure, with no password data

### Requirement: Audit entry contents
Each entry SHALL include a timestamp (UTC), organisation (when applicable), actor user (when known), action name, target record type and identifier, client IP address, user agent, and request ID. Entries MUST NOT contain passwords, tokens or session identifiers.

#### Scenario: Entry links to the request
- **WHEN** an audited action occurs during an API request
- **THEN** the entry's request ID matches the request ID returned in that response's headers

### Requirement: Audit entries are written atomically with changes
An audit entry for a data change SHALL be committed in the same transaction as the change: either both persist or neither does.

#### Scenario: Change fails
- **WHEN** an update to a location fails and is rolled back
- **THEN** no audit entry for that update exists

### Requirement: Audit log is append-only
Audit entries MUST NOT be modifiable or deletable through the application, including through its database role.

#### Scenario: Attempt to alter an entry
- **WHEN** the application's database role attempts to update or delete an audit entry
- **THEN** the database rejects the operation

### Requirement: Viewing the audit log
Members with the audit view permission SHALL be able to list their organisation's audit entries, newest first, filtered by date range, actor, action and target. They MUST NOT see entries from other organisations.

#### Scenario: Filter by actor
- **WHEN** an owner lists audit entries filtered to a specific user for the last 7 days
- **THEN** only that user's entries in that range for the owner's organisation are returned
