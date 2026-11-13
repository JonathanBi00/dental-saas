# Spec Delta

## Purpose

Establishes who a user is: their account, how they join organisations, and how they sign in and out securely, so that every action in the system can be attributed to an authenticated person.

## ADDED Requirements

### Requirement: User accounts
The system SHALL store each user with a unique, case-insensitive email address, a display name, and a password hash. A single user account MAY be a member of multiple organisations. Passwords MUST never be stored or logged in plain text.

#### Scenario: Email uniqueness is case-insensitive
- **GIVEN** a user with email "Jane@Clinic.com.au"
- **WHEN** an invitation is accepted that would create a second account for "jane@clinic.com.au"
- **THEN** the system attaches the membership to the existing account instead of creating a duplicate

### Requirement: Password policy
The system SHALL require passwords of at least 12 characters and SHALL reject passwords that match the user's email address.

#### Scenario: Password too short
- **WHEN** a user sets a password of 8 characters
- **THEN** the system rejects it with a 422 validation error explaining the minimum length

### Requirement: Sign in
The system SHALL let a user sign in with email and password. On success it SHALL create a server-side session and return it as an HttpOnly, SameSite cookie. Failure responses MUST NOT reveal whether the email exists.

#### Scenario: Successful sign in
- **WHEN** a user submits a correct email and password
- **THEN** the system sets a session cookie and returns the user's profile, memberships, and active organisation

#### Scenario: Wrong password or unknown email
- **WHEN** a user submits an incorrect password, or an email with no account
- **THEN** the system responds with the same 401 error and message in both cases

#### Scenario: User with a single organisation
- **WHEN** a user with exactly one active membership signs in
- **THEN** that organisation becomes the session's active organisation

### Requirement: Brute-force protection
The system SHALL temporarily block sign-in for an email address after 5 consecutive failed attempts within 15 minutes, for 15 minutes.

#### Scenario: Lockout after repeated failures
- **GIVEN** 5 failed sign-in attempts for an email within 15 minutes
- **WHEN** a sixth attempt is made, even with the correct password
- **THEN** the system responds with 429 and does not create a session

### Requirement: Session lifetime
Sessions SHALL expire after 60 minutes of inactivity and after 12 hours regardless of activity. Expired or revoked sessions MUST be rejected.

#### Scenario: Idle timeout
- **GIVEN** a session with no requests for more than 60 minutes
- **WHEN** the client makes a request with that session cookie
- **THEN** the system responds with 401 and the client is returned to sign in

#### Scenario: Activity extends the idle window
- **GIVEN** a session last used 50 minutes ago and created less than 12 hours ago
- **WHEN** the client makes a request
- **THEN** the request succeeds and the idle window restarts

### Requirement: Sign out
The system SHALL let a user sign out, which revokes the current session server-side and clears the cookie.

#### Scenario: Reusing a signed-out session
- **GIVEN** a user who has signed out
- **WHEN** a request is made with the old session cookie value
- **THEN** the system responds with 401

### Requirement: Cross-site request protection
State-changing requests authenticated by session cookie SHALL be rejected unless they originate from the application's own origin.

#### Scenario: Forged cross-site request
- **WHEN** a POST request carrying a valid session cookie arrives with an Origin header that is not the application's origin
- **THEN** the system rejects it with 403 and makes no change

### Requirement: Invitations
Owners and admins SHALL be able to invite a person by email to their organisation with a role. The invitation link MUST be single-use and expire after 7 days. Accepting it creates the account (if needed) and the membership.

#### Scenario: Invite a new person
- **WHEN** an admin invites "new@clinic.com.au" with the front desk role
- **THEN** the system emails a single-use link, and the pending invitation appears in the organisation's user list

#### Scenario: Accept an invitation as a new user
- **WHEN** the invitee opens a valid link and sets a name and a valid password
- **THEN** the system creates the account, adds an active membership with the invited role, and signs them in

#### Scenario: Expired or reused invitation
- **WHEN** someone opens an invitation link that has expired, been revoked, or already been used
- **THEN** the system shows that the invitation is no longer valid and creates nothing

### Requirement: Password reset
The system SHALL let a user request a password reset by email. The reset link MUST be single-use and expire after 60 minutes. Completing a reset MUST revoke all of that user's existing sessions. The request response MUST NOT reveal whether the email exists.

#### Scenario: Request a reset
- **WHEN** someone submits the reset form with any email address
- **THEN** the system responds identically whether or not an account exists, and emails a link only if it does

#### Scenario: Complete a reset
- **WHEN** a user opens a valid reset link and sets a new valid password
- **THEN** the password is changed, all their other sessions are revoked, and the link cannot be used again

### Requirement: Membership deactivation
Owners and admins SHALL be able to deactivate a member's access to their organisation. Deactivation MUST take effect on the member's next request.

#### Scenario: Deactivated member's next request
- **GIVEN** a member with an active session whose membership was just deactivated
- **WHEN** they make a request scoped to that organisation
- **THEN** the system responds with 403 and returns no organisation data
