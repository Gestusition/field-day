-- Each migration runs in one transaction: it applies fully or not at all.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Every state-changing admin request is recorded.
CREATE TABLE audit_logs (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    actor_subject text NOT NULL,
    method        text NOT NULL,
    path          text NOT NULL,
    status_code   integer NOT NULL,
    request_id    text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_actor_user_id_idx ON audit_logs (actor_user_id, id DESC);

COMMIT;
