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

	logger.Info("starting relaydb capture",
		"source", cfg.SourceDBURL,
		"slot", cfg.ReplicationSlot,
		"owner", cfg.CaptureOwnerID,
	)

	telemetry.SetBuildInfo("0.1.0", "capture")

	// Health endpoints
	live, ready := telemetry.HealthHandler(func() bool {
		// TODO: check replication connection
		return true
	})

	mux := http.NewServeMux()
	mux.Handle("/health/live", live)
	mux.Handle("/health/ready", ready)
	mux.Handle("/metrics", telemetry.MetricsHandler())

	// TODO: start capture service
	logger.Info("capture placeholder - replication not yet implemented")
	logger.Info("metrics listening", "addr", cfg.MetricsAddr)
	if err := http.ListenAndServe(cfg.MetricsAddr, mux); err != nil {
		log.Fatal(err)
	}
}

var _ = context.Background // for future use