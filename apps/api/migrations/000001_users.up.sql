-- Each migration runs in one transaction: it applies fully or not at all.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Shared trigger function: keeps updated_at current on every UPDATE.
CREATE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

-- Users are provisioned just-in-time from Casdoor (subject = OIDC "sub").
-- Admins may pre-register a user by email; the first login with that verified
-- email links the subject to the existing row.
CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    subject       text UNIQUE,
    email         text,
    display_name  text NOT NULL DEFAULT '',
    status        text NOT NULL DEFAULT 'active'
                  CHECK (status IN ('active', 'suspended')),
    last_login_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX users_email_idx ON users (lower(email));

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
