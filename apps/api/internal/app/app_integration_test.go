package app_test

// End-to-end tests: real PostgreSQL (Testcontainers), real migrations, the
// full HTTP stack including contract validation. Only the identity provider
// is replaced by a static token table.
//
// Skipped with `go test -short` or when Docker is unavailable.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Gestusition/field-day/apps/api/internal/app"
	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
	"github.com/Gestusition/field-day/apps/api/internal/platform/config"
	"github.com/Gestusition/field-day/apps/api/internal/platform/secret"
)

// staticTokens maps bearer tokens to claims.
type staticTokens map[string]auth.Claims

func (s staticTokens) Verify(_ context.Context, raw string) (auth.Claims, error) {
	c, ok := s[raw]
	if !ok {
		return auth.Claims{}, errors.New("invalid token")
	}
	return c, nil
}

var tokens = staticTokens{
	"admin": {Subject: "sub-admin", Email: "admin@example.com", EmailVerified: true, Name: "Admin", Roles: []string{"admin"}},
	"ada":   {Subject: "sub-ada", Email: "ada@example.com", EmailVerified: true, Name: "Ada Lovelace"},
	"grace": {Subject: "sub-grace", Email: "grace@example.com", EmailVerified: true, Name: "Grace Hopper"},
}

type client struct {
	t   *testing.T
	srv *httptest.Server
}

type resp struct {
	Status int
	Body   map[string]any
}

func (r resp) str(k string) string { s, _ := r.Body[k].(string); return s }

func (r resp) items() []any { i, _ := r.Body["items"].([]any); return i }

func (c *client) do(method, path, token string, body any) resp {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(c.t, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.srv.URL+path, rd)
	require.NoError(c.t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.srv.Client().Do(req)
	require.NoError(c.t, err)
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	out := resp{Status: res.StatusCode, Body: map[string]any{}}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out.Body)
	}
	return out
}

// must asserts the status and returns the response.
func (c *client) must(status int, method, path, token string, body any) resp {
	c.t.Helper()
	r := c.do(method, path, token, body)
	require.Equalf(c.t, status, r.Status, "%s %s -> %v", method, path, r.Body)
	return r
}

// setup starts the API with the static test tokens (OIDC mode).
func setup(t *testing.T) *client {
	t.Helper()
	return setupWith(t, nil, app.Options{Tokens: tokens})
}

// setupWith starts PostgreSQL and the API; mutate may adjust the config.
func setupWith(t *testing.T, mutate func(*config.Config), opts app.Options) *client {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)
	ctx := context.Background()

	pg, err := tcpostgres.Run(ctx, "postgres:18-alpine",
		tcpostgres.WithDatabase("fieldday"),
		tcpostgres.WithUsername("fieldday"),
		tcpostgres.WithPassword("fieldday"),
		tcpostgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, pg)
	require.NoError(t, err)
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	cfg := &config.Config{
		Env: "test", DatabaseURL: dsn, DatabaseMaxConns: 5, MigrateOnStart: true,
		AuthMode:   config.AuthModeOIDC,
		OIDCIssuer: "http://unused", OIDCAudiences: []string{"test"}, OIDCAdminRole: "admin",
		CredentialEncryptionKey: base64.StdEncoding.EncodeToString(secret.Random(32)),
		HCEAID:                  "F04649454C4444", SiteTimezone: "Europe/Istanbul",
		UnlockDuration: 3 * time.Second, WalletCurrency: "TRY", LogFormat: "text",
		ShutdownTimeout: time.Second,
	}
	if mutate != nil {
		mutate(cfg)
	}
	require.NoError(t, cfg.Finalize())

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := app.New(ctx, cfg, log, opts)
	require.NoError(t, err)
	t.Cleanup(a.Close)

	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	return &client{t: t, srv: srv}
}

