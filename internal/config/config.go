package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all RelayDB configuration.
type Config struct {
	// Service identification
	Env     string // development, staging, production
	Service string // api, capture, delivery

	// HTTP/gRPC servers
	HTTPAddr string
	GRPCAddr string

	// Metadata database (stores events, checkpoints, offsets)
	MetadataDBURL string

	// Source database (where we capture from) — only for capture service
	SourceDBURL string
	SourceName  string // name of the sources row this capture instance owns

	// Replication settings
	ReplicationSlot    string
	Publication        string
	StandbyMessageTimeout time.Duration

	// Capture ownership lease
	CaptureOwnerID     string
	LeaseDuration      time.Duration
	HeartbeatInterval  time.Duration

	// Consumer settings
	MaxPollBatchSize int
	MaxPollWait      time.Duration

	// Webhook delivery
	WebhookTimeout     time.Duration
	WebhookMaxAttempts int

	// Observability
	MetricsAddr string
	OTLPEndpoint string // empty disables OTLP

	// Crypto
	MasterKey string // base64-encoded 256-bit key for envelope encryption

	// Limits
	MaxTransactionBufferBytes int64
	MaxEventBatchSize         int
	MaxInflightTransactions   int

	// API keys (hashed storage)
	AdminAPIKeyID  string
	AdminAPIKey    string
	ReaderAPIKeyID string
	ReaderAPIKey   string
}

// Load reads configuration from environment with defaults.
func Load() (Config, error) {
	cfg := Config{
		Env:     env("RELAYDB_ENV", "development"),
		Service: env("RELAYDB_SERVICE", "api"),

		HTTPAddr: env("RELAYDB_HTTP_ADDR", ":"+env("PORT", "8080")),
		GRPCAddr: env("RELAYDB_GRPC_ADDR", ":9090"),

		MetadataDBURL: env("RELAYDB_METADATA_DB_URL", "postgres://relaydb:relaydb@localhost:5433/relaydb?sslmode=disable"),
		SourceDBURL:   env("RELAYDB_SOURCE_DB_URL", ""),
		SourceName:    env("RELAYDB_SOURCE_NAME", "demo"),

		ReplicationSlot:       env("RELAYDB_REPLICATION_SLOT", "relaydb_slot"),
		Publication:           env("RELAYDB_PUBLICATION", "relaydb_pub"),
		StandbyMessageTimeout: envDuration("RELAYDB_STANDBY_TIMEOUT", 10*time.Second),

		CaptureOwnerID:    env("RELAYDB_CAPTURE_OWNER_ID", hostname()),
		LeaseDuration:     envDuration("RELAYDB_LEASE_DURATION", 30*time.Second),
		HeartbeatInterval: envDuration("RELAYDB_HEARTBEAT_INTERVAL", 10*time.Second),

		MaxPollBatchSize: envInt("RELAYDB_MAX_POLL_BATCH", 100),
		MaxPollWait:      envDuration("RELAYDB_MAX_POLL_WAIT", 5*time.Second),

		WebhookTimeout:     envDuration("RELAYDB_WEBHOOK_TIMEOUT", 10*time.Second),
		WebhookMaxAttempts: envInt("RELAYDB_WEBHOOK_MAX_ATTEMPTS", 5),

		MetricsAddr:  env("RELAYDB_METRICS_ADDR", ":2112"),
		OTLPEndpoint: env("OTEL_EXPORTER_OTLP_ENDPOINT", ""),

		MasterKey: env("RELAYDB_MASTER_KEY", ""),

		MaxTransactionBufferBytes: envInt64("RELAYDB_MAX_TX_BUFFER_BYTES", 256*1024*1024), // 256MB
		MaxEventBatchSize:         envInt("RELAYDB_MAX_EVENT_BATCH", 10000),
		MaxInflightTransactions:   envInt("RELAYDB_MAX_INFLIGHT_TX", 10),

		AdminAPIKeyID:  env("RELAYDB_ADMIN_KEY_ID", ""),
		AdminAPIKey:    env("RELAYDB_ADMIN_KEY", ""),
		ReaderAPIKeyID: env("RELAYDB_READER_KEY_ID", ""),
		ReaderAPIKey:   env("RELAYDB_READER_KEY", ""),
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// Validate checks configuration consistency.
func (c Config) Validate() error {
	if c.MetadataDBURL == "" {
		return fmt.Errorf("RELAYDB_METADATA_DB_URL is required")
	}
	if c.Service == "capture" {
		if c.SourceDBURL == "" {
			return fmt.Errorf("RELAYDB_SOURCE_DB_URL is required for capture service")
		}
		if c.MasterKey == "" {
			return fmt.Errorf("RELAYDB_MASTER_KEY is required for capture service")
		}
	}
	if c.LeaseDuration < c.HeartbeatInterval*3 {
		return fmt.Errorf("lease duration must be at least 3x heartbeat interval")
	}
	return nil
}

// MustLoad loads config or panics.
func MustLoad() Config {
	cfg, err := Load()
	if err != nil {
		panic(err)
	}
	return cfg
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return fallback
}

func hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "unknown"
}