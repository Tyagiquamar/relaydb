package main

import (
	"context"
	"log"
	"net/http"

	"github.com/tyagiquamar/relaydb/internal/config"
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

	// Health endpoints
	live, ready := telemetry.HealthHandler(func() bool {
		// TODO: check metadata DB connectivity
		return true
	})

	mux := http.NewServeMux()
	mux.Handle("/health/live", live)
	mux.Handle("/health/ready", ready)
	mux.Handle("/metrics", telemetry.MetricsHandler())

	logger.Info("api listening", "addr", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, mux); err != nil {
		log.Fatal(err)
	}
}

var _ = context.Background // for future use