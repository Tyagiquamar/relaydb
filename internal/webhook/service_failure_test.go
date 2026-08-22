package webhook

// Delivery failure scenes against a real metadata PostgreSQL (testcontainers)
// and a real HTTP destination whose behaviour flips between failing and
// healthy. Proves the documented contract: failed deliveries are retried with
// persisted state and backoff, recovered destinations receive exactly one
// successful delivery per event, exhausted deliveries are dead-lettered (never
// silently dropped), and the original event is retained either way.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/tyagiquamar/relaydb/internal/persistence"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

func startFailureMetaPG(t *testing.T, ctx context.Context) *persistence.Pool {
	t.Helper()
	c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("relaydb"),
		tcpostgres.WithUsername("relaydb"),
		tcpostgres.WithPassword("relaydb"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start metadata container (is Docker running?): %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("metadata conn string: %v", err)
	}
	pool, err := persistence.NewPool(ctx, persistence.DefaultConfig(dsn))
	if err != nil {
		t.Fatalf("metadata pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := persistence.NewMigrator(pool).Migrate(ctx); err != nil {
		t.Fatalf("migrate metadata: %v", err)
	}
	return pool
}

// seedEvent fabricates one committed insert event with the same shape capture
// persists: source -> relation version -> transaction -> event.
func seedEvent(t *testing.T, ctx context.Context, pool *persistence.Pool) (sourceID string, eventID []byte) {
	t.Helper()
	err := persistence.WithTx(ctx, pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO sources (name, description, credential_blob, replication_slot, publication)
			VALUES ('failure-test-source', 'delivery failure scene', '\x00'::bytea, 'slot_failure_test', 'pub_failure_test')
			RETURNING id
		`).Scan(&sourceID); err != nil {
			return err
		}
		var relVersion int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO relation_versions (source_id, relation_oid, schema_name, table_name, fingerprint, column_definitions, replica_identity)
			VALUES ($1, 16384, 'public', 'shop_orders', 'fp-failure-test',
			        '[{"name":"id","type":"int8","nullable":false,"position":1}]', 'default')
			RETURNING id
		`, sourceID).Scan(&relVersion); err != nil {
			return err
		}
		var txnID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO cdc_transactions (source_id, xid, commit_end_lsn, commit_timestamp, event_count, total_bytes)
			VALUES ($1, '1000'::xid8, '0/100'::pg_lsn, now(), 1, 128)
			RETURNING id
		`, sourceID).Scan(&txnID); err != nil {
			return err
		}
		eventID = make([]byte, 16)
		if _, err := rand.Read(eventID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO events (id, source_id, transaction_id, commit_end_lsn, sequence_number,
			                    schema_name, table_name, operation, relation_version_id, after, payload_hash)
			VALUES ($1, $2, $3::uuid, '0/100'::pg_lsn, 1, 'public', 'shop_orders', 'insert', $4,
			        '{"id":42}'::jsonb, 'failure-test-hash')
			RETURNING id
		`, eventID, sourceID, txnID, relVersion).Scan(&eventID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return sourceID, eventID
}

func createSink(t *testing.T, ctx context.Context, pool *persistence.Pool, url string, maxAttempts int) (sinkID string) {
	t.Helper()
	err := pool.QueryRow(ctx, `
		INSERT INTO webhook_sinks (name, url, secret_encrypted, enabled, max_attempts)
		VALUES ('failure-test-sink', $1, '\x00'::bytea, true, $2)
		RETURNING id
	`, url, maxAttempts).Scan(&sinkID)
	if err != nil {
		t.Fatalf("create sink: %v", err)
	}
	return sinkID
}

type destination struct {
	server   *httptest.Server
	failing  atomic.Bool
	requests atomic.Int64
	lastKey  atomic.Value // string
	mu       sync.Mutex
	hits     []time.Time // wall-clock record of every received request
}

func newDestination(t *testing.T) *destination {
	t.Helper()
	d := &destination{}
	d.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.hits = append(d.hits, time.Now())
		d.mu.Unlock()
		d.lastKey.Store(r.Header.Get("Idempotency-Key"))
		d.requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if d.failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"destination down"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"received":true}`))
	}))
	t.Cleanup(d.server.Close)
	d.failing.Store(true)
	return d
}

// hitGaps returns the intervals between consecutive requests received so far.
func (d *destination) hitGaps() []time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	gaps := make([]time.Duration, 0, len(d.hits))
	for i := 1; i < len(d.hits); i++ {
		gaps = append(gaps, d.hits[i].Sub(d.hits[i-1]))
	}
	return gaps
}

// runService starts the real delivery loop with an SSRF-unrestricted deliverer
// (needed to reach the loopback httptest destination) and fast backoff.
func runService(t *testing.T, pool *persistence.Pool) (svc *Service, cancel context.CancelFunc) {
	t.Helper()
	svc = &Service{
		pool:      pool,
		deliverer: NewDelivererWithOptions(Options{AllowPrivateAddresses: true}),
		envelope:  nil, // unsigned delivery; HMAC path is covered by deliverer unit tests
		backoff:   &Backoff{Initial: 200 * time.Millisecond, Max: 2 * time.Second},
		logger:    telemetry.With("test", "delivery-failure"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = svc.Run(ctx) }()
	return svc, cancel
}

func waitForCondition(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for: %s", timeout, what)
}

// TestDeliveryRetriesThenRecover proves: while the destination returns 500 the
// attempt is marked retryable (with the HTTP status recorded), the next attempt
// is scheduled with backoff instead of being dropped or immediately
// dead-lettered, and once the destination recovers the event is delivered
// successfully with the idempotency key preserved end-to-end.
func TestDeliveryRetriesThenRecover(t *testing.T) {
	ctx := context.Background()
	pool := startFailureMetaPG(t, ctx)
	dest := newDestination(t)

	_, eventID := seedEvent(t, ctx, pool)
	sinkID := createSink(t, ctx, pool, dest.server.URL, 5)
	_, cancel := runService(t, pool)
	defer cancel()

	// Phase A: destination down -> attempt 1 fails, retries are scheduled with
	// persisted backoff (proven behaviourally: repeated hits at the real
	// destination spaced by the backoff floor), and nothing is dead-lettered.
	waitForCondition(t, 30*time.Second, "attempt 1 marked retryable with http 500", func() bool {
		var status string
		var code int
		err := pool.QueryRow(ctx, `
			SELECT status, coalesce(http_status, 0) FROM delivery_attempts
			WHERE sink_id = $1 AND event_id = $2 AND attempt_number = 1
		`, sinkID, eventID).Scan(&status, &code)
		return err == nil && status == "retryable" && code == 500
	})
	// With max_attempts=5 the service must keep retrying while down; >=3 hits
	// prove the loop reschedules instead of giving up after one failure.
	waitForCondition(t, 45*time.Second, "at least 3 retry hits at the destination", func() bool {
		return dest.requests.Load() >= 3
	})
	for i, gap := range dest.hitGaps() {
		if gap < 100*time.Millisecond {
			t.Fatalf("retry hit %d arrived %s after its predecessor; backoff not enforced", i+2, gap)
		}
	}
	if key, _ := dest.lastKey.Load().(string); key == "" {
		t.Fatal("delivered request missing Idempotency-Key header")
	}
	assertNoDeadLetter(t, ctx, pool, sinkID, eventID, "while retrying")

	// Phase B: destination recovers -> eventual success, exactly one success row.
	dest.failing.Store(false)
	waitForCondition(t, 30*time.Second, "successful delivery after recovery", func() bool {
		var n int
		err := pool.QueryRow(ctx, `
			SELECT count(*) FROM delivery_attempts
			WHERE sink_id = $1 AND event_id = $2 AND status = 'success' AND http_status = 200
		`, sinkID, eventID).Scan(&n)
		return err == nil && n == 1
	})
	assertNoDeadLetter(t, ctx, pool, sinkID, eventID, "after recovery")
	if ev := eventExists(t, ctx, pool, eventID); !ev {
		t.Fatal("event vanished from the store after delivery")
	}
}

// TestDeliveryExhaustionDeadLetters proves: when every attempt fails up to the
// sink's max_attempts, attempts terminate in permanent_failure and the event is
// dead-lettered with structured history - never silently dropped, never lost
// from the event store.
func TestDeliveryExhaustionDeadLetters(t *testing.T) {
	ctx := context.Background()
	pool := startFailureMetaPG(t, ctx)
	dest := newDestination(t)
	defer dest.failing.Store(true) // stays failing for the whole scene

	_, eventID := seedEvent(t, ctx, pool)
	const maxAttempts = 2
	sinkID := createSink(t, ctx, pool, dest.server.URL, maxAttempts)
	_, cancel := runService(t, pool)
	defer cancel()

	waitForCondition(t, 45*time.Second, "all attempts terminal permanent_failure", func() bool {
		var pending, failed, total int
		err := pool.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE status = 'pending'),
			       count(*) FILTER (WHERE status = 'permanent_failure'),
			       count(*)
			FROM delivery_attempts WHERE sink_id = $1 AND event_id = $2
		`, sinkID, eventID).Scan(&pending, &failed, &total)
		return err == nil && total == maxAttempts && pending == 0 && failed == 1
	})

	var reason string
	var historyJSON []byte
	err := pool.QueryRow(ctx, `
		SELECT failure_reason, attempt_history FROM dead_letter_events
		WHERE event_id = $1 AND sink_id = $2
	`, eventID, sinkID).Scan(&reason, &historyJSON)
	if err != nil {
		t.Fatalf("event not dead-lettered after exhausting %d attempts: %v", maxAttempts, err)
	}
	if len(reason) == 0 {
		t.Fatal("dead letter has empty failure_reason")
	}
	var history []map[string]any
	if err := json.Unmarshal(historyJSON, &history); err != nil {
		t.Fatalf("attempt_history not valid JSON (%q): %v", historyJSON, err)
	}
	if len(history) == 0 || history[0]["attempt"].(float64) != float64(maxAttempts) {
		t.Fatalf("attempt_history = %s, want final attempt %d recorded", historyJSON, maxAttempts)
	}
	if got, want := dest.requests.Load(), int64(maxAttempts); got < want {
		t.Fatalf("destination saw %d requests, want >= %d (one per attempt)", got, want)
	}
	if ev := eventExists(t, ctx, pool, eventID); !ev {
		t.Fatal("event was dropped from the store on exhaustion")
	}
}

func assertNoDeadLetter(t *testing.T, ctx context.Context, pool *persistence.Pool, sinkID string, eventID []byte, when string) {
	t.Helper()
	var n int
	err := pool.QueryRow(ctx, `
		SELECT count(*) FROM dead_letter_events WHERE event_id = $1 AND sink_id = $2
	`, eventID, sinkID).Scan(&n)
	if err != nil || n != 0 {
		t.Fatalf("unexpected DLQ entry %s: count=%d err=%v", when, n, err)
	}
}

func eventExists(t *testing.T, ctx context.Context, pool *persistence.Pool, eventID []byte) bool {
	t.Helper()
	var n int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE id = $1`, eventID).Scan(&n)
	if err != nil {
		t.Fatalf("query event: %v", err)
	}
	return n == 1
}
