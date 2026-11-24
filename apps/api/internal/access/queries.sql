-- ---------------------------------------------------------------- zones

-- name: ListZones :many
SELECT * FROM zones ORDER BY name;

-- name: GetZone :one
SELECT * FROM zones WHERE id = $1;

-- name: CreateZone :one
INSERT INTO zones (name, description, require_secure_credential)
VALUES (@name, @description, @require_secure_credential)
RETURNING *;

-- name: UpdateZone :one
UPDATE zones
SET name = @name, description = @description, require_secure_credential = @require_secure_credential
WHERE id = @id
RETURNING *;

-- name: DeleteZone :execrows
DELETE FROM zones WHERE id = $1;

-- ---------------------------------------------------------------- groups

-- name: ListGroups :many
SELECT g.id, g.name, g.description, g.created_at, g.updated_at,
       (SELECT count(*) FROM access_group_members m WHERE m.group_id = g.id)::bigint AS member_count
FROM access_groups g
ORDER BY g.name;

-- name: GetGroup :one
SELECT g.id, g.name, g.description, g.created_at, g.updated_at,
       (SELECT count(*) FROM access_group_members m WHERE m.group_id = g.id)::bigint AS member_count
FROM access_groups g
WHERE g.id = $1;

-- name: CreateGroup :one
INSERT INTO access_groups (name, description) VALUES (@name, @description) RETURNING *;

-- name: UpdateGroup :one
UPDATE access_groups SET name = @name, description = @description WHERE id = @id RETURNING *;

-- name: DeleteGroup :execrows
DELETE FROM access_groups WHERE id = $1;

-- name: ListGroupMemberIDs :many
SELECT user_id FROM access_group_members WHERE group_id = $1 ORDER BY created_at;

-- name: AddGroupMember :exec
INSERT INTO access_group_members (group_id, user_id) VALUES (@group_id, @user_id)
ON CONFLICT DO NOTHING;

-- name: RemoveGroupMember :execrows
DELETE FROM access_group_members WHERE group_id = @group_id AND user_id = @user_id;

-- ---------------------------------------------------------------- grants

-- name: ListGrants :many
SELECT * FROM access_grants
WHERE (sqlc.narg('zone_id')::uuid IS NULL OR zone_id = sqlc.narg('zone_id')::uuid)
  AND (sqlc.narg('user_id')::uuid IS NULL OR user_id = sqlc.narg('user_id')::uuid)
  AND (sqlc.narg('group_id')::uuid IS NULL OR group_id = sqlc.narg('group_id')::uuid)
ORDER BY id DESC
LIMIT 500;

-- name: CreateGrant :one
INSERT INTO access_grants (zone_id, user_id, group_id, weekdays, start_time, end_time, valid_from, valid_until)
VALUES (@zone_id, sqlc.narg('user_id'), sqlc.narg('group_id'), @weekdays,
        sqlc.narg('start_time'), sqlc.narg('end_time'), sqlc.narg('valid_from'), sqlc.narg('valid_until'))
RETURNING *;

-- name: DeleteGrant :execrows
DELETE FROM access_grants WHERE id = $1;

-- name: ListCandidateGrants :many
-- Grants for a user in a zone that are inside their validity period. The
-- weekday / time-of-day window is evaluated in Go in the site time zone.
SELECT g.* FROM access_grants g
WHERE g.zone_id = @zone_id
  AND (g.user_id = @user_id
       OR g.group_id IN (SELECT m.group_id FROM access_group_members m WHERE m.user_id = @user_id))
  AND (g.valid_from IS NULL OR g.valid_from <= @at::timestamptz)
  AND (g.valid_until IS NULL OR g.valid_until > @at::timestamptz);

-- ---------------------------------------------------------------- events

-- name: InsertEvent :one
INSERT INTO access_events (reader_id, zone_id, credential_id, user_id, presented_uid,
                           decision, reason, device_event_id, occurred_at)
VALUES (@reader_id, sqlc.narg('zone_id'), sqlc.narg('credential_id'), sqlc.narg('user_id'), @presented_uid,
        @decision, @reason, sqlc.narg('device_event_id'), @occurred_at)
ON CONFLICT (reader_id, device_event_id) WHERE device_event_id IS NOT NULL DO NOTHING
RETURNING *;

-- name: GetEventByDeviceEventID :one
SELECT * FROM access_events WHERE reader_id = @reader_id AND device_event_id = @device_event_id;

-- name: ListEvents :many
SELECT * FROM access_events
WHERE (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
  AND (sqlc.narg('reader_id')::uuid IS NULL OR reader_id = sqlc.narg('reader_id')::uuid)
  AND (sqlc.narg('zone_id')::uuid IS NULL OR zone_id = sqlc.narg('zone_id')::uuid)
  AND (sqlc.narg('user_id')::uuid IS NULL OR user_id = sqlc.narg('user_id')::uuid)
  AND (sqlc.narg('decision')::text IS NULL OR decision = sqlc.narg('decision')::text)
  AND (sqlc.narg('since')::timestamptz IS NULL OR occurred_at >= sqlc.narg('since')::timestamptz)
  AND (sqlc.narg('until')::timestamptz IS NULL OR occurred_at < sqlc.narg('until')::timestamptz)
ORDER BY id DESC
LIMIT @max_rows;
