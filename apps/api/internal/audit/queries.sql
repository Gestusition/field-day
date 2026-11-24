-- name: InsertAuditLog :exec
INSERT INTO audit_logs (actor_user_id, actor_subject, method, path, status_code, request_id)
VALUES (sqlc.narg('actor_user_id'), @actor_subject, @method, @path, @status_code, @request_id);

-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
  AND (sqlc.narg('actor_user_id')::uuid IS NULL OR actor_user_id = sqlc.narg('actor_user_id')::uuid)
ORDER BY id DESC
LIMIT @max_rows;
