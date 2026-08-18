package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/tyagiquamar/relaydb/internal/capture"
	"github.com/tyagiquamar/relaydb/internal/config"
	"github.com/tyagiquamar/relaydb/internal/crypto"
	"github.com/tyagiquamar/relaydb/internal/persistence"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg := config.MustLoad()
	logger := telemetry.Logger()

	logger.Info("starting relaydb capture",
		"source", cfg.SourceDBURL,
		"source_name", cfg.SourceName,
		"slot", cfg.ReplicationSlot,
		"owner", cfg.CaptureOwnerID,
	)

	telemetry.SetBuildInfo("0.1.0", "capture")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Metadata pool + migrations
	pool, err := persistence.NewPool(ctx, persistence.DefaultConfig(cfg.MetadataDBURL))
	if err != nil {
		return fmt.Errorf("connect metadata db: %w", err)
	}
	defer pool.Close()

	if err := persistence.NewMigrator(pool).Migrate(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	// Resolve (or auto-register) the source row this instance captures.
	sourceID, err := ensureSource(ctx, cfg, pool)
	if err != nil {
		return fmt.Errorf("resolve source %q: %w", cfg.SourceName, err)
	}
	logger.Info("resolved source", "source_name", cfg.SourceName, "source_id", sourceID)

	// Capture service
	svc := capture.NewService(cfg, pool)

	svcErr := make(chan error, 1)
	go func() {
		svcErr <- svc.Run(ctx, sourceID)
	}()

	// Health + metrics endpoints
	live, ready := telemetry.HealthHandler(func() bool {
		select {
		case err := <-svcErr:
			// Run returned; put the error back and report based on it.
			svcErr <- err
			return err == nil || errors.Is(err, context.Canceled)
		default:
			return true
		}
	})

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

// ensureSource looks up the source row by name. If it does not exist yet
// (fresh demo deploy), it registers it using the capture service's own
// connection settings, encrypting the connection string at rest (KTD-2).
func ensureSource(ctx context.Context, cfg config.Config, pool *persistence.Pool) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `SELECT id FROM sources WHERE name = $1`, cfg.SourceName).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	env, err := crypto.NewEnvelope(cfg.MasterKey)
	if err != nil {
		return "", fmt.Errorf("init crypto envelope: %w", err)
	}
	aad := crypto.ComputeAAD(cfg.SourceName, "source-credential")
	blob, err := env.Encrypt([]byte(cfg.SourceDBURL), aad)
	if err != nil {
		return "", fmt.Errorf("encrypt credentials: %w", err)
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO sources (name, description, credential_blob, replication_slot, publication, status)
		VALUES ($1, $2, $3, $4, $5, 'registered')
		ON CONFLICT (name) DO UPDATE SET updated_at = now()
		RETURNING id
	`, cfg.SourceName, "auto-registered by capture service", blob,
		cfg.ReplicationSlot, cfg.Publication).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("register source: %w", err)
	}
	return id, nil
}
