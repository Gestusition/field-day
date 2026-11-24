-- name: InsertAccountIfMissing :exec
INSERT INTO wallet_accounts (user_id, currency) VALUES (@user_id, @currency)
ON CONFLICT (user_id) DO NOTHING;

-- name: GetAccountByUser :one
SELECT * FROM wallet_accounts WHERE user_id = $1;

-- name: LockAccountByUser :one
SELECT * FROM wallet_accounts WHERE user_id = $1 FOR UPDATE;

-- name: SetAccountBalance :one
UPDATE wallet_accounts SET balance_minor = @balance_minor WHERE id = @id RETURNING *;

-- name: InsertTransaction :one
INSERT INTO wallet_transactions (account_id, kind, amount_minor, balance_after_minor, refund_of,
                                 reader_id, credential_id, actor_user_id, description, idempotency_key)
VALUES (@account_id, @kind, @amount_minor, @balance_after_minor, sqlc.narg('refund_of'),
        sqlc.narg('reader_id'), sqlc.narg('credential_id'), sqlc.narg('actor_user_id'), @description, @idempotency_key)
RETURNING *;

-- name: GetTransactionByIdempotencyKey :one
SELECT * FROM wallet_transactions WHERE account_id = @account_id AND idempotency_key = @idempotency_key;

-- name: GetTransaction :one
SELECT * FROM wallet_transactions WHERE id = @id AND account_id = @account_id;

-- name: SumRefundsFor :one
SELECT COALESCE(sum(amount_minor), 0)::bigint FROM wallet_transactions WHERE refund_of = $1;

-- name: ListTransactions :many
SELECT * FROM wallet_transactions
WHERE account_id = @account_id
  AND (sqlc.narg('cursor')::uuid IS NULL OR id < sqlc.narg('cursor')::uuid)
ORDER BY id DESC
LIMIT @max_rows;
