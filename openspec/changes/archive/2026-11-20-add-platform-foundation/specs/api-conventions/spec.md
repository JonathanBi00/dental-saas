# Spec Delta

## Purpose

Defines the HTTP behaviour every endpoint shares (paths, errors, pagination, filtering, delta sync, tracing and health) so clients, including future third-party integrators, can rely on one consistent contract.

## ADDED Requirements

### Requirement: Versioned base path and JSON
All application endpoints SHALL be served under `/api/v1`, accept and return JSON, and be described in a published OpenAPI 3 document that matches the running behaviour.

#### Scenario: OpenAPI document is available
- **WHEN** a client requests the OpenAPI document from the running server
- **THEN** it receives the spec describing every `/api/v1` endpoint

### Requirement: Standard error format
Error responses SHALL use the Problem Details format (`application/problem+json`) with `type`, `title`, `status`, `detail`, and the request ID. Validation errors (422) MUST also list each invalid field and its reason.

#### Scenario: Validation error
- **WHEN** a client submits a create request missing a required field
- **THEN** the response is 422 with a problem body whose errors list names that field

#### Scenario: Unexpected server error
- **WHEN** an unhandled error occurs while processing a request
- **THEN** the response is 500 with a generic problem body and no internal details, and the error is logged with the request ID

### Requirement: Cursor pagination
List endpoints SHALL return results in pages using an opaque `cursor` and a `limit` (default 50, maximum 200). Each page SHALL include the cursor for the next page, or none on the last page. Ordering MUST be stable across pages.

#### Scenario: Page through results
- **GIVEN** 120 matching records
- **WHEN** a client requests with limit 50 and follows the returned cursors
- **THEN** it receives 50, 50 and 20 records with no duplicates or gaps, and the last page has no next cursor

#### Scenario: Limit above maximum
- **WHEN** a client requests limit 1000
- **THEN** the system responds with 422

### Requirement: Delta sync
List endpoints for tenant data SHALL accept an `updated_since` timestamp that returns records created, changed or soft-deleted after that time, ordered by modification time. Soft-deleted records SHALL be returned with their deletion timestamp so clients can remove them.

#### Scenario: Incremental sync includes deletions
- **GIVEN** a client that last synced at time T, after which one location was renamed and one archived
- **WHEN** it lists locations with `updated_since=T`
- **THEN** both locations are returned, the archived one showing its archive timestamp

### Requirement: Request IDs
Every response SHALL include an `X-Request-Id` header. If the client supplies a valid one it SHALL be reused; otherwise the server generates one. All logs for that request MUST carry it.

#### Scenario: Client-supplied request ID
- **WHEN** a client sends `X-Request-Id: abc-123`
- **THEN** the response carries `X-Request-Id: abc-123`

### Requirement: Timestamps
All timestamps in requests and responses SHALL be RFC 3339 strings. Responses SHALL return them in UTC.

#### Scenario: Timestamp format
- **WHEN** a client reads a location
- **THEN** its `created_at` and `updated_at` are RFC 3339 UTC strings ending in `Z`

### Requirement: Health endpoints
The server SHALL expose an unauthenticated liveness endpoint that reports the process is running, and a readiness endpoint that reports unhealthy when the database is unreachable.

#### Scenario: Database down
- **GIVEN** the database is unreachable
- **WHEN** the readiness endpoint is called
- **THEN** it responds with 503 while the liveness endpoint still responds with 200
