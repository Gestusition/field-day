-- name: CreateReader :one
INSERT INTO readers (name, serial, zone_id, mode, key_hash)
VALUES (@name, @serial, sqlc.narg('zone_id'), @mode, @key_hash)
RETURNING *;

-- name: GetReader :one
SELECT * FROM readers WHERE id = $1;

-- name: GetReaderByKeyHash :one
SELECT * FROM readers WHERE key_hash = $1;

-- name: UpdateReader :one
UPDATE readers
SET name    = @name,
    zone_id = sqlc.narg('zone_id'),
    mode    = @mode,
    status  = @status
WHERE id = @id
RETURNING *;

-- name: SetReaderKeyHash :one
UPDATE readers SET key_hash = @key_hash WHERE id = @id RETURNING *;

-- name: TouchReaderSeen :exec
UPDATE readers
SET last_seen_at     = now(),
    firmware_version = COALESCE(sqlc.narg('firmware_version'), firmware_version)
WHERE id = @id;

-- name: ListReaders :many
SELECT * FROM readers
WHERE (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
  AND (sqlc.narg('zone_id')::uuid IS NULL OR zone_id = sqlc.narg('zone_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY id DESC
LIMIT @max_rows;

-- name: CreateReaderCommand :one
INSERT INTO reader_commands (reader_id, type, payload, created_by, expires_at)
VALUES (@reader_id, @type, @payload, sqlc.narg('created_by'), @expires_at)
RETURNING *;

-- name: ListReaderCommands :many
SELECT * FROM reader_commands
WHERE reader_id = @reader_id
  AND (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
ORDER BY id DESC
LIMIT @max_rows;

-- name: ListOpenReaderCommands :many
SELECT * FROM reader_commands
WHERE reader_id = $1
  AND status IN ('pending', 'delivered')
  AND expires_at > now()
ORDER BY id
LIMIT 20;

-- name: MarkReaderCommandsDelivered :exec
UPDATE reader_commands
SET status = 'delivered', delivered_at = now()
WHERE id = ANY(@ids::uuid[]) AND status = 'pending';

-- name: CompleteReaderCommand :one
UPDATE reader_commands
SET status = @status, result = @result, completed_at = now()
WHERE id = @id AND reader_id = @reader_id AND status IN ('pending', 'delivered')
RETURNING *;

-- name: GetReaderCommand :one
SELECT * FROM reader_commands WHERE id = @id AND reader_id = @reader_id;

-- name: ExpireReaderCommands :execrows
UPDATE reader_commands
SET status = 'expired', completed_at = now()
WHERE status IN ('pending', 'delivered') AND expires_at <= now();
