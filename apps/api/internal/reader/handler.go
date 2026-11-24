package reader

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Gestusition/field-day/apps/api/internal/apigen"
	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
	"github.com/Gestusition/field-day/apps/api/internal/platform/mqtt"
	"github.com/Gestusition/field-day/apps/api/internal/platform/page"
	"github.com/Gestusition/field-day/apps/api/internal/reader/store"
)

const defaultCommandTTL = 10 * time.Minute

// Handler implements the reader operations of apigen.StrictServerInterface.
type Handler struct {
	svc    *Service
	hceAID string
}

// NewHandler builds the handler.
func NewHandler(svc *Service, hceAID string) *Handler { return &Handler{svc: svc, hceAID: hceAID} }

func toAPI(r store.Reader) apigen.Reader {
	return apigen.Reader{
		Id:              r.ID,
		Name:            r.Name,
		Serial:          r.Serial,
		ZoneId:          r.ZoneID,
		Mode:            apigen.ReaderMode(r.Mode),
		Status:          apigen.ReaderStatus(r.Status),
		FirmwareVersion: r.FirmwareVersion,
		LastSeenAt:      r.LastSeenAt,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

func jsonObject(raw json.RawMessage) map[string]any {
	m := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
	}
	return m
}

func commandToAPI(c store.ReaderCommand) apigen.ReaderCommand {
	out := apigen.ReaderCommand{
		Id:          c.ID,
		ReaderId:    c.ReaderID,
		Type:        apigen.ReaderCommandType(c.Type),
		Payload:     jsonObject(c.Payload),
		Status:      apigen.ReaderCommandStatus(c.Status),
		ExpiresAt:   c.ExpiresAt,
		DeliveredAt: c.DeliveredAt,
		CompletedAt: c.CompletedAt,
		CreatedAt:   c.CreatedAt,
	}
	if len(c.Result) > 0 {
		r := jsonObject(c.Result)
		out.Result = &r
	}
	return out
}

// ---------------------------------------------------------------- admin

func (h *Handler) ListReaders(ctx context.Context, r apigen.ListReadersRequestObject) (apigen.ListReadersResponseObject, error) {
	var status *string
	if r.Params.Status != nil {
		s := string(*r.Params.Status)
		status = &s
	}
	rows, next, err := h.svc.List(ctx, page.New(r.Params.Limit, r.Params.Cursor), r.Params.ZoneId, status)
	if err != nil {
		return nil, err
	}
	return apigen.ListReaders200JSONResponse{Items: page.Map(rows, toAPI), NextCursor: next}, nil
}

func (h *Handler) CreateReader(ctx context.Context, r apigen.CreateReaderRequestObject) (apigen.CreateReaderResponseObject, error) {
	mode := ModeAccess
	if r.Body.Mode != nil {
		mode = string(*r.Body.Mode)
	}
	rd, key, err := h.svc.Create(ctx, r.Body.Name, r.Body.Serial, r.Body.ZoneId, mode)
	if err != nil {
		return nil, err
	}
	return apigen.CreateReader201JSONResponse{Reader: toAPI(rd), ApiKey: key}, nil
}

func (h *Handler) GetReader(ctx context.Context, r apigen.GetReaderRequestObject) (apigen.GetReaderResponseObject, error) {
	rd, err := h.svc.Get(ctx, r.ReaderId)
	if err != nil {
		return nil, err
	}
	return apigen.GetReader200JSONResponse(toAPI(rd)), nil
}

func (h *Handler) UpdateReader(ctx context.Context, r apigen.UpdateReaderRequestObject) (apigen.UpdateReaderResponseObject, error) {
	rd, err := h.svc.Update(ctx, r.ReaderId, r.Body.Name, r.Body.ZoneId, string(r.Body.Mode), string(r.Body.Status))
	if err != nil {
		return nil, err
	}
	return apigen.UpdateReader200JSONResponse(toAPI(rd)), nil
}

func (h *Handler) RotateReaderKey(ctx context.Context, r apigen.RotateReaderKeyRequestObject) (apigen.RotateReaderKeyResponseObject, error) {
	rd, key, err := h.svc.RotateKey(ctx, r.ReaderId)
	if err != nil {
		return nil, err
	}
	return apigen.RotateReaderKey200JSONResponse{Reader: toAPI(rd), ApiKey: key}, nil
}

func (h *Handler) ListReaderCommands(ctx context.Context, r apigen.ListReaderCommandsRequestObject) (apigen.ListReaderCommandsResponseObject, error) {
	rows, next, err := h.svc.ListCommands(ctx, r.ReaderId, page.New(r.Params.Limit, r.Params.Cursor))
	if err != nil {
		return nil, err
	}
	return apigen.ListReaderCommands200JSONResponse{Items: page.Map(rows, commandToAPI), NextCursor: next}, nil
}

func (h *Handler) CreateReaderCommand(ctx context.Context, r apigen.CreateReaderCommandRequestObject) (apigen.CreateReaderCommandResponseObject, error) {
	u, err := auth.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	payload := json.RawMessage(`{}`)
	if r.Body.Payload != nil {
		if payload, err = json.Marshal(r.Body.Payload); err != nil {
			return nil, err
		}
	}
	ttl := defaultCommandTTL
	if r.Body.TtlSeconds != nil {
		ttl = time.Duration(*r.Body.TtlSeconds) * time.Second
	}
	cmd, err := h.svc.CreateCommand(ctx, r.ReaderId, string(r.Body.Type), payload, ttl, u.ID)
	if err != nil {
		return nil, err
	}
	return apigen.CreateReaderCommand201JSONResponse(commandToAPI(cmd)), nil
}

// ---------------------------------------------------------------- device

func (h *Handler) DeviceHeartbeat(ctx context.Context, r apigen.DeviceHeartbeatRequestObject) (apigen.DeviceHeartbeatResponseObject, error) {
	d, err := auth.RequireDevice(ctx)
	if err != nil {
		return nil, err
	}
	rd, err := h.svc.Heartbeat(ctx, d.ReaderID, r.Body.FirmwareVersion)
	if err != nil {
		return nil, err
	}
	return apigen.DeviceHeartbeat200JSONResponse{
		ServerTime: time.Now().UTC(),
		Reader:     toAPI(rd),
		MqttTopic:  mqtt.CommandTopic(rd.ID),
		HceAid:     h.hceAID,
	}, nil
}

func (h *Handler) ListDeviceCommands(ctx context.Context, _ apigen.ListDeviceCommandsRequestObject) (apigen.ListDeviceCommandsResponseObject, error) {
	d, err := auth.RequireDevice(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := h.svc.OpenCommands(ctx, d.ReaderID)
	if err != nil {
		return nil, err
	}
	return apigen.ListDeviceCommands200JSONResponse{Items: page.Map(rows, commandToAPI)}, nil
}

func (h *Handler) ReportDeviceCommandResult(ctx context.Context, r apigen.ReportDeviceCommandResultRequestObject) (apigen.ReportDeviceCommandResultResponseObject, error) {
	d, err := auth.RequireDevice(ctx)
	if err != nil {
		return nil, err
	}
	result := json.RawMessage(`{}`)
	if r.Body.Result != nil {
		if result, err = json.Marshal(r.Body.Result); err != nil {
			return nil, err
		}
	}
	cmd, err := h.svc.CompleteCommand(ctx, d.ReaderID, r.CommandId, string(r.Body.Status), result)
	if err != nil {
		return nil, err
	}
	return apigen.ReportDeviceCommandResult200JSONResponse(commandToAPI(cmd)), nil
}
