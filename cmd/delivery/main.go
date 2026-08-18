package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/tyagiquamar/relaydb/internal/config"
	"github.com/tyagiquamar/relaydb/internal/persistence"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
	"github.com/tyagiquamar/relaydb/internal/webhook"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg := config.MustLoad()
	logger := telemetry.Logger()

	logger.Info("starting relaydb delivery",
		"timeout", cfg.WebhookTimeout,
		"max_attempts", cfg.WebhookMaxAttempts,
	)

	telemetry.SetBuildInfo("0.1.0", "delivery")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := persistence.NewPool(ctx, persistence.DefaultConfig(cfg.MetadataDBURL))
	if err != nil {
		return fmt.Errorf("connect metadata db: %w", err)
	}
	defer pool.Close()

	if err := persistence.NewMigrator(pool).Migrate(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	svc, err := webhook.NewService(pool, cfg.MasterKey)
	if err != nil {
		return err
	}

	svcErr := make(chan error, 1)
	go func() {
		svcErr <- svc.Run(ctx)
	}()

	live, ready := telemetry.HealthHandler(func() bool { return true })

	mux := http.NewServeMux()
	mux.Handle("/health/live", live)
	mux.Handle("/health/ready", ready)
	mux.Handle("/metrics", telemetry.MetricsHandler())

	httpErr := make(chan error, 1)
	go func() {
		logger.Info("metrics listening", "addr", cfg.MetricsAddr)
		httpErr <- http.ListenAndServe(cfg.MetricsAddr, mux)
	}()

	select {
	case err := <-svcErr:
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case err := <-httpErr:
		return fmt.Errorf("health server: %w", err)
	}
}
