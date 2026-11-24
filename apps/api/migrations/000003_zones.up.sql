-- Each migration runs in one transaction: it applies fully or not at all.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- A zone is a physical area (door, lab, building) guarded by one or more readers.
CREATE TABLE zones (
    id                        uuid PRIMARY KEY DEFAULT uuidv7(),
    name                      text NOT NULL UNIQUE,
    description               text NOT NULL DEFAULT '',
    -- When true, only cryptographically authenticated credentials
    -- (DESFire mutual auth or verified HCE) may enter.
    require_secure_credential boolean NOT NULL DEFAULT false,
    created_at                timestamptz NOT NULL DEFAULT now(),
    updated_at                timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER zones_set_updated_at
    BEFORE UPDATE ON zones
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
