-- Each migration runs in one transaction: it applies fully or not at all.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- A credential is anything a reader can identify: a physical NFC card/tag or a
-- phone using Host Card Emulation (HCE). The uid is normalised upper-case hex.
CREATE TABLE credentials (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    kind         text NOT NULL
                 CHECK (kind IN ('desfire', 'mifare_classic', 'mifare_ultralight',
                                 'ntag', 'iso15693', 'mobile_hce')),
    uid          text NOT NULL CHECK (uid ~ '^([0-9A-F]{2}){4,32}$'),
    user_id      uuid REFERENCES users (id) ON DELETE SET NULL,
    label        text NOT NULL DEFAULT '',
    status       text NOT NULL DEFAULT 'active'
                 CHECK (status IN ('active', 'suspended', 'lost', 'revoked')),
    -- AES-GCM sealed HMAC secret, only for mobile_hce credentials.
    secret_enc   bytea,
    last_used_at timestamptz,
    revoked_at   timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT credentials_secret_only_for_hce
        CHECK ((kind = 'mobile_hce') = (secret_enc IS NOT NULL))
);

-- A UID may be re-issued once the previous credential has been revoked.
CREATE UNIQUE INDEX credentials_live_uid_key ON credentials (uid) WHERE status <> 'revoked';
CREATE INDEX credentials_user_id_idx ON credentials (user_id);

CREATE TRIGGER credentials_set_updated_at
    BEFORE UPDATE ON credentials
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMIT;
