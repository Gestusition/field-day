-- Each migration runs in one transaction: it applies fully or not at all.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- A reader is one embedded unit (STM32 + NFC frontend + ESP32-C3 Wi-Fi).
-- It authenticates with a per-device API key; only its SHA-256 hash is stored.
CREATE TABLE readers (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    name             text NOT NULL,
    serial           text NOT NULL UNIQUE,
    zone_id          uuid REFERENCES zones (id) ON DELETE SET NULL,
    mode             text NOT NULL DEFAULT 'access'
                     CHECK (mode IN ('access', 'wallet')),
    status           text NOT NULL DEFAULT 'active'
                     CHECK (status IN ('active', 'disabled')),
    key_hash         bytea NOT NULL UNIQUE,
    firmware_version text NOT NULL DEFAULT '',
    last_seen_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX readers_zone_id_idx ON readers (zone_id);

CREATE TRIGGER readers_set_updated_at
    BEFORE UPDATE ON readers
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Enrollment: an admin arms a reader, and the next card tapped on it is
-- registered as a new credential (optionally bound to a user).
CREATE TABLE reader_enrollments (
    reader_id  uuid PRIMARY KEY REFERENCES readers (id) ON DELETE CASCADE,
    user_id    uuid REFERENCES users (id) ON DELETE CASCADE,
    label      text NOT NULL DEFAULT '',
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Commands the server asks a reader to execute (e.g. write data to a card).
-- Pushed over MQTT as a notification and always available by HTTP polling.
CREATE TABLE reader_commands (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    reader_id    uuid NOT NULL REFERENCES readers (id) ON DELETE CASCADE,
    type         text NOT NULL
                 CHECK (type IN ('write_card', 'read_card', 'reboot', 'sync')),
    payload      jsonb NOT NULL DEFAULT '{}'::jsonb,
    status       text NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'delivered', 'succeeded', 'failed', 'expired')),
    result       jsonb,
    created_by   uuid REFERENCES users (id) ON DELETE SET NULL,
    expires_at   timestamptz NOT NULL,
    delivered_at timestamptz,
    completed_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX reader_commands_reader_id_idx ON reader_commands (reader_id, id DESC);
CREATE INDEX reader_commands_open_idx ON reader_commands (expires_at)
    WHERE status IN ('pending', 'delivered');

COMMIT;
