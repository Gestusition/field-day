// Package wallet owns the closed-loop stored-value accounts: balances, the
// append-only ledger, admin top-ups/refunds and card payments at readers.
//
// Every balance change happens in one transaction that locks the account row
// (SELECT ... FOR UPDATE), so concurrent charges can never overspend.
package wallet

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gestusition/field-day/apps/api/internal/credential"
	"github.com/Gestusition/field-day/apps/api/internal/platform/apperr"
	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
	"github.com/Gestusition/field-day/apps/api/internal/platform/database"
	"github.com/Gestusition/field-day/apps/api/internal/platform/page"
	"github.com/Gestusition/field-day/apps/api/internal/user"
	"github.com/Gestusition/field-day/apps/api/internal/wallet/store"
)

const (
	KindTopup      = "topup"
	KindCharge     = "charge"
	KindRefund     = "refund"
	KindAdjustment = "adjustment"

	StatusFrozen = "frozen"

	ReasonApproved          = "granted"
	ReasonWrongMode         = "reader_wrong_mode"
	ReasonUserInactive      = "user_inactive"
	ReasonInsufficientFunds = "insufficient_funds"
	ReasonFrozen            = "wallet_frozen"

	readerModeWallet = "wallet"
)

// Credentials is what wallet needs from the credential module.
type Credentials interface {
	Resolve(ctx context.Context, readerID uuid.UUID, p credential.Presentation) (credential.Resolution, error)
	MarkUsed(ctx context.Context, id uuid.UUID) error
}

// Users is what wallet needs from the user module.
type Users interface {
	Summary(ctx context.Context, id uuid.UUID) (user.Summary, error)
}

// Service implements the wallet use cases.
type Service struct {
	pool     *pgxpool.Pool
	q        *store.Queries
	creds    Credentials
	users    Users
	currency string
	log      *slog.Logger
}

// NewService builds the service.
func NewService(pool *pgxpool.Pool, creds Credentials, users Users, currency string, log *slog.Logger) *Service {
	return &Service{pool: pool, q: store.New(pool), creds: creds, users: users, currency: currency, log: log}
}

// Account returns a user's wallet, opening it on first access.
func (s *Service) Account(ctx context.Context, userID uuid.UUID) (store.WalletAccount, error) {
	err := s.q.InsertAccountIfMissing(ctx, store.InsertAccountIfMissingParams{UserID: userID, Currency: s.currency})
	if database.IsForeignKeyViolation(err) {
		return store.WalletAccount{}, apperr.NotFound("user_not_found", "User not found.")
	}
	if err != nil {
		return store.WalletAccount{}, err
	}
	return s.q.GetAccountByUser(ctx, userID)
}

// Transactions pages through a user's ledger.
func (s *Service) Transactions(ctx context.Context, userID uuid.UUID, p page.Request) ([]store.WalletTransaction, *uuid.UUID, error) {
	acc, err := s.Account(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.q.ListTransactions(ctx, store.ListTransactionsParams{
		AccountID: acc.ID, Cursor: p.Cursor, MaxRows: p.FetchRows(),
	})
	if err != nil {
		return nil, nil, err
	}
	rows, next := page.Trim(p, rows, func(t store.WalletTransaction) uuid.UUID { return t.ID })
	return rows, next, nil
}

// AdminEntry is an admin-initiated ledger entry.
type AdminEntry struct {
	Kind           string // topup | refund | adjustment
	Amount         int64
	RefundOf       *uuid.UUID
	Description    string
	IdempotencyKey string
}

// Post records an admin top-up, refund or adjustment.
func (s *Service) Post(ctx context.Context, userID, actorID uuid.UUID, e AdminEntry) (store.WalletTransaction, error) {
	switch {
	case e.Kind == KindTopup && e.Amount <= 0:
		return store.WalletTransaction{}, apperr.InvalidField("amount_minor", "A top-up must be positive.")
	case e.Kind == KindRefund && e.Amount <= 0:
		return store.WalletTransaction{}, apperr.InvalidField("amount_minor", "A refund must be positive.")
	case e.Kind == KindRefund && e.RefundOf == nil:
		return store.WalletTransaction{}, apperr.InvalidField("refund_of", "A refund must reference the charge it refunds.")
	case e.Kind != KindRefund && e.RefundOf != nil:
		return store.WalletTransaction{}, apperr.InvalidField("refund_of", "Only refunds may reference a charge.")
	case e.Amount == 0:
		return store.WalletTransaction{}, apperr.InvalidField("amount_minor", "Amount must not be zero.")
	}
	if _, err := s.Account(ctx, userID); err != nil {
		return store.WalletTransaction{}, err
	}

	var out store.WalletTransaction
	err := database.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		acc, err := q.LockAccountByUser(ctx, userID)
		if err != nil {
			return err
		}
		if prev, ok, err := existing(ctx, q, acc.ID, e.IdempotencyKey); err != nil || ok {
			if ok && (prev.Kind != e.Kind || prev.AmountMinor != e.Amount) {
				return apperr.Conflict("idempotency_key_reused", "This idempotency key was used for a different transaction.")
			}
			out = prev
			return err
		}
		if e.Kind == KindRefund {
			if err := checkRefund(ctx, q, acc.ID, *e.RefundOf, e.Amount); err != nil {
				return err
			}
		}
		balance := acc.BalanceMinor + e.Amount
		if balance < 0 {
			return apperr.Unprocessable("insufficient_funds", "The adjustment would make the balance negative.")
		}
		out, err = apply(ctx, q, acc, store.InsertTransactionParams{
			Kind: e.Kind, AmountMinor: e.Amount, RefundOf: e.RefundOf, ActorUserID: &actorID,
			Description: e.Description, IdempotencyKey: e.IdempotencyKey,
		}, balance)
		return err
	})
	return out, err
}

