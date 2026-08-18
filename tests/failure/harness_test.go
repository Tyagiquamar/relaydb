//go:build integration

package failure

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestHarness provides infrastructure for failure testing.
type TestHarness struct {
	Source   *postgres.PostgresContainer
	Metadata *postgres.PostgresContainer
	Capture  *CaptureController
}

// CaptureController wraps a capture service with crash hooks.
type CaptureController struct {
	CrashAfterCommit  bool
	CrashBeforeCommit bool
	CrashMidStream    bool
}

func NewTestHarness(t *testing.T, ctx context.Context) *TestHarness {
	t.Helper()

	source, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("source"),
		postgres.WithUsername("relaydb"),
		postgres.WithPassword("relaydb"),
		postgres.WithConfig(map[string]string{
			"wal_level":             "logical",
			"max_replication_slots": "10",
			"max_wal_senders":       "10",
		}),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start source: %v", err)
	}

	metadata, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("relaydb"),
		postgres.WithUsername("relaydb"),
		postgres.WithPassword("relaydb"),
	)
	if err != nil {
		t.Fatalf("start metadata: %v", err)
	}

	return &TestHarness{
		Source:   source,
		Metadata: metadata,
		Capture:  &CaptureController{},
	}
}

func (h *TestHarness) Cleanup(ctx context.Context) {
	if h.Source != nil {
		h.Source.Terminate(ctx)
	}
	if h.Metadata != nil {
		h.Metadata.Terminate(ctx)
	}
}

// TestCrashAfterCommitBeforeAck tests the critical crash window.
func TestCrashAfterCommitBeforeAck(t *testing.T) {
	ctx := context.Background()
	harness := NewTestHarness(t, ctx)
	defer harness.Cleanup(ctx)

	// TODO: Start capture with crash-after-commit hook
	// TODO: Write transaction
	// TODO: Verify crash
	// TODO: Restart capture
	// TODO: Verify no duplicates, checkpoint advances

	t.Log("Crash window test placeholder")
}

// TestOwnershipRace tests split-brain prevention.
func TestOwnershipRace(t *testing.T) {
	ctx := context.Background()
	harness := NewTestHarness(t, ctx)
	defer harness.Cleanup(ctx)

	// TODO: Start two capture instances
	// TODO: Verify only one owns the source
	// TODO: Kill leader
	// TODO: Verify standby takes over

	t.Log("Ownership race test placeholder")
}

// TestMetadataOutage tests behavior when metadata DB is unavailable.
func TestMetadataOutage(t *testing.T) {
	ctx := context.Background()
	harness := NewTestHarness(t, ctx)
	defer harness.Cleanup(ctx)

	// TODO: Start capture
	// TODO: Write transactions
	// TODO: Kill metadata DB
	// TODO: Verify WAL retained on source
	// TODO: Restart metadata
	// TODO: Verify recovery without loss

	t.Log("Metadata outage test placeholder")
}

// TestSourceRestart tests reconnection after source restart.
func TestSourceRestart(t *testing.T) {
	ctx := context.Background()
	harness := NewTestHarness(t, ctx)
	defer harness.Cleanup(ctx)

	// TODO: Start capture
	// TODO: Write transactions
	// TODO: Restart source
	// TODO: Verify capture reconnects and resumes

	t.Log("Source restart test placeholder")
}

// TestStaleConsumerFencing tests that stale consumers cannot commit.
func TestStaleConsumerFencing(t *testing.T) {
	ctx := context.Background()
	harness := NewTestHarness(t, ctx)
	defer harness.Cleanup(ctx)

	// TODO: Create consumer group
	// TODO: Consumer A polls and crashes before ACK
	// TODO: Consumer B claims partition
	// TODO: Consumer A tries stale ACK
	// TODO: Verify rejection

	t.Log("Stale fencing test placeholder")
}