// TestDevAuthMode covers `bun run dev`: no identity provider, "Bearer dev".
func TestDevAuthMode(t *testing.T) {
	c := setupWith(t, func(cfg *config.Config) {
		cfg.Env = "development"
		cfg.AuthMode = config.AuthModeDev
		cfg.OIDCIssuer, cfg.OIDCAudiences = "", nil
	}, app.Options{})

	me := c.must(200, "GET", "/api/me", "dev", nil)
	assert.Equal(t, true, me.Body["is_admin"])
	assert.Equal(t, "Developer", me.str("display_name"))
	c.must(201, "POST", "/api/admin/zones", "dev", map[string]any{"name": "Lab"})
	c.must(401, "GET", "/api/me", "not-dev", nil)
	c.must(401, "GET", "/api/me", "", nil)

	// The root address lands on the interactive docs.
	c.must(200, "GET", "/", "", nil)
}

// isoToday returns today's ISO weekday in the site time zone.
func isoToday(t *testing.T) int {
	loc, err := time.LoadLocation("Europe/Istanbul")
	require.NoError(t, err)
	wd := int(time.Now().In(loc).Weekday())
	if wd == 0 {
		return 7
	}
	return wd
}

func TestAPI(t *testing.T) {
	c := setup(t)

	t.Run("operational endpoints", func(t *testing.T) {
		c.must(200, "GET", "/healthz", "", nil)
		c.must(200, "GET", "/readyz", "", nil)
		spec := c.must(200, "GET", "/api/openapi.json", "", nil)
		assert.Equal(t, "3.0.3", spec.str("openapi"))
		r := c.must(404, "GET", "/api/nope", "", nil)
		assert.Equal(t, "route_not_found", r.str("code"))
	})

	t.Run("authentication and authorization", func(t *testing.T) {
		r := c.must(401, "GET", "/api/me", "", nil)
		assert.Equal(t, "missing_token", r.str("code"))
		assert.NotEmpty(t, r.str("request_id"))
		c.must(401, "GET", "/api/me", "forged", nil)
		r = c.must(403, "GET", "/api/admin/users", "ada", nil)
		assert.Equal(t, "admin_required", r.str("code"))
		c.must(401, "POST", "/api/device/heartbeat", "fdr_bogus", map[string]any{})
	})

	me := c.must(200, "GET", "/api/me", "admin", nil)
	assert.Equal(t, true, me.Body["is_admin"])
	ada := c.must(200, "GET", "/api/me", "ada", nil)
	assert.Equal(t, "Ada Lovelace", ada.str("display_name"))
	assert.Equal(t, false, ada.Body["is_admin"])
	adaID := ada.str("id")

	t.Run("contract validation", func(t *testing.T) {
		r := c.must(400, "POST", "/api/admin/zones", "admin", map[string]any{})
		assert.Equal(t, "validation_failed", r.str("code"))
		assert.NotEmpty(t, r.Body["errors"])
		c.must(400, "POST", "/api/admin/zones", "admin", map[string]any{"name": "x", "bogus": 1})
		c.must(400, "GET", "/api/admin/users?limit=999", "admin", nil)
		c.must(400, "GET", "/api/admin/users/not-a-uuid", "admin", nil)
	})

	// --- access control setup
	zone := c.must(201, "POST", "/api/admin/zones", "admin", map[string]any{"name": "Lab", "description": "Embedded lab"})
	zoneID := zone.str("id")
	c.must(409, "POST", "/api/admin/zones", "admin", map[string]any{"name": "Lab"})

	group := c.must(201, "POST", "/api/admin/access-groups", "admin", map[string]any{"name": "Members"})
	groupID := group.str("id")
	c.must(204, "PUT", "/api/admin/access-groups/"+groupID+"/members/"+adaID, "admin", nil)
	c.must(204, "PUT", "/api/admin/access-groups/"+groupID+"/members/"+adaID, "admin", nil) // idempotent
	members := c.must(200, "GET", "/api/admin/access-groups/"+groupID+"/members", "admin", nil)
	require.Len(t, members.items(), 1)
	g := c.must(200, "GET", "/api/admin/access-groups/"+groupID, "admin", nil)
	assert.EqualValues(t, 1, g.Body["member_count"])

	c.must(201, "POST", "/api/admin/access-grants", "admin", map[string]any{"zone_id": zoneID, "group_id": groupID})
	r := c.must(400, "POST", "/api/admin/access-grants", "admin", map[string]any{"zone_id": zoneID})
	assert.Equal(t, "validation_failed", r.str("code"))

	rd := c.must(201, "POST", "/api/admin/readers", "admin", map[string]any{
		"name": "Lab door", "serial": "RDR-0001", "zone_id": zoneID,
	})
	readerKey := rd.str("api_key")
	readerID := rd.Body["reader"].(map[string]any)["id"].(string)
	require.NotEmpty(t, readerKey)
	c.must(409, "POST", "/api/admin/readers", "admin", map[string]any{"name": "dup", "serial": "RDR-0001"})

	hb := c.must(200, "POST", "/api/device/heartbeat", readerKey, map[string]any{"firmware_version": "0.1.0"})
	assert.Equal(t, "fieldday/readers/"+readerID+"/commands", hb.str("mqtt_topic"))
	assert.Equal(t, "F04649454C4444", hb.str("hce_aid"))

	cred := c.must(201, "POST", "/api/admin/credentials", "admin", map[string]any{
		"kind": "desfire", "uid": "04a1b2c3d4e5f6", "user_id": adaID, "label": "Ada's card",
	})
	assert.Equal(t, "04A1B2C3D4E5F6", cred.str("uid"), "uid is normalised")
	credID := cred.str("id")
	c.must(409, "POST", "/api/admin/credentials", "admin", map[string]any{"kind": "desfire", "uid": "04A1B2C3D4E5F6"})

	tap := func(body map[string]any) resp { return c.must(200, "POST", "/api/device/taps", readerKey, body) }

	t.Run("tap granted and idempotent", func(t *testing.T) {
		eventID := uuid.NewString()
		r := tap(map[string]any{"uid": "04A1B2C3D4E5F6", "kind": "desfire", "event_id": eventID})
		assert.Equal(t, "granted", r.str("decision"))
		assert.Equal(t, "Ada Lovelace", r.str("display_name"))
		assert.EqualValues(t, 3000, r.Body["unlock_ms"])

		again := tap(map[string]any{"uid": "04A1B2C3D4E5F6", "kind": "desfire", "event_id": eventID})
		assert.Equal(t, r.str("event_id"), again.str("event_id"))
		assert.Equal(t, "granted", again.str("decision"))

		events := c.must(200, "GET", "/api/me/access-events", "ada", nil)
		assert.Len(t, events.items(), 1, "a retried tap is recorded once")
	})

	t.Run("tap denials", func(t *testing.T) {
		r := tap(map[string]any{"uid": "DEADBEEF", "kind": "desfire"})
		assert.Equal(t, "denied", r.str("decision"))
		assert.Equal(t, "unknown_credential", r.str("reason"))
		assert.EqualValues(t, 0, r.Body["unlock_ms"])
		assert.Nil(t, r.Body["display_name"])

		r = tap(map[string]any{"uid": "04A1B2C3D4E5F6", "kind": "ntag"})
		assert.Equal(t, "unknown_credential", r.str("reason"), "kind must match")

		c.must(400, "POST", "/api/device/taps", readerKey, map[string]any{"uid": "XYZ", "kind": "desfire"})
	})

	t.Run("secure zone requires crypto auth", func(t *testing.T) {
		c.must(200, "PUT", "/api/admin/zones/"+zoneID, "admin", map[string]any{
			"name": "Lab", "require_secure_credential": true,
		})
		r := tap(map[string]any{"uid": "04A1B2C3D4E5F6", "kind": "desfire"})
		assert.Equal(t, "insecure_credential", r.str("reason"))
		r = tap(map[string]any{"uid": "04A1B2C3D4E5F6", "kind": "desfire", "secure_auth": true})
		assert.Equal(t, "granted", r.str("decision"))
		c.must(200, "PUT", "/api/admin/zones/"+zoneID, "admin", map[string]any{"name": "Lab"})
	})

	t.Run("schedule", func(t *testing.T) {
		z := c.must(201, "POST", "/api/admin/zones", "admin", map[string]any{"name": "Office"})
		other := []int{}
		for d := 1; d <= 7; d++ {
			if d != isoToday(t) {
				other = append(other, d)
			}
		}
		c.must(201, "POST", "/api/admin/access-grants", "admin", map[string]any{
			"zone_id": z.str("id"), "user_id": adaID, "weekdays": other,
		})
		rd := c.must(201, "POST", "/api/admin/readers", "admin", map[string]any{
			"name": "Office door", "serial": "RDR-0002", "zone_id": z.str("id"),
		})
		r := c.must(200, "POST", "/api/device/taps", rd.str("api_key"), map[string]any{"uid": "04A1B2C3D4E5F6", "kind": "desfire"})
		assert.Equal(t, "outside_schedule", r.str("reason"))

		c.must(400, "POST", "/api/admin/access-grants", "admin", map[string]any{
			"zone_id": z.str("id"), "user_id": adaID, "start_time": "08:00",
		})
	})

	t.Run("tap to enroll", func(t *testing.T) {
		e := c.must(201, "POST", "/api/admin/readers/"+readerID+"/enrollment", "admin", map[string]any{
			"user_id": adaID, "label": "spare",
		})
		assert.Equal(t, adaID, e.str("user_id"))

		r := tap(map[string]any{"uid": "04A1B2C3D4E5F6", "kind": "desfire"})
		assert.Equal(t, "already_registered", r.str("reason"), "a known card does not consume the enrollment")

		r = tap(map[string]any{"uid": "0499887766", "kind": "desfire"})
		assert.Equal(t, "enrolled", r.str("decision"))

		r = tap(map[string]any{"uid": "0499887766", "kind": "desfire"})
		assert.Equal(t, "granted", r.str("decision"), "enrolled card works for its owner")

		c.must(404, "DELETE", "/api/admin/readers/"+readerID+"/enrollment", "admin", nil)
	})

	t.Run("mobile HCE credential", func(t *testing.T) {
		issued := c.must(201, "POST", "/api/me/mobile-credentials", "ada", map[string]any{"label": "Pixel"})
		key, err := base64.StdEncoding.DecodeString(issued.str("secret"))
		require.NoError(t, err)
		require.Len(t, key, 32)
		credUID := issued.Body["credential"].(map[string]any)["uid"].(string)

		challenge := secret.Random(16)
		rid := uuid.MustParse(readerID)
		good := hex.EncodeToString(secret.MAC(key, challenge, rid[:]))
		r := tap(map[string]any{"uid": credUID, "kind": "mobile_hce",
			"mobile": map[string]any{"challenge": hex.EncodeToString(challenge), "response": good}})
		assert.Equal(t, "granted", r.str("decision"))

		bad := hex.EncodeToString(secret.MAC(secret.Random(32), challenge, rid[:]))
		r = tap(map[string]any{"uid": credUID, "kind": "mobile_hce",
			"mobile": map[string]any{"challenge": hex.EncodeToString(challenge), "response": bad}})
		assert.Equal(t, "mobile_verification_failed", r.str("reason"))

		r = tap(map[string]any{"uid": credUID, "kind": "mobile_hce"})
		assert.Equal(t, "mobile_verification_failed", r.str("reason"), "proof is mandatory")

		mine := c.must(200, "GET", "/api/me/credentials", "ada", nil)
		assert.Len(t, mine.items(), 3)
	})

	t.Run("report lost and revoke", func(t *testing.T) {
		c.must(404, "POST", "/api/me/credentials/"+credID+"/report-lost", "grace", nil)
		lost := c.must(200, "POST", "/api/me/credentials/"+credID+"/report-lost", "ada", nil)
		assert.Equal(t, "lost", lost.str("status"))
		r := tap(map[string]any{"uid": "04A1B2C3D4E5F6", "kind": "desfire"})
		assert.Equal(t, "credential_inactive", r.str("reason"))

		c.must(200, "PUT", "/api/admin/credentials/"+credID, "admin", map[string]any{
			"user_id": adaID, "label": "Ada's card", "status": "revoked",
		})
		c.must(409, "PUT", "/api/admin/credentials/"+credID, "admin", map[string]any{
			"user_id": adaID, "label": "Ada's card", "status": "active",
		})
		// A revoked UID can be issued again.
		c.must(201, "POST", "/api/admin/credentials", "admin", map[string]any{"kind": "desfire", "uid": "04A1B2C3D4E5F6"})
	})

	t.Run("suspended user", func(t *testing.T) {
		grace := c.must(200, "GET", "/api/me", "grace", nil)
		c.must(200, "PUT", "/api/admin/users/"+grace.str("id"), "admin", map[string]any{
			"email": "grace@example.com", "display_name": "Grace Hopper", "status": "suspended",
		})
		r := c.must(403, "GET", "/api/me", "grace", nil)
		assert.Equal(t, "user_suspended", r.str("code"))
	})

	t.Run("pre-registered user is linked on first login", func(t *testing.T) {
		u := c.must(201, "POST", "/api/admin/users", "admin", map[string]any{"email": "linus@example.com", "display_name": "Linus"})
		c.must(409, "POST", "/api/admin/users", "admin", map[string]any{"email": "LINUS@example.com", "display_name": "dup"})
		tokens["linus"] = auth.Claims{Subject: "sub-linus", Email: "linus@example.com", EmailVerified: true, Name: "Linus T"}
		me := c.must(200, "GET", "/api/me", "linus", nil)
		assert.Equal(t, u.str("id"), me.str("id"))
		assert.Equal(t, "Linus", me.str("display_name"), "admin-chosen name is kept")
	})

	t.Run("wallet", func(t *testing.T) {
		w := c.must(201, "POST", "/api/admin/readers", "admin", map[string]any{
			"name": "Cafeteria", "serial": "POS-0001", "mode": "wallet",
		})
		posKey := w.str("api_key")
		card := c.must(201, "POST", "/api/admin/credentials", "admin", map[string]any{
			"kind": "desfire", "uid": "0411223344", "user_id": adaID,
		})
		_ = card

		path := "/api/admin/users/" + adaID + "/wallet/transactions"
		c.must(201, "POST", path, "admin", map[string]any{"kind": "topup", "amount_minor": 1000, "idempotency_key": "topup-0001"})
		c.must(201, "POST", path, "admin", map[string]any{"kind": "topup", "amount_minor": 1000, "idempotency_key": "topup-0001"})
		c.must(409, "POST", path, "admin", map[string]any{"kind": "topup", "amount_minor": 5, "idempotency_key": "topup-0001"})

		charge := func(amount int, key string) resp {
			return c.must(200, "POST", "/api/device/wallet/charges", posKey, map[string]any{
				"uid": "0411223344", "kind": "desfire", "amount_minor": amount, "idempotency_key": key,
			})
		}
		ch := charge(300, "charge-0001")
		assert.Equal(t, true, ch.Body["approved"])
		assert.EqualValues(t, 700, ch.Body["balance_minor"])
		again := charge(300, "charge-0001")
		assert.Equal(t, ch.str("transaction_id"), again.str("transaction_id"), "charge is idempotent")

		poor := charge(5000, "charge-0002")
		assert.Equal(t, false, poor.Body["approved"])
		assert.Equal(t, "insufficient_funds", poor.str("reason"))

		r := c.must(200, "POST", "/api/device/wallet/charges", readerKey, map[string]any{
			"uid": "0411223344", "kind": "desfire", "amount_minor": 1, "idempotency_key": "charge-0003",
		})
		assert.Equal(t, "reader_wrong_mode", r.str("reason"))

		c.must(201, "POST", path, "admin", map[string]any{
			"kind": "refund", "amount_minor": 100, "refund_of": ch.str("transaction_id"), "idempotency_key": "refund-0001",
		})
		r = c.must(422, "POST", path, "admin", map[string]any{
			"kind": "refund", "amount_minor": 300, "refund_of": ch.str("transaction_id"), "idempotency_key": "refund-0002",
		})
		assert.Equal(t, "refund_exceeds_charge", r.str("code"))
		c.must(422, "POST", path, "admin", map[string]any{"kind": "adjustment", "amount_minor": -5000, "idempotency_key": "adjust-0001"})

		mine := c.must(200, "GET", "/api/me/wallet", "ada", nil)
		assert.EqualValues(t, 800, mine.Body["balance_minor"])
		txs := c.must(200, "GET", "/api/me/wallet/transactions?limit=2", "ada", nil)
		assert.Len(t, txs.items(), 2)
		require.NotNil(t, txs.Body["next_cursor"])
		rest := c.must(200, "GET", fmt.Sprintf("/api/me/wallet/transactions?cursor=%s", txs.str("next_cursor")), "ada", nil)
		assert.Len(t, rest.items(), 1)
		assert.Nil(t, rest.Body["next_cursor"])
	})

	t.Run("reader commands", func(t *testing.T) {
		cmd := c.must(201, "POST", "/api/admin/readers/"+readerID+"/commands", "admin", map[string]any{
			"type": "write_card", "payload": map[string]any{"aid": "F00001", "file_no": 1, "offset": 0, "data_hex": "CAFE"},
		})
		cmdID := cmd.str("id")
		open := c.must(200, "GET", "/api/device/commands", readerKey, nil)
		require.Len(t, open.items(), 1)
		assert.Equal(t, "CAFE", open.items()[0].(map[string]any)["payload"].(map[string]any)["data_hex"])

		done := c.must(200, "POST", "/api/device/commands/"+cmdID+"/result", readerKey, map[string]any{"status": "succeeded"})
		assert.Equal(t, "succeeded", done.str("status"))
		c.must(409, "POST", "/api/device/commands/"+cmdID+"/result", readerKey, map[string]any{"status": "failed"})
		c.must(404, "POST", "/api/device/commands/"+uuid.NewString()+"/result", readerKey, map[string]any{"status": "failed"})

		hist := c.must(200, "GET", "/api/admin/readers/"+readerID+"/commands", "admin", nil)
		assert.Len(t, hist.items(), 1)
	})

	t.Run("disabled reader and key rotation", func(t *testing.T) {
		rot := c.must(200, "POST", "/api/admin/readers/"+readerID+"/rotate-key", "admin", nil)
		c.must(401, "POST", "/api/device/heartbeat", readerKey, map[string]any{})
		newKey := rot.str("api_key")
		c.must(200, "POST", "/api/device/heartbeat", newKey, map[string]any{})

		c.must(200, "PUT", "/api/admin/readers/"+readerID, "admin", map[string]any{
			"name": "Lab door", "zone_id": zoneID, "mode": "access", "status": "disabled",
		})
		r := c.must(403, "POST", "/api/device/heartbeat", newKey, map[string]any{})
		assert.Equal(t, "reader_disabled", r.str("code"))
	})

	t.Run("audit log", func(t *testing.T) {
		logs := c.must(200, "GET", "/api/admin/audit-logs?limit=200", "admin", nil)
		assert.NotEmpty(t, logs.items())
		methods := []string{}
		for _, it := range logs.items() {
			methods = append(methods, it.(map[string]any)["method"].(string))
		}
		assert.False(t, slices.Contains(methods, "GET"), "reads are not audited")
	})

	t.Run("admin event log filters", func(t *testing.T) {
		ev := c.must(200, "GET", "/api/admin/access-events?decision=enrolled", "admin", nil)
		assert.Len(t, ev.items(), 1)
		ev = c.must(200, "GET", "/api/admin/access-events?reader_id="+readerID+"&limit=200", "admin", nil)
		assert.Greater(t, len(ev.items()), 5)
	})
}
