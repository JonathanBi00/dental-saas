-- name: ListMembers :many
SELECT m.id, m.user_id, u.email, u.name, m.role, m.status, m.created_at, m.updated_at,
       coalesce(array_agg(ml.location_id) FILTER (WHERE ml.location_id IS NOT NULL), '{}')::uuid[] AS location_ids
FROM memberships m
JOIN users u ON u.id = m.user_id
LEFT JOIN membership_locations ml ON ml.membership_id = m.id
WHERE m.org_id = sqlc.arg(org_id)
  AND (sqlc.narg(after_created)::timestamptz IS NULL
       OR (m.created_at, m.id) > (sqlc.narg(after_created)::timestamptz, sqlc.narg(after_id)::uuid))
GROUP BY m.id, u.id
ORDER BY m.created_at, m.id
LIMIT sqlc.arg(row_limit);

-- name: GetMember :one
SELECT m.id, m.user_id, u.email, u.name, m.role, m.status, m.created_at, m.updated_at,
       coalesce(array_agg(ml.location_id) FILTER (WHERE ml.location_id IS NOT NULL), '{}')::uuid[] AS location_ids
FROM memberships m
JOIN users u ON u.id = m.user_id
LEFT JOIN membership_locations ml ON ml.membership_id = m.id
WHERE m.org_id = sqlc.arg(org_id) AND m.id = sqlc.arg(id)
GROUP BY m.id, u.id;

-- name: UpdateMembershipRole :exec
UPDATE memberships SET role = $2 WHERE id = $1;

-- name: UpdateMembershipStatus :exec
UPDATE memberships SET status = $2 WHERE id = $1;

-- name: CountActiveOwners :one
SELECT count(*) FROM memberships WHERE org_id = $1 AND role = 'owner' AND status = 'active';

-- name: CountLocationsInOrg :one
SELECT count(*) FROM locations WHERE id = ANY (sqlc.arg(ids)::uuid[]);
