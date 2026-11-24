// Package app wires the modules together. It is the composition root: the
// only package that knows about every module.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Gestusition/field-day/apps/api/internal/access"
	"github.com/Gestusition/field-day/apps/api/internal/apigen"
	"github.com/Gestusition/field-day/apps/api/internal/audit"
	"github.com/Gestusition/field-day/apps/api/internal/credential"
	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
	"github.com/Gestusition/field-day/apps/api/internal/platform/config"
	"github.com/Gestusition/field-day/apps/api/internal/platform/database"
	"github.com/Gestusition/field-day/apps/api/internal/platform/httpx"
	"github.com/Gestusition/field-day/apps/api/internal/platform/mqtt"
	"github.com/Gestusition/field-day/apps/api/internal/platform/secret"
	"github.com/Gestusition/field-day/apps/api/internal/reader"
	"github.com/Gestusition/field-day/apps/api/internal/user"
	"github.com/Gestusition/field-day/apps/api/internal/wallet"
)

// BasePath is where the contract is mounted (servers[0].url in openapi.yaml).
const BasePath = "/api"

// Aliases give each embedded handler a distinct field name.
type (
	userHandler       = user.Handler
	credentialHandler = credential.Handler
	readerHandler     = reader.Handler
	accessHandler     = access.Handler
	walletHandler     = wallet.Handler
	auditHandler      = audit.Handler
)

// server composes the module handlers into one apigen.StrictServerInterface.
// Each module implements a disjoint set of operations; the compiler checks
// that together they cover the whole contract.
type server struct {
	*userHandler
	*credentialHandler
	*readerHandler
	*accessHandler
	*walletHandler
	*auditHandler
}

var _ apigen.StrictServerInterface = (*server)(nil)

// App is a fully wired API instance.
type App struct {
	cfg     *config.Config
	log     *slog.Logger
	pool    *pgxpool.Pool
	handler http.Handler
	readers *reader.Service
	broker  *mqtt.Broker
}

// Options lets tests replace infrastructure.
type Options struct {
	// Tokens overrides OIDC verification (tests use a local issuer).
	Tokens auth.TokenVerifier
}

// New connects to the database, applies migrations if configured and builds
// the HTTP handler and MQTT broker.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger, opts Options) (*App, error) {
	pool, err := database.Connect(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConns)
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, log: log, pool: pool}
	if err := a.build(opts); err != nil {
		pool.Close()
		return nil, err
	}
	return a, nil
}

func (a *App) build(opts Options) error {
	cfg, log := a.cfg, a.log
	if cfg.MigrateOnStart {
		if err := database.MigrateUp(a.pool, log); err != nil {
			return err
		}
	}

	sealer, err := secret.NewSealer(cfg.CredentialKey())
	if err != nil {
		return err
	}

	// Modules. Dependencies point one way: access, wallet -> credential, user.
	pub := &lazyPublisher{}
	users := user.NewService(a.pool)
	creds := credential.NewService(a.pool, sealer, cfg.HCEAID)
	a.readers = reader.NewService(a.pool, pub, log)
	accessSvc := access.NewService(a.pool, creds, users, cfg.Location(), cfg.UnlockDuration)
	walletSvc := wallet.NewService(a.pool, creds, users, cfg.WalletCurrency, log)
	auditSvc := audit.NewService(a.pool, log)

	if cfg.MQTTEnabled {
		a.broker, err = mqtt.NewBroker(mqtt.Options{
			Addr: cfg.MQTTAddr, CertFile: cfg.MQTTTLSCertFile, KeyFile: cfg.MQTTTLSKeyFile,
		}, a.readers, log)
		if err != nil {
			return err
		}
		pub.target = a.broker
	}

	tokens := opts.Tokens
	if tokens == nil {
		tokens = auth.NewOIDCVerifier(cfg.OIDCIssuer, cfg.OIDCJWKSURL, cfg.OIDCAudiences, cfg.OIDCRolesClaim)
	}
	authn := &auth.Authenticator{
		Tokens: tokens, Users: users, Devices: a.readers, AdminRole: cfg.OIDCAdminRole, Log: log,
	}

	srv := &server{
		userHandler:       user.NewHandler(users),
		credentialHandler: credential.NewHandler(creds),
		readerHandler:     reader.NewHandler(a.readers, cfg.HCEAID),
		accessHandler:     access.NewHandler(accessSvc),
		walletHandler:     wallet.NewHandler(walletSvc),
		auditHandler:      audit.NewHandler(auditSvc),
	}

	a.handler, err = a.router(srv, authn, auditSvc)
	return err
}

