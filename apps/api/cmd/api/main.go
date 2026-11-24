// Command api runs the Field Day backend.
//
//	api                 serve HTTP + MQTT (default)
//	api migrate up      apply all pending migrations
//	api migrate down N  roll back N migrations
//	api migrate version print the current schema version
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	_ "time/tzdata" // distroless images have no zoneinfo

	"github.com/golang-migrate/migrate/v4"

	"github.com/Gestusition/field-day/apps/api/internal/app"
	"github.com/Gestusition/field-day/apps/api/internal/platform/config"
	"github.com/Gestusition/field-day/apps/api/internal/platform/database"
	"github.com/Gestusition/field-day/apps/api/internal/platform/logging"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New(os.Stdout, cfg.LogFormat, cfg.LogLevel)
	slog.SetDefault(log)
	log.Info("field-day api", "version", version, "env", cfg.Env)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(args) > 0 && args[0] == "migrate" {
		return runMigrate(ctx, cfg, log, args[1:])
	}
	if len(args) > 0 && args[0] != "serve" {
		return fmt.Errorf("unknown command %q (want serve or migrate)", args[0])
	}

	a, err := app.New(ctx, cfg, log, app.Options{})
	if err != nil {
		return err
	}
	defer a.Close()
	return a.Run(ctx)
}

func runMigrate(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	pool, err := database.Connect(ctx, cfg.DatabaseURL, 2)
	if err != nil {
		return err
	}
	defer pool.Close()

	if len(args) == 0 {
		return errors.New("usage: migrate up | down N | version")
	}
	switch args[0] {
	case "up":
		return database.MigrateUp(pool, log)
	case "down":
		if len(args) != 2 {
			return errors.New("usage: migrate down N")
		}
		n, err := strconv.Atoi(args[1])
		if err != nil || n < 1 {
			return errors.New("migrate down: N must be a positive integer")
		}
		m, err := database.Migrator(pool)
		if err != nil {
			return err
		}
		defer func() { _, _ = m.Close() }()
		if err := m.Steps(-n); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return err
		}
		log.Info("rolled back", "steps", n)
		return nil
	case "version":
		m, err := database.Migrator(pool)
		if err != nil {
			return err
		}
		defer func() { _, _ = m.Close() }()
		v, dirty, err := m.Version()
		if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
			return err
		}
		fmt.Printf("version=%d dirty=%t\n", v, dirty)
		return nil
	}
	return fmt.Errorf("unknown migrate command %q", args[0])
}
