package user

import (
	"context"

	"github.com/Gestusition/field-day/apps/api/internal/apigen"
	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
	"github.com/Gestusition/field-day/apps/api/internal/platform/page"
	"github.com/Gestusition/field-day/apps/api/internal/user/store"
)

// Handler implements the user operations of apigen.StrictServerInterface.
type Handler struct{ svc *Service }

// NewHandler builds the handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ToAPI maps a stored user to the contract type. Exported for modules that
// return users (e.g. access group members).
func ToAPI(u store.User) apigen.User {
	return apigen.User{
		Id:          u.ID,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Status:      apigen.UserStatus(u.Status),
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}

func (h *Handler) GetMe(ctx context.Context, _ apigen.GetMeRequestObject) (apigen.GetMeResponseObject, error) {
	p, err := auth.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	u, err := h.svc.Get(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	roles := p.Roles
	if roles == nil {
		roles = []string{}
	}
	a := ToAPI(u)
	return apigen.GetMe200JSONResponse{
		Id: a.Id, Email: a.Email, DisplayName: a.DisplayName, Status: a.Status,
		LastLoginAt: a.LastLoginAt, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		Roles: roles, IsAdmin: p.IsAdmin,
	}, nil
}

func (h *Handler) ListUsers(ctx context.Context, r apigen.ListUsersRequestObject) (apigen.ListUsersResponseObject, error) {
	var status *string
	if r.Params.Status != nil {
		s := string(*r.Params.Status)
		status = &s
	}
	rows, next, err := h.svc.List(ctx, page.New(r.Params.Limit, r.Params.Cursor), status, r.Params.Search)
	if err != nil {
		return nil, err
	}
	return apigen.ListUsers200JSONResponse{Items: page.Map(rows, ToAPI), NextCursor: next}, nil
}

func (h *Handler) CreateUser(ctx context.Context, r apigen.CreateUserRequestObject) (apigen.CreateUserResponseObject, error) {
	u, err := h.svc.Create(ctx, string(r.Body.Email), r.Body.DisplayName)
	if err != nil {
		return nil, err
	}
	return apigen.CreateUser201JSONResponse(ToAPI(u)), nil
}

func (h *Handler) GetUser(ctx context.Context, r apigen.GetUserRequestObject) (apigen.GetUserResponseObject, error) {
	u, err := h.svc.Get(ctx, r.UserId)
	if err != nil {
		return nil, err
	}
	return apigen.GetUser200JSONResponse(ToAPI(u)), nil
}

func (h *Handler) UpdateUser(ctx context.Context, r apigen.UpdateUserRequestObject) (apigen.UpdateUserResponseObject, error) {
	var email *string
	if r.Body.Email != nil {
		e := string(*r.Body.Email)
		email = &e
	}
	u, err := h.svc.Update(ctx, r.UserId, email, r.Body.DisplayName, string(r.Body.Status))
	if err != nil {
		return nil, err
	}
	return apigen.UpdateUser200JSONResponse(ToAPI(u)), nil
}
