//go:build integration

package failure

import (
	"context"
	"testing"
	"time"
)

// TestCheckpointWindows tests all crash windows systematically.
func TestCheckpointWindows(t *testing.T) {
	windows := []struct {
		name        string
		crashPoint  string
		expectLoss  bool
		expectDupe  bool
	}{
		{"before_persist", "before_commit", false, false},
		{"after_persist_before_ack", "after_commit", false, false},
		{"mid_transaction", "mid_stream", false, false},
	}

	for _, w := range windows {
		t.Run(w.name, func(t *testing.T) {
			ctx := context.Background()
			harness := NewTestHarness(t, ctx)
			defer harness.Cleanup(ctx)

			// TODO: Configure crash point
			// TODO: Write transaction
			// TODO: Trigger crash
			// TODO: Restart and verify

			t.Logf("Window %s: placeholder", w.name)
		})
	}
}

// TestLargeTransactionCrash tests 100k-row transaction recovery.
func TestLargeTransactionCrash(t *testing.T) {
	ctx := context.Background()
	harness := NewTestHarness(t, ctx)
	defer harness.Cleanup(ctx)

	// TODO: Start capture
	// TODO: Begin 100k-row transaction
	// TODO: Crash at 50k
	// TODO: Restart
	// TODO: Verify full transaction replayed once

	t.Log("Large transaction crash test placeholder")
}

// TestKeepaliveStarvation tests WAL sender timeout handling.
func TestKeepaliveStarvation(t *testing.T) {
	ctx := context.Background()
	harness := NewTestHarness(t, ctx)
	defer harness.Cleanup(ctx)

	// TODO: Start capture
	// TODO: Block metadata writes (simulate slow DB)
	// TODO: Verify keepalives still answered
	// TODO: Verify no wal_sender_timeout disconnect

	t.Log("Keepalive starvation test placeholder")
}