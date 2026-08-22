package integration

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tyagiquamar/relaydb/internal/consumer"
	"github.com/tyagiquamar/relaydb/internal/lease"
	"github.com/tyagiquamar/relaydb/internal/persistence"
)

// TestConsumerAckFencing proves against real PostgreSQL that a stale lease
// owner cannot advance a consumer offset after a newer owner takes over
// (fencing TOCTOU regression guard).
func TestConsumerAckFencing(t *testing.T) {
	ctx := context.Background()
	metaURL := startMetadata(t, ctx).mustConn(ctx)

	pool, err := persistence.NewPool(ctx, persistence.DefaultConfig(metaURL))
	if err != nil {
		t.Fatalf("metadata pool: %v", err)
	}
	defer pool.Close()
	if err := persistence.NewMigrator(pool).Migrate(ctx); err != nil {
		t.Fatalf("migrate metadata: %v", err)
	}

	groupID := newConsumerGroup(t, ctx, pool, 1)
	svc := consumer.NewService(pool)
	mgr := lease.NewManager(pool)
	const partition = 0

	// 1. Worker A claims the partition (generation N).
	genA, err := mgr.Claim(ctx, groupID, partition, "worker-a", 30*time.Second)
	if err != nil {
		t.Fatalf("worker-a claim: %v", err)
	}

	// 2. Establish an initial offset via a valid ACK.
	const initialLSN = uint64(0x100)
	if err := svc.Ack(ctx, groupID, partition, "worker-a", genA.Generation,
		initialLSN, 1, []byte("evt-1")); err != nil {
		t.Fatalf("worker-a initial ack: %v", err)
	}
	got := mustOffset(t, ctx, pool, groupID, partition)
	if got.lsn != initialLSN || got.seq != 1 || string(got.eventID) != "evt-1" {
		t.Fatalf("initial offset = lsn %#x seq %d event %q, want %#x/1/evt-1",
			got.lsn, got.seq, got.eventID, initialLSN)
	}

	// 3. Expire A's lease.
	expireLease(t, ctx, pool, groupID, partition)

	// 4. Worker B claims generation N+1.
	genB, err := mgr.Claim(ctx, groupID, partition, "worker-b", 30*time.Second)
	if err != nil {
		t.Fatalf("worker-b takeover claim: %v", err)
	}
	if genB.Generation <= genA.Generation {
		t.Fatalf("takeover generation = %d, want > %d", genB.Generation, genA.Generation)
	}

	// 5+6+7. Stale worker A ACK must fail and the offset must not move.
	err = svc.Ack(ctx, groupID, partition, "worker-a", genA.Generation,
		uint64(0x200), 2, []byte("evt-stale"))
	if err == nil {
		t.Fatal("stale-generation ACK accepted; fencing broken")
	}
	if !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale ACK error = %v, want stale-lease failure", err)
	}
	got = mustOffset(t, ctx, pool, groupID, partition)
	if got.lsn != initialLSN || got.seq != 1 || string(got.eventID) != "evt-1" {
		t.Fatalf("offset moved after stale ACK: lsn %#x seq %d event %q",
			got.lsn, got.seq, got.eventID)
	}

	// Monotonicity: a lower position is rejected even for the rightful owner.
	err = svc.Ack(ctx, groupID, partition, "worker-b", genB.Generation,
		uint64(0x50), 0, []byte("evt-regress"))
	if err == nil || !strings.Contains(err.Error(), "regression") {
		t.Fatalf("offset regression accepted: err=%v", err)
	}
	got = mustOffset(t, ctx, pool, groupID, partition)
	if got.lsn != initialLSN {
		t.Fatalf("offset moved after rejected regression: %#x", got.lsn)
	}
	// Equal LSN with larger sequence number wins.
	if err := svc.Ack(ctx, groupID, partition, "worker-b", genB.Generation,
		initialLSN, 2, []byte("evt-tie")); err != nil {
		t.Fatalf("equal-LSN higher-seq ack: %v", err)
	}

	// 8. Worker B ACKs a new high-water mark with its own generation.
	const finalLSN = uint64(0x300)
	if err := svc.Ack(ctx, groupID, partition, "worker-b", genB.Generation,
		finalLSN, 5, []byte("evt-final")); err != nil {
		t.Fatalf("worker-b ack: %v", err)
	}

	// 9. The stored offset equals B's ACK exactly.
	got = mustOffset(t, ctx, pool, groupID, partition)
	if got.lsn != finalLSN || got.seq != 5 || string(got.eventID) != "evt-final" {
		t.Fatalf("final offset = lsn %#x seq %d event %q, want %#x/5/evt-final",
			got.lsn, got.seq, got.eventID, finalLSN)
	}
}

