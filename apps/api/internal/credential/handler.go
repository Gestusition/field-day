package credential

import (
	"context"
	"time"

	"github.com/Gestusition/field-day/apps/api/internal/apigen"
	"github.com/Gestusition/field-day/apps/api/internal/credential/store"
	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
	"github.com/Gestusition/field-day/apps/api/internal/platform/page"
)

const defaultEnrollmentTTL = 120 * time.Second

// Handler implements the credential operations of apigen.StrictServerInterface.
type Handler struct{ svc *Service }

// NewHandler builds the handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func toAPI(c store.Credential) apigen.Credential {
	return apigen.Credential{
		Id:         c.ID,
		Kind:       apigen.CredentialKind(c.Kind),
		Uid:        c.Uid,
		UserId:     c.UserID,
		Label:      c.Label,
		Status:     apigen.CredentialStatus(c.Status),
		LastUsedAt: c.LastUsedAt,
		RevokedAt:  c.RevokedAt,
		CreatedAt:  c.CreatedAt,
		UpdatedAt:  c.UpdatedAt,
	}
}

// PresentationFrom converts the presentation fields shared by the tap and
// charge requests of the contract.
func PresentationFrom(uid string, kind apigen.CredentialKind, secure *bool, mobile *apigen.MobileProof) Presentation {
	p := Presentation{UID: uid, Kind: string(kind), SecureAuth: secure != nil && *secure}
	if mobile != nil {
		p.Mobile = &MobileProof{Challenge: mobile.Challenge, Response: mobile.Response}
	}
	return p
}

// ---------------------------------------------------------------- me

func (h *Handler) ListMyCredentials(ctx context.Context, _ apigen.ListMyCredentialsRequestObject) (apigen.ListMyCredentialsResponseObject, error) {
	u, err := auth.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := h.svc.ListByUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return apigen.ListMyCredentials200JSONResponse{Items: page.Map(rows, toAPI)}, nil
}

func (h *Handler) ReportMyCredentialLost(ctx context.Context, r apigen.ReportMyCredentialLostRequestObject) (apigen.ReportMyCredentialLostResponseObject, error) {
	u, err := auth.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	c, err := h.svc.ReportLost(ctx, u.ID, r.CredentialId)
	if err != nil {
		return nil, err
	}
	return apigen.ReportMyCredentialLost200JSONResponse(toAPI(c)), nil
}

func (h *Handler) IssueMyMobileCredential(ctx context.Context, r apigen.IssueMyMobileCredentialRequestObject) (apigen.IssueMyMobileCredentialResponseObject, error) {
	u, err := auth.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	label := ""
	if r.Body.Label != nil {
		label = *r.Body.Label
	}
	m, err := h.svc.IssueMobile(ctx, u.ID, label)
	if err != nil {
		return nil, err
	}
	return apigen.IssueMyMobileCredential201JSONResponse{
		Credential: toAPI(m.Credential), Secret: m.Secret, Aid: m.AID,
	}, nil
}

// ---------------------------------------------------------------- admin

func (h *Handler) ListCredentials(ctx context.Context, r apigen.ListCredentialsRequestObject) (apigen.ListCredentialsResponseObject, error) {
	f := ListFilter{UserID: r.Params.UserId, UIDPrefix: r.Params.UidPrefix}
	if r.Params.Status != nil {
		s := string(*r.Params.Status)
		f.Status = &s
	}
	if r.Params.Kind != nil {
		k := string(*r.Params.Kind)
		f.Kind = &k
	}
	rows, next, err := h.svc.List(ctx, page.New(r.Params.Limit, r.Params.Cursor), f)
	if err != nil {
		return nil, err
	}
	return apigen.ListCredentials200JSONResponse{Items: page.Map(rows, toAPI), NextCursor: next}, nil
}

func (h *Handler) CreateCredential(ctx context.Context, r apigen.CreateCredentialRequestObject) (apigen.CreateCredentialResponseObject, error) {
	label := ""
	if r.Body.Label != nil {
		label = *r.Body.Label
	}
	c, err := h.svc.CreatePhysical(ctx, string(r.Body.Kind), r.Body.Uid, r.Body.UserId, label)
	if err != nil {
		return nil, err
	}
	return apigen.CreateCredential201JSONResponse(toAPI(c)), nil
}

func (h *Handler) GetCredential(ctx context.Context, r apigen.GetCredentialRequestObject) (apigen.GetCredentialResponseObject, error) {
	c, err := h.svc.Get(ctx, r.CredentialId)
	if err != nil {
		return nil, err
	}
	return apigen.GetCredential200JSONResponse(toAPI(c)), nil
}

func (h *Handler) UpdateCredential(ctx context.Context, r apigen.UpdateCredentialRequestObject) (apigen.UpdateCredentialResponseObject, error) {
	c, err := h.svc.Update(ctx, r.CredentialId, r.Body.UserId, r.Body.Label, string(r.Body.Status))
	if err != nil {
		return nil, err
	}
	return apigen.UpdateCredential200JSONResponse(toAPI(c)), nil
}

func (h *Handler) StartReaderEnrollment(ctx context.Context, r apigen.StartReaderEnrollmentRequestObject) (apigen.StartReaderEnrollmentResponseObject, error) {
	u, err := auth.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	ttl := defaultEnrollmentTTL
	if r.Body.TtlSeconds != nil {
		ttl = time.Duration(*r.Body.TtlSeconds) * time.Second
	}
	label := ""
	if r.Body.Label != nil {
		label = *r.Body.Label
	}
	e, err := h.svc.StartEnrollment(ctx, r.ReaderId, r.Body.UserId, label, ttl, u.ID)
	if err != nil {
		return nil, err
	}
	return apigen.StartReaderEnrollment201JSONResponse{
		ReaderId: e.ReaderID, UserId: e.UserID, Label: e.Label, ExpiresAt: e.ExpiresAt,
	}, nil
}

func (h *Handler) CancelReaderEnrollment(ctx context.Context, r apigen.CancelReaderEnrollmentRequestObject) (apigen.CancelReaderEnrollmentResponseObject, error) {
	if err := h.svc.CancelEnrollment(ctx, r.ReaderId); err != nil {
		return nil, err
	}
	return apigen.CancelReaderEnrollment204Response{}, nil
}
