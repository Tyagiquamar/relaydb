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

	logger.Info("starting relaydb delivery",
		"timeout", cfg.WebhookTimeout,
		"max_attempts", cfg.WebhookMaxAttempts,
	)

	telemetry.SetBuildInfo("0.1.0", "delivery")

	// Health endpoints
	live, ready := telemetry.HealthHandler(func() bool {
		return true
	})

	mux := http.NewServeMux()
	mux.Handle("/health/live", live)
	mux.Handle("/health/ready", ready)
	mux.Handle("/metrics", telemetry.MetricsHandler())

	// TODO: start delivery service
	logger.Info("delivery placeholder - webhook delivery not yet implemented")
	logger.Info("metrics listening", "addr", cfg.MetricsAddr)
	if err := http.ListenAndServe(cfg.MetricsAddr, mux); err != nil {
		log.Fatal(err)
	}
}

var _ = context.Background // for future use