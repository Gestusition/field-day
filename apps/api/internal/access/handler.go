package access

import (
	"context"

	"github.com/Gestusition/field-day/apps/api/internal/access/store"
	"github.com/Gestusition/field-day/apps/api/internal/apigen"
	"github.com/Gestusition/field-day/apps/api/internal/credential"
	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
	"github.com/Gestusition/field-day/apps/api/internal/platform/page"
	"github.com/Gestusition/field-day/apps/api/internal/user"
)

// Handler implements the access operations of apigen.StrictServerInterface.
type Handler struct{ svc *Service }

// NewHandler builds the handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// ---------------------------------------------------------------- mapping

func zoneToAPI(z store.Zone) apigen.Zone {
	return apigen.Zone{
		Id: z.ID, Name: z.Name, Description: z.Description,
		RequireSecureCredential: z.RequireSecureCredential, CreatedAt: z.CreatedAt, UpdatedAt: z.UpdatedAt,
	}
}

func groupToAPI(g store.ListGroupsRow) apigen.AccessGroup {
	return apigen.AccessGroup{
		Id: g.ID, Name: g.Name, Description: g.Description, MemberCount: g.MemberCount,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
	}
}

func grantToAPI(g store.AccessGrant) apigen.AccessGrant {
	days := make([]int, len(g.Weekdays))
	for i, d := range g.Weekdays {
		days[i] = int(d)
	}
	out := apigen.AccessGrant{
		Id: g.ID, ZoneId: g.ZoneID, UserId: g.UserID, GroupId: g.GroupID, Weekdays: days,
		ValidFrom: g.ValidFrom, ValidUntil: g.ValidUntil, CreatedAt: g.CreatedAt,
	}
	if m := pgToMinutes(g.StartTime); m != nil {
		s := FormatTimeOfDay(*m)
		out.StartTime = &s
	}
	if m := pgToMinutes(g.EndTime); m != nil {
		s := FormatTimeOfDay(*m)
		out.EndTime = &s
	}
	return out
}

func eventToAPI(e store.AccessEvent) apigen.AccessEvent {
	return apigen.AccessEvent{
		Id: e.ID, ReaderId: e.ReaderID, ZoneId: e.ZoneID, CredentialId: e.CredentialID, UserId: e.UserID,
		PresentedUid: e.PresentedUid, Decision: apigen.AccessDecision(e.Decision),
		Reason: apigen.AccessReason(e.Reason), OccurredAt: e.OccurredAt, CreatedAt: e.CreatedAt,
	}
}

