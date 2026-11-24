package wallet

import (
	"context"

	"github.com/Gestusition/field-day/apps/api/internal/apigen"
	"github.com/Gestusition/field-day/apps/api/internal/credential"
	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
	"github.com/Gestusition/field-day/apps/api/internal/platform/page"
	"github.com/Gestusition/field-day/apps/api/internal/wallet/store"
)

// Handler implements the wallet operations of apigen.StrictServerInterface.
type Handler struct{ svc *Service }

// NewHandler builds the handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func accountToAPI(a store.WalletAccount) apigen.Wallet {
	return apigen.Wallet{
		Id: a.ID, UserId: a.UserID, Currency: a.Currency, BalanceMinor: a.BalanceMinor,
		Status: apigen.WalletStatus(a.Status), CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
}

func txToAPI(t store.WalletTransaction) apigen.WalletTransaction {
	return apigen.WalletTransaction{
		Id: t.ID, AccountId: t.AccountID, Kind: apigen.WalletTransactionKind(t.Kind),
		AmountMinor: t.AmountMinor, BalanceAfterMinor: t.BalanceAfterMinor, RefundOf: t.RefundOf,
		ReaderId: t.ReaderID, Description: t.Description, CreatedAt: t.CreatedAt,
	}
}

func (h *Handler) GetMyWallet(ctx context.Context, _ apigen.GetMyWalletRequestObject) (apigen.GetMyWalletResponseObject, error) {
	u, err := auth.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	a, err := h.svc.Account(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return apigen.GetMyWallet200JSONResponse(accountToAPI(a)), nil
}

func (h *Handler) ListMyWalletTransactions(ctx context.Context, r apigen.ListMyWalletTransactionsRequestObject) (apigen.ListMyWalletTransactionsResponseObject, error) {
	u, err := auth.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	rows, next, err := h.svc.Transactions(ctx, u.ID, page.New(r.Params.Limit, r.Params.Cursor))
	if err != nil {
		return nil, err
	}
	return apigen.ListMyWalletTransactions200JSONResponse{Items: page.Map(rows, txToAPI), NextCursor: next}, nil
}

func (h *Handler) GetUserWallet(ctx context.Context, r apigen.GetUserWalletRequestObject) (apigen.GetUserWalletResponseObject, error) {
	a, err := h.svc.Account(ctx, r.UserId)
	if err != nil {
		return nil, err
	}
	return apigen.GetUserWallet200JSONResponse(accountToAPI(a)), nil
}

func (h *Handler) ListUserWalletTransactions(ctx context.Context, r apigen.ListUserWalletTransactionsRequestObject) (apigen.ListUserWalletTransactionsResponseObject, error) {
	rows, next, err := h.svc.Transactions(ctx, r.UserId, page.New(r.Params.Limit, r.Params.Cursor))
	if err != nil {
		return nil, err
	}
	return apigen.ListUserWalletTransactions200JSONResponse{Items: page.Map(rows, txToAPI), NextCursor: next}, nil
}

func (h *Handler) CreateUserWalletTransaction(ctx context.Context, r apigen.CreateUserWalletTransactionRequestObject) (apigen.CreateUserWalletTransactionResponseObject, error) {
	actor, err := auth.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	desc := ""
	if r.Body.Description != nil {
		desc = *r.Body.Description
	}
	t, err := h.svc.Post(ctx, r.UserId, actor.ID, AdminEntry{
		Kind: string(r.Body.Kind), Amount: r.Body.AmountMinor, RefundOf: r.Body.RefundOf,
		Description: desc, IdempotencyKey: r.Body.IdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	return apigen.CreateUserWalletTransaction201JSONResponse(txToAPI(t)), nil
}

func (h *Handler) DeviceWalletCharge(ctx context.Context, r apigen.DeviceWalletChargeRequestObject) (apigen.DeviceWalletChargeResponseObject, error) {
	d, err := auth.RequireDevice(ctx)
	if err != nil {
		return nil, err
	}
	desc := ""
	if r.Body.Description != nil {
		desc = *r.Body.Description
	}
	res, err := h.svc.Charge(ctx, d, ChargeRequest{
		Presentation:   credential.PresentationFrom(r.Body.Uid, r.Body.Kind, r.Body.SecureAuth, r.Body.Mobile),
		Amount:         r.Body.AmountMinor,
		Description:    desc,
		IdempotencyKey: r.Body.IdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	return apigen.DeviceWalletCharge200JSONResponse{
		Approved: res.Approved, Reason: apigen.AccessReason(res.Reason),
		BalanceMinor: res.Balance, TransactionId: res.TransactionID,
	}, nil
}
