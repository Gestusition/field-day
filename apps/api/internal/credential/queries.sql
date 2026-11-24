-- name: CreateCredential :one
INSERT INTO credentials (kind, uid, user_id, label, secret_enc)
VALUES (@kind, @uid, sqlc.narg('user_id'), @label, sqlc.narg('secret_enc'))
RETURNING *;

-- name: GetCredential :one
SELECT * FROM credentials WHERE id = $1;

-- name: GetLiveCredentialByUID :one
SELECT * FROM credentials WHERE uid = $1 AND status <> 'revoked';

-- name: UpdateCredential :one
UPDATE credentials
SET user_id    = sqlc.narg('user_id'),
    label      = @label,
    status     = @status::text,
    revoked_at = CASE WHEN @status::text = 'revoked' THEN now() ELSE NULL END
WHERE id = @id
RETURNING *;

-- name: TouchCredentialUsed :exec
UPDATE credentials SET last_used_at = now() WHERE id = $1;

-- name: ListCredentials :many
SELECT * FROM credentials
WHERE (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
  AND (sqlc.narg('user_id')::uuid IS NULL OR user_id = sqlc.narg('user_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('kind')::text IS NULL OR kind = sqlc.narg('kind')::text)
  AND (sqlc.narg('uid_prefix')::text IS NULL OR uid LIKE sqlc.narg('uid_prefix')::text || '%')
ORDER BY id DESC
LIMIT @max_rows;

-- name: ListCredentialsByUser :many
SELECT * FROM credentials WHERE user_id = $1 ORDER BY id DESC;

-- name: CountActiveMobileCredentials :one
SELECT count(*) FROM credentials
WHERE user_id = $1 AND kind = 'mobile_hce' AND status = 'active';

-- name: UpsertEnrollment :one
INSERT INTO reader_enrollments (reader_id, user_id, label, created_by, expires_at)
VALUES (@reader_id, sqlc.narg('user_id'), @label, sqlc.narg('created_by'), @expires_at)
ON CONFLICT (reader_id) DO UPDATE
SET user_id    = EXCLUDED.user_id,
    label      = EXCLUDED.label,
    created_by = EXCLUDED.created_by,
    expires_at = EXCLUDED.expires_at,
    created_at = now()
RETURNING *;

-- name: DeleteEnrollment :execrows
DELETE FROM reader_enrollments WHERE reader_id = $1;

-- name: TakeEnrollment :one
DELETE FROM reader_enrollments
WHERE reader_id = $1 AND expires_at > now()
RETURNING *;
