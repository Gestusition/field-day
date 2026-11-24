// Package access owns physical access control: zones, groups, time-boxed
// grants, the tap decision and the access event log.
package access

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gestusition/field-day/apps/api/internal/access/store"
	"github.com/Gestusition/field-day/apps/api/internal/credential"
	"github.com/Gestusition/field-day/apps/api/internal/platform/apperr"
	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
	"github.com/Gestusition/field-day/apps/api/internal/platform/database"
	"github.com/Gestusition/field-day/apps/api/internal/platform/page"
	"github.com/Gestusition/field-day/apps/api/internal/user"
	userstore "github.com/Gestusition/field-day/apps/api/internal/user/store"
)

// Decisions and reasons recorded on access events.
const (
	DecisionGranted  = "granted"
	DecisionDenied   = "denied"
	DecisionEnrolled = "enrolled"

	ReasonGranted          = "granted"
	ReasonEnrolled         = "enrolled"
	ReasonUserInactive     = "user_inactive"
	ReasonNotInZone        = "reader_not_in_zone"
	ReasonWrongMode        = "reader_wrong_mode"
	ReasonInsecure         = "insecure_credential"
	ReasonNoGrant          = "no_grant"
	ReasonOutsideSchedule  = "outside_schedule"
	readerModeAccess       = "access"
	maxClockSkewIntoFuture = 5 * time.Minute
)

// Credentials is what access needs from the credential module.
type Credentials interface {
	Resolve(ctx context.Context, readerID uuid.UUID, p credential.Presentation) (credential.Resolution, error)
	TryEnroll(ctx context.Context, readerID uuid.UUID, p credential.Presentation) (*credential.Enrollment, error)
	MarkUsed(ctx context.Context, id uuid.UUID) error
}

// Users is what access needs from the user module.
type Users interface {
	Summary(ctx context.Context, id uuid.UUID) (user.Summary, error)
	GetMany(ctx context.Context, ids []uuid.UUID) ([]userstore.User, error)
}

// Service implements the access use cases.
type Service struct {
	q        *store.Queries
	creds    Credentials
	users    Users
	loc      *time.Location
	unlockMs int
	now      func() time.Time
}

// NewService builds the service.
func NewService(pool *pgxpool.Pool, creds Credentials, users Users, loc *time.Location, unlock time.Duration) *Service {
	return &Service{
		q: store.New(pool), creds: creds, users: users, loc: loc,
		unlockMs: int(unlock / time.Millisecond), now: time.Now,
	}
}

// ---------------------------------------------------------------- tap

// TapRequest is a reader's report of a presented credential.
type TapRequest struct {
	credential.Presentation
	EventID    *uuid.UUID
	OccurredAt *time.Time
}

// TapResult is the decision sent back to the reader.
type TapResult struct {
	EventID     uuid.UUID
	Decision    string
	Reason      string
	UnlockMs    int
	DisplayName *string
}

// Tap decides whether to open, records the event and returns the decision.
// It is idempotent per (reader, EventID).
func (s *Service) Tap(ctx context.Context, d auth.Device, req TapRequest) (TapResult, error) {
	if req.EventID != nil {
		ev, err := s.q.GetEventByDeviceEventID(ctx, store.GetEventByDeviceEventIDParams{
			ReaderID: d.ReaderID, DeviceEventID: req.EventID,
		})
		if err == nil {
			return s.resultFromEvent(ctx, ev), nil
		}
		if !database.IsNotFound(err) {
			return TapResult{}, err
		}
	}

	now := s.now()
	occurred := now
	if req.OccurredAt != nil && !req.OccurredAt.After(now.Add(maxClockSkewIntoFuture)) {
		occurred = *req.OccurredAt
	}

	ev := store.InsertEventParams{
		ReaderID: d.ReaderID, ZoneID: d.ZoneID, PresentedUid: credential.NormalizeUID(req.UID),
		DeviceEventID: req.EventID, OccurredAt: occurred,
	}
	var displayName *string

	enr, err := s.creds.TryEnroll(ctx, d.ReaderID, req.Presentation)
	if err != nil {
		return TapResult{}, err
	}
	if enr != nil {
		ev.CredentialID, ev.UserID = enr.CredentialID, enr.UserID
		if enr.Reason == ReasonEnrolled {
			ev.Decision, ev.Reason = DecisionEnrolled, ReasonEnrolled
		} else {
			ev.Decision, ev.Reason = DecisionDenied, enr.Reason
		}
	} else {
		dec, err := s.decide(ctx, d, req.Presentation, now)
		if err != nil {
			return TapResult{}, err
		}
		ev.CredentialID, ev.UserID = dec.credentialID, dec.userID
		ev.Decision, ev.Reason = DecisionDenied, dec.reason
		if dec.reason == ReasonGranted {
			ev.Decision = DecisionGranted
			displayName = &dec.displayName
			if err := s.creds.MarkUsed(ctx, *dec.credentialID); err != nil {
				return TapResult{}, err
			}
		}
	}

	saved, err := s.q.InsertEvent(ctx, ev)
	if database.IsNotFound(err) {
		// A concurrent retry with the same event id won the insert.
		saved, err = s.q.GetEventByDeviceEventID(ctx, store.GetEventByDeviceEventIDParams{
			ReaderID: d.ReaderID, DeviceEventID: req.EventID,
		})
		if err != nil {
			return TapResult{}, err
		}
		return s.resultFromEvent(ctx, saved), nil
	}
	if err != nil {
		return TapResult{}, err
	}
	return s.result(saved, displayName), nil
}

