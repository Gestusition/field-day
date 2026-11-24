-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserBySubject :one
SELECT * FROM users WHERE subject = $1;

-- name: ListUsersByIDs :many
SELECT * FROM users WHERE id = ANY(@ids::uuid[]) ORDER BY display_name, id;

-- name: FindUnlinkedUsersByEmail :many
SELECT * FROM users
WHERE subject IS NULL AND lower(email) = lower(@email::text)
LIMIT 2;

-- name: LinkUserSubject :one
UPDATE users
SET subject       = @subject,
    display_name  = CASE WHEN display_name = '' THEN @display_name::text ELSE display_name END,
    last_login_at = now()
WHERE id = @id AND subject IS NULL
RETURNING *;

-- name: CreateUserFromLogin :one
INSERT INTO users (subject, email, display_name, last_login_at)
VALUES (@subject, sqlc.narg('email'), @display_name, now())
ON CONFLICT (subject) DO UPDATE SET last_login_at = now()
RETURNING *;

-- name: TouchUserLogin :one
UPDATE users
SET last_login_at = now(),
    email         = COALESCE(sqlc.narg('email'), email)
WHERE id = @id
RETURNING *;

-- name: CreateUser :one
INSERT INTO users (email, display_name)
VALUES (@email, @display_name)
RETURNING *;

-- name: CountUsersWithEmail :one
SELECT count(*) FROM users
WHERE lower(email) = lower(@email::text) AND id <> COALESCE(sqlc.narg('exclude_id')::uuid, '00000000-0000-0000-0000-000000000000');

-- name: UpdateUser :one
UPDATE users
SET email        = sqlc.narg('email'),
    display_name = @display_name,
    status       = @status
WHERE id = @id
RETURNING *;

-- name: ListUsers :many
SELECT * FROM users
WHERE (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('search')::text IS NULL
       OR email ILIKE '%' || sqlc.narg('search')::text || '%'
       OR display_name ILIKE '%' || sqlc.narg('search')::text || '%')
ORDER BY id DESC
LIMIT @max_rows;