func derefOr(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

// ---------------------------------------------------------------- device

func (h *Handler) DeviceTap(ctx context.Context, r apigen.DeviceTapRequestObject) (apigen.DeviceTapResponseObject, error) {
	d, err := auth.RequireDevice(ctx)
	if err != nil {
		return nil, err
	}
	res, err := h.svc.Tap(ctx, d, TapRequest{
		Presentation: credential.PresentationFrom(r.Body.Uid, r.Body.Kind, r.Body.SecureAuth, r.Body.Mobile),
		EventID:      r.Body.EventId,
		OccurredAt:   r.Body.OccurredAt,
	})
	if err != nil {
		return nil, err
	}
	return apigen.DeviceTap200JSONResponse{
		EventId: res.EventID, Decision: apigen.AccessDecision(res.Decision),
		Reason: apigen.AccessReason(res.Reason), UnlockMs: res.UnlockMs, DisplayName: res.DisplayName,
	}, nil
}

// ---------------------------------------------------------------- events

func (h *Handler) ListMyAccessEvents(ctx context.Context, r apigen.ListMyAccessEventsRequestObject) (apigen.ListMyAccessEventsResponseObject, error) {
	u, err := auth.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	rows, next, err := h.svc.ListEvents(ctx, page.New(r.Params.Limit, r.Params.Cursor), EventFilter{UserID: &u.ID})
	if err != nil {
		return nil, err
	}
	return apigen.ListMyAccessEvents200JSONResponse{Items: page.Map(rows, eventToAPI), NextCursor: next}, nil
}

func (h *Handler) ListAccessEvents(ctx context.Context, r apigen.ListAccessEventsRequestObject) (apigen.ListAccessEventsResponseObject, error) {
	f := EventFilter{
		ReaderID: r.Params.ReaderId, ZoneID: r.Params.ZoneId, UserID: r.Params.UserId,
		Since: r.Params.Since, Until: r.Params.Until,
	}
	if r.Params.Decision != nil {
		d := string(*r.Params.Decision)
		f.Decision = &d
	}
	rows, next, err := h.svc.ListEvents(ctx, page.New(r.Params.Limit, r.Params.Cursor), f)
	if err != nil {
		return nil, err
	}
	return apigen.ListAccessEvents200JSONResponse{Items: page.Map(rows, eventToAPI), NextCursor: next}, nil
}

// ---------------------------------------------------------------- zones

func (h *Handler) ListZones(ctx context.Context, _ apigen.ListZonesRequestObject) (apigen.ListZonesResponseObject, error) {
	rows, err := h.svc.ListZones(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.ListZones200JSONResponse{Items: page.Map(rows, zoneToAPI)}, nil
}

func (h *Handler) CreateZone(ctx context.Context, r apigen.CreateZoneRequestObject) (apigen.CreateZoneResponseObject, error) {
	z, err := h.svc.CreateZone(ctx, r.Body.Name, derefOr(r.Body.Description, ""),
		r.Body.RequireSecureCredential != nil && *r.Body.RequireSecureCredential)
	if err != nil {
		return nil, err
	}
	return apigen.CreateZone201JSONResponse(zoneToAPI(z)), nil
}

func (h *Handler) GetZone(ctx context.Context, r apigen.GetZoneRequestObject) (apigen.GetZoneResponseObject, error) {
	z, err := h.svc.GetZone(ctx, r.ZoneId)
	if err != nil {
		return nil, err
	}
	return apigen.GetZone200JSONResponse(zoneToAPI(z)), nil
}

func (h *Handler) UpdateZone(ctx context.Context, r apigen.UpdateZoneRequestObject) (apigen.UpdateZoneResponseObject, error) {
	z, err := h.svc.UpdateZone(ctx, r.ZoneId, r.Body.Name, derefOr(r.Body.Description, ""),
		r.Body.RequireSecureCredential != nil && *r.Body.RequireSecureCredential)
	if err != nil {
		return nil, err
	}
	return apigen.UpdateZone200JSONResponse(zoneToAPI(z)), nil
}

func (h *Handler) DeleteZone(ctx context.Context, r apigen.DeleteZoneRequestObject) (apigen.DeleteZoneResponseObject, error) {
	if err := h.svc.DeleteZone(ctx, r.ZoneId); err != nil {
		return nil, err
	}
	return apigen.DeleteZone204Response{}, nil
}

// ---------------------------------------------------------------- groups

func (h *Handler) ListAccessGroups(ctx context.Context, _ apigen.ListAccessGroupsRequestObject) (apigen.ListAccessGroupsResponseObject, error) {
	rows, err := h.svc.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	return apigen.ListAccessGroups200JSONResponse{Items: page.Map(rows, groupToAPI)}, nil
}

func (h *Handler) CreateAccessGroup(ctx context.Context, r apigen.CreateAccessGroupRequestObject) (apigen.CreateAccessGroupResponseObject, error) {
	g, err := h.svc.CreateGroup(ctx, r.Body.Name, derefOr(r.Body.Description, ""))
	if err != nil {
		return nil, err
	}
	return apigen.CreateAccessGroup201JSONResponse(groupToAPI(store.ListGroupsRow{
		ID: g.ID, Name: g.Name, Description: g.Description, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
	})), nil
}

func (h *Handler) GetAccessGroup(ctx context.Context, r apigen.GetAccessGroupRequestObject) (apigen.GetAccessGroupResponseObject, error) {
	g, err := h.svc.GetGroup(ctx, r.GroupId)
	if err != nil {
		return nil, err
	}
	return apigen.GetAccessGroup200JSONResponse(groupToAPI(store.ListGroupsRow(g))), nil
}

func (h *Handler) UpdateAccessGroup(ctx context.Context, r apigen.UpdateAccessGroupRequestObject) (apigen.UpdateAccessGroupResponseObject, error) {
	if _, err := h.svc.UpdateGroup(ctx, r.GroupId, r.Body.Name, derefOr(r.Body.Description, "")); err != nil {
		return nil, err
	}
	g, err := h.svc.GetGroup(ctx, r.GroupId)
	if err != nil {
		return nil, err
	}
	return apigen.UpdateAccessGroup200JSONResponse(groupToAPI(store.ListGroupsRow(g))), nil
}

func (h *Handler) DeleteAccessGroup(ctx context.Context, r apigen.DeleteAccessGroupRequestObject) (apigen.DeleteAccessGroupResponseObject, error) {
	if err := h.svc.DeleteGroup(ctx, r.GroupId); err != nil {
		return nil, err
	}
	return apigen.DeleteAccessGroup204Response{}, nil
}

func (h *Handler) ListAccessGroupMembers(ctx context.Context, r apigen.ListAccessGroupMembersRequestObject) (apigen.ListAccessGroupMembersResponseObject, error) {
	users, err := h.svc.GroupMembers(ctx, r.GroupId)
	if err != nil {
		return nil, err
	}
	return apigen.ListAccessGroupMembers200JSONResponse{Items: page.Map(users, user.ToAPI)}, nil
}

func (h *Handler) AddAccessGroupMember(ctx context.Context, r apigen.AddAccessGroupMemberRequestObject) (apigen.AddAccessGroupMemberResponseObject, error) {
	if err := h.svc.AddMember(ctx, r.GroupId, r.UserId); err != nil {
		return nil, err
	}
	return apigen.AddAccessGroupMember204Response{}, nil
}

func (h *Handler) RemoveAccessGroupMember(ctx context.Context, r apigen.RemoveAccessGroupMemberRequestObject) (apigen.RemoveAccessGroupMemberResponseObject, error) {
	if err := h.svc.RemoveMember(ctx, r.GroupId, r.UserId); err != nil {
		return nil, err
	}
	return apigen.RemoveAccessGroupMember204Response{}, nil
}

// ---------------------------------------------------------------- grants

func (h *Handler) ListAccessGrants(ctx context.Context, r apigen.ListAccessGrantsRequestObject) (apigen.ListAccessGrantsResponseObject, error) {
	rows, err := h.svc.ListGrants(ctx, r.Params.ZoneId, r.Params.UserId, r.Params.GroupId)
	if err != nil {
		return nil, err
	}
	return apigen.ListAccessGrants200JSONResponse{Items: page.Map(rows, grantToAPI)}, nil
}

func (h *Handler) CreateAccessGrant(ctx context.Context, r apigen.CreateAccessGrantRequestObject) (apigen.CreateAccessGrantResponseObject, error) {
	in := GrantInput{
		ZoneID: r.Body.ZoneId, UserID: r.Body.UserId, GroupID: r.Body.GroupId,
		Start: r.Body.StartTime, End: r.Body.EndTime, ValidFrom: r.Body.ValidFrom, ValidTo: r.Body.ValidUntil,
	}
	if r.Body.Weekdays != nil {
		for _, d := range *r.Body.Weekdays {
			in.Weekdays = append(in.Weekdays, int16(d)) //nolint:gosec // validated 1..7 by the contract
		}
	}
	g, err := h.svc.CreateGrant(ctx, in)
	if err != nil {
		return nil, err
	}
	return apigen.CreateAccessGrant201JSONResponse(grantToAPI(g)), nil
}

func (h *Handler) DeleteAccessGrant(ctx context.Context, r apigen.DeleteAccessGrantRequestObject) (apigen.DeleteAccessGrantResponseObject, error) {
	if err := h.svc.DeleteGrant(ctx, r.GrantId); err != nil {
		return nil, err
	}
	return apigen.DeleteAccessGrant204Response{}, nil
}