type decision struct {
	reason       string
	credentialID *uuid.UUID
	userID       *uuid.UUID
	displayName  string
}

func (s *Service) decide(ctx context.Context, d auth.Device, p credential.Presentation, now time.Time) (decision, error) {
	if d.Mode != readerModeAccess {
		return decision{reason: ReasonWrongMode}, nil
	}
	if d.ZoneID == nil {
		return decision{reason: ReasonNotInZone}, nil
	}

	res, err := s.creds.Resolve(ctx, d.ReaderID, p)
	if err != nil {
		return decision{}, err
	}
	dec := decision{credentialID: res.CredentialID, userID: res.UserID}
	if res.Reason != "" {
		dec.reason = res.Reason
		return dec, nil
	}

	u, err := s.users.Summary(ctx, *res.UserID)
	if err != nil {
		return decision{}, err
	}
	if !u.Active {
		dec.reason = ReasonUserInactive
		return dec, nil
	}

	zone, err := s.q.GetZone(ctx, *d.ZoneID)
	if database.IsNotFound(err) {
		dec.reason = ReasonNotInZone // zone deleted between auth and now
		return dec, nil
	}
	if err != nil {
		return decision{}, err
	}
	if zone.RequireSecureCredential && !res.Secure {
		dec.reason = ReasonInsecure
		return dec, nil
	}

	grants, err := s.q.ListCandidateGrants(ctx, store.ListCandidateGrantsParams{
		ZoneID: zone.ID, UserID: res.UserID, At: now,
	})
	if err != nil {
		return decision{}, err
	}
	if len(grants) == 0 {
		dec.reason = ReasonNoGrant
		return dec, nil
	}
	local := now.In(s.loc)
	for _, g := range grants {
		if windowOf(g).Allows(local) {
			dec.reason = ReasonGranted
			dec.displayName = u.DisplayName
			return dec, nil
		}
	}
	dec.reason = ReasonOutsideSchedule
	return dec, nil
}

func (s *Service) result(ev store.AccessEvent, displayName *string) TapResult {
	r := TapResult{EventID: ev.ID, Decision: ev.Decision, Reason: ev.Reason, DisplayName: displayName}
	if ev.Decision == DecisionGranted {
		r.UnlockMs = s.unlockMs
	}
	return r
}

// resultFromEvent rebuilds the response for a retried tap.
func (s *Service) resultFromEvent(ctx context.Context, ev store.AccessEvent) TapResult {
	var name *string
	if ev.Decision == DecisionGranted && ev.UserID != nil {
		if u, err := s.users.Summary(ctx, *ev.UserID); err == nil {
			name = &u.DisplayName
		}
	}
	return s.result(ev, name)
}

func windowOf(g store.AccessGrant) Window {
	return Window{Weekdays: g.Weekdays, Start: pgToMinutes(g.StartTime), End: pgToMinutes(g.EndTime)}
}

// ---------------------------------------------------------------- events

