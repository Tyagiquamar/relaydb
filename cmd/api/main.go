package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/tyagiquamar/relaydb/internal/api"
	"github.com/tyagiquamar/relaydb/internal/config"
	"github.com/tyagiquamar/relaydb/internal/consumer"
	relaygrpc "github.com/tyagiquamar/relaydb/internal/grpc"
	"github.com/tyagiquamar/relaydb/internal/persistence"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

func main() {
	cfg := config.MustLoad()
	logger := telemetry.Logger()

	logger.Info("starting relaydb api",
		"http_addr", cfg.HTTPAddr,
		"grpc_addr", cfg.GRPCAddr,
		"env", cfg.Env,
	)

	telemetry.SetBuildInfo("0.1.0", "api")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("shutting down")
		cancel()
	}()

	// Connect to metadata database
	pool, err := persistence.NewPool(ctx, persistence.DefaultConfig(cfg.MetadataDBURL))
	if err != nil {
		log.Fatalf("connect to metadata db: %v", err) //nolint:gocritic // CLI entrypoints intentionally exit via log.Fatal; process teardown releases resources
	}
	defer pool.Close()

	// Run migrations
	migrator := persistence.NewMigrator(pool)
	if err := migrator.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err) //nolint:gocritic // CLI entrypoints intentionally exit via log.Fatal; process teardown releases resources
	}
	logger.Info("migrations applied")

	// Create services
	consumerSvc := consumer.NewService(pool)
	apiServer := api.NewServer(cfg, pool)
	grpcServer := relaygrpc.NewServer(cfg, consumerSvc)

	// Start gRPC server
	go func() {
		if err := grpcServer.Serve(ctx, cfg.GRPCAddr); err != nil {
			logger.Error("grpc server error", "error", err)
		}
	}()

	// Start HTTP server
	logger.Info("api listening", "http", cfg.HTTPAddr, "grpc", cfg.GRPCAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, apiServer.Handler()); err != nil {
		log.Fatal(err)
	}
}
