-- Users -------------------------------------------------------------------

-- name: CreateUser :exec
INSERT INTO users (id, email, name, password_hash) VALUES ($1, $2, $3, $4);

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- Memberships -------------------------------------------------------------

-- name: ListUserMemberships :many
SELECT m.id, m.org_id, o.name AS org_name, o.status AS org_status, m.role, m.status
FROM memberships m
JOIN organisations o ON o.id = m.org_id
WHERE m.user_id = $1
ORDER BY o.name, o.id;

-- name: GetMembershipByUser :one
SELECT * FROM memberships WHERE org_id = $1 AND user_id = $2;

-- name: UpsertMembership :one
INSERT INTO memberships (id, org_id, user_id, role)
VALUES ($1, $2, $3, $4)
ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role, status = 'active'
RETURNING *;

-- name: ListMembershipLocationIDs :many
SELECT location_id FROM membership_locations WHERE membership_id = $1;

-- name: DeleteMembershipLocations :exec
DELETE FROM membership_locations WHERE membership_id = $1;

-- name: AddMembershipLocation :exec
INSERT INTO membership_locations (org_id, membership_id, location_id) VALUES ($1, $2, $3);

-- Sessions ----------------------------------------------------------------

-- name: CreateSession :one
INSERT INTO sessions (id, token_hash, user_id, active_org_id, ip, user_agent, created_at, last_seen_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $8)
RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = $1;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = $2 WHERE id = $1;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeUserSessions :exec
UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL;

-- name: SetSessionOrganisation :exec
UPDATE sessions SET active_org_id = $2, active_location_id = NULL WHERE id = $1;

-- name: SetSessionLocation :exec
UPDATE sessions SET active_location_id = $2 WHERE id = $1;

-- Sign-in throttling ------------------------------------------------------

-- name: GetLoginAttempt :one
SELECT * FROM login_attempts WHERE email = $1;

-- name: UpsertLoginAttempt :exec
INSERT INTO login_attempts (email, failures, window_started_at, locked_until)
VALUES ($1, $2, $3, $4)
ON CONFLICT (email) DO UPDATE SET
  failures = EXCLUDED.failures,
  window_started_at = EXCLUDED.window_started_at,
  locked_until = EXCLUDED.locked_until;

-- name: DeleteLoginAttempt :exec
DELETE FROM login_attempts WHERE email = $1;

-- Password reset ----------------------------------------------------------

-- name: CreatePasswordReset :exec
INSERT INTO password_resets (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4);

-- name: GetPasswordResetByTokenHash :one
SELECT * FROM password_resets WHERE token_hash = $1;

-- name: MarkPasswordResetUsed :execrows
UPDATE password_resets SET used_at = $2 WHERE id = $1 AND used_at IS NULL;

-- Invitations -------------------------------------------------------------

-- name: CreateInvitation :one
INSERT INTO invitations (id, org_id, email, role, location_ids, token_hash, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetInvitation :one
SELECT * FROM invitations WHERE id = $1;

-- name: ListPendingInvitations :many
SELECT * FROM invitations
WHERE accepted_at IS NULL AND revoked_at IS NULL AND expires_at > sqlc.arg(now)::timestamptz
  AND (sqlc.narg(after_created)::timestamptz IS NULL
       OR (created_at, id) > (sqlc.narg(after_created)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY created_at, id
LIMIT sqlc.arg(row_limit);

-- name: RevokeInvitation :one
UPDATE invitations SET revoked_at = $2
WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL
RETURNING *;

-- name: RevokePendingInvitationsForEmail :exec
UPDATE invitations SET revoked_at = $2
WHERE email = $1 AND accepted_at IS NULL AND revoked_at IS NULL;

-- name: MarkInvitationAccepted :execrows
UPDATE invitations SET accepted_at = $2
WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL;