// EventFilter narrows ListEvents.
type EventFilter struct {
	ReaderID, ZoneID, UserID *uuid.UUID
	Decision                 *string
	Since, Until             *time.Time
}

// ListEvents pages through the access log.
func (s *Service) ListEvents(ctx context.Context, p page.Request, f EventFilter) ([]store.AccessEvent, *uuid.UUID, error) {
	rows, err := s.q.ListEvents(ctx, store.ListEventsParams{
		Cursor: p.Cursor, ReaderID: f.ReaderID, ZoneID: f.ZoneID, UserID: f.UserID,
		Decision: f.Decision, Since: f.Since, Until: f.Until, MaxRows: p.FetchRows(),
	})
	if err != nil {
		return nil, nil, err
	}
	rows, next := page.Trim(p, rows, func(e store.AccessEvent) uuid.UUID { return e.ID })
	return rows, next, nil
}

// ---------------------------------------------------------------- zones

var errZoneNotFound = apperr.NotFound("zone_not_found", "Zone not found.")

func (s *Service) ListZones(ctx context.Context) ([]store.Zone, error) { return s.q.ListZones(ctx) }

func (s *Service) GetZone(ctx context.Context, id uuid.UUID) (store.Zone, error) {
	z, err := s.q.GetZone(ctx, id)
	if database.IsNotFound(err) {
		return z, errZoneNotFound
	}
	return z, err
}

func (s *Service) CreateZone(ctx context.Context, name, desc string, secure bool) (store.Zone, error) {
	z, err := s.q.CreateZone(ctx, store.CreateZoneParams{
		Name: strings.TrimSpace(name), Description: desc, RequireSecureCredential: secure,
	})
	return z, mapZoneErr(err)
}

func (s *Service) UpdateZone(ctx context.Context, id uuid.UUID, name, desc string, secure bool) (store.Zone, error) {
	z, err := s.q.UpdateZone(ctx, store.UpdateZoneParams{
		ID: id, Name: strings.TrimSpace(name), Description: desc, RequireSecureCredential: secure,
	})
	return z, mapZoneErr(err)
}

func (s *Service) DeleteZone(ctx context.Context, id uuid.UUID) error {
	n, err := s.q.DeleteZone(ctx, id)
	if err == nil && n == 0 {
		return errZoneNotFound
	}
	return err
}

func mapZoneErr(err error) error {
	switch {
	case database.IsNotFound(err):
		return errZoneNotFound
	case database.IsUniqueViolation(err, "zones_name_key"):
		return apperr.Conflict("zone_name_in_use", "A zone with this name already exists.")
	}
	return err
}

// ---------------------------------------------------------------- groups

var errGroupNotFound = apperr.NotFound("group_not_found", "Access group not found.")

func (s *Service) ListGroups(ctx context.Context) ([]store.ListGroupsRow, error) {
	return s.q.ListGroups(ctx)
}

func (s *Service) GetGroup(ctx context.Context, id uuid.UUID) (store.GetGroupRow, error) {
	g, err := s.q.GetGroup(ctx, id)
	if database.IsNotFound(err) {
		return g, errGroupNotFound
	}
	return g, err
}

func (s *Service) CreateGroup(ctx context.Context, name, desc string) (store.AccessGroup, error) {
	g, err := s.q.CreateGroup(ctx, store.CreateGroupParams{Name: strings.TrimSpace(name), Description: desc})
	return g, mapGroupErr(err)
}

func (s *Service) UpdateGroup(ctx context.Context, id uuid.UUID, name, desc string) (store.AccessGroup, error) {
	g, err := s.q.UpdateGroup(ctx, store.UpdateGroupParams{ID: id, Name: strings.TrimSpace(name), Description: desc})
	return g, mapGroupErr(err)
}

func (s *Service) DeleteGroup(ctx context.Context, id uuid.UUID) error {
	n, err := s.q.DeleteGroup(ctx, id)
	if err == nil && n == 0 {
		return errGroupNotFound
	}
	return err
}

