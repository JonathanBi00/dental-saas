-- name: CreateOrganisation :one
INSERT INTO organisations (id, name, abn) VALUES ($1, $2, $3)
RETURNING *;

-- name: GetOrganisation :one
SELECT * FROM organisations WHERE id = $1;

-- name: FindOrganisationByName :one
SELECT * FROM organisations WHERE name = $1;

-- name: CreateLocation :one
INSERT INTO locations (
  id, org_id, name, address_line1, address_line2, suburb, state, postcode, phone, email, timezone
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: GetLocation :one
SELECT * FROM locations WHERE id = $1;

-- name: UpdateLocation :one
UPDATE locations SET
  name = $2, address_line1 = $3, address_line2 = $4, suburb = $5, state = $6,
  postcode = $7, phone = $8, email = $9, timezone = $10
WHERE id = $1
RETURNING *;

-- name: ArchiveLocation :one
UPDATE locations SET archived_at = now()
WHERE id = $1 AND archived_at IS NULL
RETURNING *;

-- name: ListLocationsByName :many
-- Default listing, ordered by name. location_ids restricts to a member's locations (NULL = all).
SELECT * FROM locations
WHERE (sqlc.arg(include_archived)::bool OR archived_at IS NULL)
  AND (sqlc.narg(location_ids)::uuid[] IS NULL OR id = ANY (sqlc.narg(location_ids)::uuid[]))
  AND (sqlc.narg(after_name)::text IS NULL
       OR (name, id) > (sqlc.narg(after_name)::text, sqlc.narg(after_id)::uuid))
ORDER BY name, id
LIMIT sqlc.arg(row_limit);

-- name: ListLocationsUpdatedSince :many
-- Delta sync: everything changed (including archived) after a timestamp, in modification order.
SELECT * FROM locations
WHERE updated_at > sqlc.arg(updated_since)::timestamptz
  AND (sqlc.narg(location_ids)::uuid[] IS NULL OR id = ANY (sqlc.narg(location_ids)::uuid[]))
  AND (sqlc.narg(after_updated)::timestamptz IS NULL
       OR (updated_at, id) > (sqlc.narg(after_updated)::timestamptz, sqlc.narg(after_id)::uuid))
ORDER BY updated_at, id
LIMIT sqlc.arg(row_limit);

-- name: ListAccessibleLocationIDs :many
SELECT id FROM locations
WHERE archived_at IS NULL
  AND (sqlc.narg(location_ids)::uuid[] IS NULL OR id = ANY (sqlc.narg(location_ids)::uuid[]))
ORDER BY name, id;
