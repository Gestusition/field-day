-- Each migration runs in one transaction: it applies fully or not at all.
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Closed-loop stored value (cafeteria, vending, printing...). Amounts are
-- integer minor units (kurus). The balance can never go negative.
CREATE TABLE wallet_accounts (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id       uuid NOT NULL UNIQUE REFERENCES users (id) ON DELETE RESTRICT,
    currency      text NOT NULL DEFAULT 'TRY' CHECK (currency ~ '^[A-Z]{3}$'),
    balance_minor bigint NOT NULL DEFAULT 0 CHECK (balance_minor >= 0),
    status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'frozen')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER wallet_accounts_set_updated_at
    BEFORE UPDATE ON wallet_accounts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Append-only ledger. Charges are negative, top-ups and refunds positive,
-- adjustments either way. balance_after_minor makes audits trivial.
CREATE TABLE wallet_transactions (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    account_id          uuid NOT NULL REFERENCES wallet_accounts (id) ON DELETE RESTRICT,
    kind                text NOT NULL CHECK (kind IN ('topup', 'charge', 'refund', 'adjustment')),
    amount_minor        bigint NOT NULL CHECK (amount_minor <> 0),
    balance_after_minor bigint NOT NULL CHECK (balance_after_minor >= 0),
    refund_of           uuid REFERENCES wallet_transactions (id) ON DELETE RESTRICT,
    reader_id           uuid REFERENCES readers (id) ON DELETE SET NULL,
    credential_id       uuid REFERENCES credentials (id) ON DELETE SET NULL,
    actor_user_id       uuid REFERENCES users (id) ON DELETE SET NULL,
    description         text NOT NULL DEFAULT '',
    idempotency_key     text NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT wallet_transactions_idempotency_key UNIQUE (account_id, idempotency_key),
    CONSTRAINT wallet_transactions_sign CHECK (
        (kind = 'charge' AND amount_minor < 0)
        OR (kind IN ('topup', 'refund') AND amount_minor > 0)
        OR kind = 'adjustment'
    ),
    CONSTRAINT wallet_transactions_refund_link CHECK ((kind = 'refund') = (refund_of IS NOT NULL))
);

CREATE INDEX wallet_transactions_account_id_idx ON wallet_transactions (account_id, id DESC);
CREATE INDEX wallet_transactions_refund_of_idx ON wallet_transactions (refund_of);

COMMIT;