func (a *App) router(srv *server, authn *auth.Authenticator, auditSvc *audit.Service) (http.Handler, error) {
	doc, err := apigen.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load contract: %w", err)
	}
	specJSON, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshal contract: %w", err)
	}
	contract, err := httpx.NewContract(doc, authn, a.log)
	if err != nil {
		return nil, err
	}

	if a.cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.ContextWithFallback = true // handlers read the principal from the request context
	r.HandleMethodNotAllowed = true
	r.NoRoute(httpx.NotFound)
	r.NoMethod(httpx.MethodNotAllowed)
	r.Use(httpx.RequestIDMiddleware(), httpx.LogMiddleware(a.log), httpx.RecoverMiddleware(a.log))
	if len(a.cfg.CORSAllowedOrigins) > 0 {
		r.Use(cors.New(cors.Config{
			AllowOrigins:  a.cfg.CORSAllowedOrigins,
			AllowMethods:  []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowHeaders:  []string{"Authorization", "Content-Type", "X-Request-ID"},
			ExposeHeaders: []string{"X-Request-ID"},
			MaxAge:        12 * time.Hour,
		}))
	}

	// Operational endpoints (outside the contract, unauthenticated).
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := a.pool.Ping(ctx); err != nil {
			httpx.WriteProblem(c, http.StatusServiceUnavailable, "not_ready", "Database unavailable.", nil)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	r.GET(BasePath+"/openapi.json", httpx.SpecHandler(specJSON))
	r.GET("/docs", httpx.DocsHandler)

	// The contract. Order matters: audit wraps everything so it sees the
	// final status; the contract middleware authenticates and validates.
	api := r.Group("", auditSvc.Middleware(BasePath+"/admin/"), contract.Middleware())
	strict := apigen.NewStrictHandlerWithOptions(srv, nil, apigen.StrictGinServerOptions{
		RequestErrorHandlerFunc: func(c *gin.Context, err error) {
			httpx.WriteProblem(c, http.StatusBadRequest, "malformed_request", err.Error(), nil)
		},
		HandlerErrorFunc: func(c *gin.Context, err error) { httpx.WriteError(c, a.log, err) },
		ResponseErrorHandlerFunc: func(c *gin.Context, err error) {
			httpx.WriteError(c, a.log, fmt.Errorf("encode response: %w", err))
		},
	})
	apigen.RegisterHandlersWithOptions(api, strict, apigen.GinServerOptions{
		BaseURL: BasePath,
		ErrorHandler: func(c *gin.Context, err error, status int) {
			httpx.WriteProblem(c, status, "invalid_parameter", err.Error(), nil)
		},
	})
	return r, nil
}

// lazyPublisher lets the reader service exist before the broker (the broker
// needs the reader service to authenticate devices).
type lazyPublisher struct{ target reader.Publisher }

func (p *lazyPublisher) Publish(topic string, payload []byte) error {
	if p.target == nil {
		return nil
	}
	return p.target.Publish(topic, payload)
}

// Handler returns the HTTP handler.
func (a *App) Handler() http.Handler { return a.handler }

// Pool exposes the database pool (used by tests).
func (a *App) Pool() *pgxpool.Pool { return a.pool }

// Close releases resources.
func (a *App) Close() {
	if a.broker != nil {
		_ = a.broker.Close()
	}
	a.pool.Close()
}

// Run serves HTTP (and MQTT) until ctx is cancelled, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	httpSrv := &http.Server{
		Addr:              a.cfg.HTTPAddr,
		Handler:           a.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	if a.broker != nil {
		if err := a.broker.Start(); err != nil {
			return fmt.Errorf("mqtt: %w", err)
		}
		a.log.Info("mqtt broker listening", "addr", a.cfg.MQTTAddr, "tls", a.cfg.MQTTTLSCertFile != "")
	}

	workerCtx, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	go a.readers.RunExpiry(workerCtx)

	errCh := make(chan error, 1)
	go func() {
		a.log.Info("http server listening", "addr", a.cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	a.log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}
