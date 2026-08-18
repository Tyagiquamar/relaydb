package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Clear env to test defaults
	os.Clearenv()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":8080")
	}
	if cfg.MetadataDBURL == "" {
		t.Error("MetadataDBURL should have default")
	}
	if cfg.LeaseDuration != 30*time.Second {
		t.Errorf("LeaseDuration = %v, want %v", cfg.LeaseDuration, 30*time.Second)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("RELAYDB_HTTP_ADDR", ":9090")
	t.Setenv("RELAYDB_LEASE_DURATION", "60s")
	t.Setenv("RELAYDB_METADATA_DB_URL", "postgres://test:test@localhost:5432/test")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":9090")
	}
	if cfg.LeaseDuration != 60*time.Second {
		t.Errorf("LeaseDuration = %v, want %v", cfg.LeaseDuration, 60*time.Second)
	}
}

func TestValidateCaptureRequiresSource(t *testing.T) {
	os.Clearenv()
	t.Setenv("RELAYDB_SERVICE", "capture")
	t.Setenv("RELAYDB_METADATA_DB_URL", "postgres://localhost/relaydb")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail without SOURCE_DB_URL for capture")
	}
}

func TestValidateLeaseDuration(t *testing.T) {
	os.Clearenv()
	t.Setenv("RELAYDB_METADATA_DB_URL", "postgres://localhost/relaydb")
	t.Setenv("RELAYDB_LEASE_DURATION", "10s")
	t.Setenv("RELAYDB_HEARTBEAT_INTERVAL", "5s")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail when lease < 3x heartbeat")
	}
}

func TestEnvIntInvalid(t *testing.T) {
	os.Clearenv()
	t.Setenv("RELAYDB_METADATA_DB_URL", "postgres://localhost/relaydb")
	t.Setenv("RELAYDB_MAX_POLL_BATCH", "-1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// Invalid values fall back to default
	if cfg.MaxPollBatchSize != 100 {
		t.Errorf("MaxPollBatchSize = %d, want default 100", cfg.MaxPollBatchSize)
	}
}