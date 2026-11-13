-- name: InsertAuditEvent :exec
INSERT INTO audit_events (
  id, org_id, actor_user_id, action, target_type, target_id, ip, user_agent, request_id, details
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
);

-- name: ListAuditEvents :many
-- Newest first. ids are UUIDv7, so id order is creation order.
SELECT a.id, a.org_id, a.actor_user_id, coalesce(u.email::text, '')::text AS actor_email, coalesce(u.name, '')::text AS actor_name,
       a.action, a.target_type, a.target_id, a.ip, a.user_agent, a.request_id, a.details, a.created_at
FROM audit_events a
LEFT JOIN users u ON u.id = a.actor_user_id
WHERE (sqlc.narg(from_time)::timestamptz IS NULL OR a.created_at >= sqlc.narg(from_time)::timestamptz)
  AND (sqlc.narg(to_time)::timestamptz IS NULL OR a.created_at < sqlc.narg(to_time)::timestamptz)
  AND (sqlc.narg(actor_user_id)::uuid IS NULL OR a.actor_user_id = sqlc.narg(actor_user_id)::uuid)
  AND (sqlc.narg(action)::text IS NULL OR a.action = sqlc.narg(action)::text)
  AND (sqlc.narg(target_type)::text IS NULL OR a.target_type = sqlc.narg(target_type)::text)
  AND (sqlc.narg(target_id)::uuid IS NULL OR a.target_id = sqlc.narg(target_id)::uuid)
  AND (sqlc.narg(before_id)::uuid IS NULL OR a.id < sqlc.narg(before_id)::uuid)
ORDER BY a.id DESC
LIMIT sqlc.arg(row_limit);