// TestConsumerAckSerializesAgainstTakeover proves the race window directly:
// while an in-flight ACK holds the lease row FOR UPDATE between validation and
// offset write, a concurrent takeover claim cannot proceed — serialization by
// row lock makes interleaving impossible.
func TestConsumerAckSerializesAgainstTakeover(t *testing.T) {
	ctx := context.Background()
	metaURL := startMetadata(t, ctx).mustConn(ctx)

	pool, err := persistence.NewPool(ctx, persistence.DefaultConfig(metaURL))
	if err != nil {
		t.Fatalf("metadata pool: %v", err)
	}
	defer pool.Close()
	if err := persistence.NewMigrator(pool).Migrate(ctx); err != nil {
		t.Fatalf("migrate metadata: %v", err)
	}

	groupID := newConsumerGroup(t, ctx, pool, 1)
	svc := consumer.NewService(pool)
	mgr := lease.NewManager(pool)
	const partition = 0

	genA, err := mgr.Claim(ctx, groupID, partition, "worker-a", time.Minute)
	if err != nil {
		t.Fatalf("worker-a claim: %v", err)
	}

	// In-flight ACK critical section: one open transaction that locks and
	// validates the lease row, then advances the offset (same two statements
	// Service.Ack performs inside its transaction). Held open deliberately.
	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin ack tx: %v", err)
	}
	valid, err := mgr.ValidateForUpdate(ctx, txA, groupID, partition, "worker-a", genA.Generation)
	if err != nil || !valid {
		_ = txA.Rollback(ctx)
		t.Fatalf("in-flight ack lease validation = %v (err=%v); want valid", valid, err)
	}
	// pgx cannot encode a raw Go uint64 as pg_lsn in text format, so stage
	// the LSN as text and cast in SQL — the same workaround capture uses.
	if _, err := txA.Exec(ctx, `
		INSERT INTO consumer_offsets (group_id, partition, commit_end_lsn, sequence_number, last_event_id)
		VALUES ($1, $2, $3::pg_lsn, $4, $5)
		ON CONFLICT (group_id, partition) DO UPDATE
		SET commit_end_lsn = $3::pg_lsn, sequence_number = $4, last_event_id = $5, updated_at = now()
		WHERE consumer_offsets.commit_end_lsn < $3::pg_lsn
		   OR (consumer_offsets.commit_end_lsn = $3::pg_lsn AND consumer_offsets.sequence_number < $4)
	`, groupID, partition, "0/500", 1, []byte("evt-inflight")); err != nil {
		_ = txA.Rollback(ctx)
		t.Fatalf("in-flight ack offset update: %v", err)
	}

	// While the ACK transaction holds the lock, takeover must block: worker B
	// cannot even evaluate claim eligibility until the ACK commits or rolls
	// back, so it can never slip between validation and the offset write.
	claimResult := make(chan error, 1)
	go func() {
		_, err := mgr.Claim(ctx, groupID, partition, "worker-b", 30*time.Second)
		claimResult <- err
	}()
	select {
	case err := <-claimResult:
		t.Fatalf("takeover proceeded while an in-flight ACK held the lease row (err=%v)", err)
	case <-time.After(800 * time.Millisecond):
		// Still blocked: serialized behind the ACK's row lock, as required.
	}

	// Commit the ACK: worker A was the rightful owner throughout the whole
	// critical section, so its offset advancement lands.
	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("commit in-flight ack: %v", err)
	}
	got := mustOffset(t, ctx, pool, groupID, partition)
	if got.lsn != uint64(0x500) || string(got.eventID) != "evt-inflight" {
		t.Fatalf("post-commit offset = lsn %#x event %q; want in-flight ACK to land",
			got.lsn, got.eventID)
	}

	// Now let ownership actually change hands.
	select {
	case <-claimResult: // takeover already unblocked by commit; may have succeeded or failed on validity
	default:
	}
	expireLease(t, ctx, pool, groupID, partition)
	genB, err := mgr.Claim(ctx, groupID, partition, "worker-b", 30*time.Second)
	if err != nil {
		t.Fatalf("worker-b takeover claim: %v", err)
	}
	if genB.Generation <= genA.Generation {
		t.Fatalf("takeover generation = %d, want > %d", genB.Generation, genA.Generation)
	}

	// Stale owner A can no longer advance anything.
	if err := svc.Ack(ctx, groupID, partition, "worker-a", genA.Generation,
		uint64(0x900), 2, []byte("evt-stale")); err == nil {
		t.Fatal("stale-generation ACK accepted after takeover; fencing broken")
	}
	got = mustOffset(t, ctx, pool, groupID, partition)
	if got.lsn != uint64(0x500) {
		t.Fatalf("offset moved to %#x after stale ACK; want %#x unchanged", got.lsn, uint64(0x500))
	}

	// The new owner advances normally.
	if err := svc.Ack(ctx, groupID, partition, "worker-b", genB.Generation,
		uint64(0xA00), 3, []byte("evt-new-owner")); err != nil {
		t.Fatalf("worker-b ack: %v", err)
	}
	got = mustOffset(t, ctx, pool, groupID, partition)
	if got.lsn != uint64(0xA00) || got.seq != 3 {
		t.Fatalf("final offset = lsn %#x seq %d, want %#x/3", got.lsn, got.seq, uint64(0xA00))
	}
}