func checkRefund(ctx context.Context, q *store.Queries, accountID, chargeID uuid.UUID, amount int64) error {
	charge, err := q.GetTransaction(ctx, store.GetTransactionParams{ID: chargeID, AccountID: accountID})
	if database.IsNotFound(err) {
		return apperr.NotFound("transaction_not_found", "The referenced charge does not exist on this wallet.")
	}
	if err != nil {
		return err
	}
	if charge.Kind != KindCharge {
		return apperr.Unprocessable("not_a_charge", "Only charges can be refunded.")
	}
	refunded, err := q.SumRefundsFor(ctx, &chargeID)
	if err != nil {
		return err
	}
	if refunded+amount > -charge.AmountMinor {
		return apperr.Unprocessable("refund_exceeds_charge", "The refund exceeds the remaining refundable amount.")
	}
	return nil
}

// ChargeRequest is a payment at a wallet-mode reader.
type ChargeRequest struct {
	credential.Presentation
	Amount         int64
	Description    string
	IdempotencyKey string
}

// ChargeResult is returned to the reader.
type ChargeResult struct {
	Approved      bool
	Reason        string
	Balance       *int64
	TransactionID *uuid.UUID
}

// Charge debits the wallet of the presented credential's owner.
func (s *Service) Charge(ctx context.Context, d auth.Device, req ChargeRequest) (ChargeResult, error) {
	if d.Mode != readerModeWallet {
		return ChargeResult{Reason: ReasonWrongMode}, nil
	}
	res, err := s.creds.Resolve(ctx, d.ReaderID, req.Presentation)
	if err != nil {
		return ChargeResult{}, err
	}
	if res.Reason != "" {
		return ChargeResult{Reason: res.Reason}, nil
	}
	u, err := s.users.Summary(ctx, *res.UserID)
	if err != nil {
		return ChargeResult{}, err
	}
	if !u.Active {
		return ChargeResult{Reason: ReasonUserInactive}, nil
	}
	if _, err := s.Account(ctx, u.ID); err != nil {
		return ChargeResult{}, err
	}

	var out ChargeResult
	err = database.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		acc, err := q.LockAccountByUser(ctx, u.ID)
		if err != nil {
			return err
		}
		if prev, ok, err := existing(ctx, q, acc.ID, req.IdempotencyKey); err != nil || ok {
			if ok {
				out = ChargeResult{Approved: true, Reason: ReasonApproved, Balance: &acc.BalanceMinor, TransactionID: &prev.ID}
			}
			return err
		}
		balance := acc.BalanceMinor
		switch {
		case acc.Status == StatusFrozen:
			out = ChargeResult{Reason: ReasonFrozen, Balance: &balance}
			return nil
		case acc.BalanceMinor < req.Amount:
			out = ChargeResult{Reason: ReasonInsufficientFunds, Balance: &balance}
			return nil
		}
		balance -= req.Amount
		t, err := apply(ctx, q, acc, store.InsertTransactionParams{
			Kind: KindCharge, AmountMinor: -req.Amount, ReaderID: &d.ReaderID, CredentialID: res.CredentialID,
			Description: req.Description, IdempotencyKey: req.IdempotencyKey,
		}, balance)
		if err != nil {
			return err
		}
		out = ChargeResult{Approved: true, Reason: ReasonApproved, Balance: &balance, TransactionID: &t.ID}
		return nil
	})
	if err != nil {
		return ChargeResult{}, err
	}
	if out.Approved {
		if err := s.creds.MarkUsed(ctx, *res.CredentialID); err != nil {
			s.log.WarnContext(ctx, "mark credential used", "err", err)
		}
	}
	return out, nil
}

func existing(ctx context.Context, q *store.Queries, accountID uuid.UUID, key string) (store.WalletTransaction, bool, error) {
	t, err := q.GetTransactionByIdempotencyKey(ctx, store.GetTransactionByIdempotencyKeyParams{
		AccountID: accountID, IdempotencyKey: key,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return t, false, nil
	}
	return t, err == nil, err
}

func apply(ctx context.Context, q *store.Queries, acc store.WalletAccount, p store.InsertTransactionParams, balance int64) (store.WalletTransaction, error) {
	p.AccountID = acc.ID
	p.BalanceAfterMinor = balance
	t, err := q.InsertTransaction(ctx, p)
	if err != nil {
		return t, err
	}
	_, err = q.SetAccountBalance(ctx, store.SetAccountBalanceParams{ID: acc.ID, BalanceMinor: balance})
	return t, err
}