// GroupMembers returns the users in a group.
func (s *Service) GroupMembers(ctx context.Context, id uuid.UUID) ([]userstore.User, error) {
	if _, err := s.GetGroup(ctx, id); err != nil {
		return nil, err
	}
	ids, err := s.q.ListGroupMemberIDs(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.users.GetMany(ctx, ids)
}

func (s *Service) AddMember(ctx context.Context, groupID, userID uuid.UUID) error {
	err := s.q.AddGroupMember(ctx, store.AddGroupMemberParams{GroupID: groupID, UserID: userID})
	switch {
	case database.IsForeignKeyViolation(err, "access_group_members_group_id_fkey"):
		return errGroupNotFound
	case database.IsForeignKeyViolation(err, "access_group_members_user_id_fkey"):
		return apperr.NotFound("user_not_found", "User not found.")
	}
	return err
}

func (s *Service) RemoveMember(ctx context.Context, groupID, userID uuid.UUID) error {
	n, err := s.q.RemoveGroupMember(ctx, store.RemoveGroupMemberParams{GroupID: groupID, UserID: userID})
	if err == nil && n == 0 {
		return apperr.NotFound("member_not_found", "The user is not a member of this group.")
	}
	return err
}

func mapGroupErr(err error) error {
	switch {
	case database.IsNotFound(err):
		return errGroupNotFound
	case database.IsUniqueViolation(err, "access_groups_name_key"):
		return apperr.Conflict("group_name_in_use", "A group with this name already exists.")
	}
	return err
}

// ---------------------------------------------------------------- grants

// GrantInput is a validated request to create a grant.
type GrantInput struct {
	ZoneID             uuid.UUID
	UserID, GroupID    *uuid.UUID
	Weekdays           []int16
	Start, End         *string
	ValidFrom, ValidTo *time.Time
}

func (s *Service) ListGrants(ctx context.Context, zoneID, userID, groupID *uuid.UUID) ([]store.AccessGrant, error) {
	return s.q.ListGrants(ctx, store.ListGrantsParams{ZoneID: zoneID, UserID: userID, GroupID: groupID})
}

func (s *Service) CreateGrant(ctx context.Context, in GrantInput) (store.AccessGrant, error) {
	if (in.UserID == nil) == (in.GroupID == nil) {
		return store.AccessGrant{}, apperr.InvalidField("user_id", "Exactly one of user_id and group_id is required.")
	}
	if (in.Start == nil) != (in.End == nil) {
		return store.AccessGrant{}, apperr.InvalidField("end_time", "start_time and end_time must be given together.")
	}
	var start, end *int
	if in.Start != nil {
		a, err := ParseTimeOfDay(*in.Start)
		if err != nil {
			return store.AccessGrant{}, apperr.InvalidField("start_time", err.Error())
		}
		b, err := ParseTimeOfDay(*in.End)
		if err != nil {
			return store.AccessGrant{}, apperr.InvalidField("end_time", err.Error())
		}
		if a == b {
			return store.AccessGrant{}, apperr.InvalidField("end_time", "end_time must differ from start_time; omit both for all day.")
		}
		start, end = &a, &b
	}
	if in.ValidFrom != nil && in.ValidTo != nil && !in.ValidFrom.Before(*in.ValidTo) {
		return store.AccessGrant{}, apperr.InvalidField("valid_until", "valid_until must be after valid_from.")
	}
	if len(in.Weekdays) == 0 {
		in.Weekdays = []int16{1, 2, 3, 4, 5, 6, 7}
	}

	g, err := s.q.CreateGrant(ctx, store.CreateGrantParams{
		ZoneID: in.ZoneID, UserID: in.UserID, GroupID: in.GroupID, Weekdays: in.Weekdays,
		StartTime: minutesToPG(start), EndTime: minutesToPG(end),
		ValidFrom: in.ValidFrom, ValidUntil: in.ValidTo,
	})
	switch {
	case database.IsForeignKeyViolation(err, "access_grants_zone_id_fkey"):
		return g, errZoneNotFound
	case database.IsForeignKeyViolation(err, "access_grants_user_id_fkey"):
		return g, apperr.NotFound("user_not_found", "User not found.")
	case database.IsForeignKeyViolation(err, "access_grants_group_id_fkey"):
		return g, errGroupNotFound
	}
	return g, err
}

func (s *Service) DeleteGrant(ctx context.Context, id uuid.UUID) error {
	n, err := s.q.DeleteGrant(ctx, id)
	if err == nil && n == 0 {
		return apperr.NotFound("grant_not_found", "Grant not found.")
	}
	return err
}
