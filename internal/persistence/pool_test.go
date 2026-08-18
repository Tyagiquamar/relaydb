package persistence

import (
	"context"
	"testing"
	"time"
)

// TestPoolConfig tests pool configuration validation.
func TestPoolConfig(t *testing.T) {
	cfg := DefaultConfig("postgres://localhost/relaydb")
	
	if cfg.MinConns != 2 {
		t.Errorf("MinConns = %d, want 2", cfg.MinConns)
	}
	if cfg.MaxConns != 10 {
		t.Errorf("MaxConns = %d, want 10", cfg.MaxConns)
	}
	if cfg.MaxConnLifetime != time.Hour {
		t.Errorf("MaxConnLifetime = %v, want 1h", cfg.MaxConnLifetime)
	}
}

// TestPoolRequiresValidURL tests that invalid URLs fail.
func TestPoolRequiresValidURL(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultConfig("invalid-url")
	
	_, err := NewPool(ctx, cfg)
	if err == nil {
		t.Fatal("NewPool should fail with invalid URL")
	}
}