type storedOffset struct {
	lsn     uint64
	seq     int
	eventID []byte
}

func mustOffset(t *testing.T, ctx context.Context, pool *persistence.Pool, groupID string, partition int) storedOffset {
	t.Helper()
	var lsnStr string
	var seq int
	var eventID []byte
	err := pool.QueryRow(ctx, `
		SELECT commit_end_lsn::text, sequence_number, last_event_id
		FROM consumer_offsets WHERE group_id = $1 AND partition = $2
	`, groupID, partition).Scan(&lsnStr, &seq, &eventID)
	if err != nil {
		t.Fatalf("read offset: %v", err)
	}
	high, low, ok := strings.Cut(lsnStr, "/")
	if !ok {
		t.Fatalf("malformed offset LSN %q", lsnStr)
	}
	highVal, err := strconv.ParseUint(high, 16, 32)
	if err != nil {
		t.Fatalf("parse offset LSN %q: %v", lsnStr, err)
	}
	lowVal, err := strconv.ParseUint(low, 16, 32)
	if err != nil {
		t.Fatalf("parse offset LSN %q: %v", lsnStr, err)
	}
	return storedOffset{lsn: highVal<<32 | lowVal, seq: seq, eventID: eventID}
}

func expireLease(t *testing.T, ctx context.Context, pool *persistence.Pool, groupID string, partition int) {
	t.Helper()
	tag, err := pool.Exec(ctx, `
		UPDATE partition_leases SET lease_expires_at = now() - interval '1 second'
		WHERE group_id = $1 AND partition = $2
	`, groupID, partition)
	if err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("expire lease affected %d rows, want 1", tag.RowsAffected())
	}
}

func newConsumerGroup(t *testing.T, ctx context.Context, pool *persistence.Pool, partitions int) string {
	t.Helper()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	var groupID string
	err := pool.QueryRow(ctx, `
		WITH c AS (
			INSERT INTO consumers (name) VALUES ($1) RETURNING id
		), s AS (
			INSERT INTO sources (name, description, credential_blob, replication_slot, publication, status)
			VALUES ($2, 'consumer fencing test', 'unused'::bytea, 'slot_fencing_test', 'pub_fencing_test', 'registered')
			RETURNING id
		)
		INSERT INTO consumer_groups (consumer_id, source_id, name, partition_count)
		SELECT c.id, s.id, $3, $4 FROM c, s
		RETURNING id
	`, "fencing-consumer-"+suffix, "fencing-source-"+suffix, "fencing-group-"+suffix,
		partitions).Scan(&groupID)
	if err != nil {
		t.Fatalf("create consumer group: %v", err)
	}
	return groupID
}
