package integration

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/tyagiquamar/relaydb/internal/capture"
	"github.com/tyagiquamar/relaydb/internal/checkpoint"
	"github.com/tyagiquamar/relaydb/internal/config"
	"github.com/tyagiquamar/relaydb/internal/crypto"
	"github.com/tyagiquamar/relaydb/internal/persistence"
)

// TestCaptureEndToEnd drives a real logical-replication round trip against two
// throwaway Postgres clusters: schema + publication on the source, migrations
// + source registration on metadata, the capture service streaming pgoutput
// in-process. Proves: every committed DML lands in the event store in order,
// a crash-before-ACK replay re-ingests the same transaction exactly once, and
// checkpoint writes bearing a stale generation are fenced out.
func TestCaptureEndToEnd(t *testing.T) {
	ctx := context.Background()

	sourceURL := startSource(t, ctx).mustConn(ctx)
	metaURL := startMetadata(t, ctx).mustConn(ctx)

	pool, err := persistence.NewPool(ctx, persistence.DefaultConfig(metaURL))
	if err != nil {
		t.Fatalf("metadata pool: %v", err)
	}
	defer pool.Close()
	if err := persistence.NewMigrator(pool).Migrate(ctx); err != nil {
		t.Fatalf("migrate metadata: %v", err)
	}

	setupSourceSchema(t, ctx, sourceURL)
	sourceID := registerSource(t, ctx, metaURL)
	cfg := e2eConfig(sourceURL, metaURL)

	// Phase A: happy-path capture of insert -> update -> delete.
	runCtx, cancelRun := context.WithCancel(ctx)
	svc := capture.NewService(cfg, pool)
	svcErr := make(chan error, 1)
	go func() { svcErr <- svc.Run(runCtx, sourceID) }()
	waitForOwnership(t, pool, sourceID)

	execute(t, ctx, sourceURL, `INSERT INTO shop_orders (id, item, qty) VALUES (1, 'widget', 3)`)
	execute(t, ctx, sourceURL, `UPDATE shop_orders SET qty = 5 WHERE id = 1`)
	execute(t, ctx, sourceURL, `DELETE FROM shop_orders WHERE id = 1`)
	waitForEvents(t, pool, sourceID, 3, 30*time.Second)
	assertOperationSequence(t, pool, sourceID, []string{"insert", "update", "delete"})

	cancelRun()
	if err := <-svcErr; err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
		t.Fatalf("capture run A: %v", err)
	}
	totalAfterA := countEventsTotal(t, pool, sourceID)

	// Phase B: crash-replay idempotency. A change commits while capture is
	// down; the restarted capture persists it, "crashes" before ACK (test
	// hook), reconnects, and re-ingests the same transaction from the same
	// persisted LSN. The event must exist exactly once afterwards.
	execute(t, ctx, sourceURL, `INSERT INTO shop_orders (id, item, qty) VALUES (2, 'gadget', 7)`)

	svc2 := capture.NewService(cfg, pool)
	svc2.SetCrashAfterCommit(true)
	run2Ctx, cancelRun2 := context.WithCancel(ctx)
	go func() { _ = svc2.Run(run2Ctx, sourceID) }()
	time.Sleep(6 * time.Second) // allow >=2 crash -> reconnect -> replay cycles
	cancelRun2()

	if got := countEventsTotal(t, pool, sourceID); got != totalAfterA+1 {
		t.Fatalf("events after crash-replay = %d, want exactly %d (+1 new, zero duplicates)",
			got, totalAfterA)
	}

	// Phase C: checkpoint fencing. Standby takeover requires the leader's
	// lease to lapse first (a healthy capture must never be stolen from).
	time.Sleep(cfg.LeaseDuration * 2) // let svc2's lease lapse
	mgr := checkpoint.NewManager(pool)
	genA, err := mgr.Claim(ctx, sourceID, "owner-a", cfg.LeaseDuration)
	if err != nil {
		t.Fatalf("owner-a claim: %v", err)
	}
	time.Sleep(cfg.LeaseDuration * 2)
	genB, err := mgr.Claim(ctx, sourceID, "owner-b", cfg.LeaseDuration)
	if err != nil {
		t.Fatalf("owner-b takeover claim: %v", err)
	}
	if genB <= genA {
		t.Fatalf("takeover generation = %d, want > %d", genB, genA)
	}
	if err := mgr.Heartbeat(ctx, sourceID, "owner-a", genA, cfg.LeaseDuration); err == nil {
		t.Fatal("stale-generation heartbeat accepted; fencing broken")
	}
}

type sourceContainer struct {
	connStr func() (string, error)
}

func (c sourceContainer) mustConn(ctx context.Context) string {
	url, err := c.connStr()
	if err != nil {
		panic(err)
	}
	return url
}

func startSource(t *testing.T, ctx context.Context) sourceContainer {
	t.Helper()
	// wal_level MUST be set at boot via command flags: ALTER SYSTEM alone
	// cannot enable logical replication without a restart.
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "postgres:16-alpine",
			Env:          map[string]string{"POSTGRES_DB": "source", "POSTGRES_USER": "relaydb", "POSTGRES_PASSWORD": "relaydb"},
			Cmd:          []string{"postgres", "-c", "wal_level=logical", "-c", "max_replication_slots=10", "-c", "max_wal_senders=10"},
			ExposedPorts: []string{"5432/tcp"},
			WaitingFor:   wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start source container (is Docker running?): %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	return sourceContainer{connStr: func() (string, error) {
		return gConnString(ctx, c)
	}}
}

func gConnString(ctx context.Context, c testcontainers.Container) (string, error) {
	host, err := c.Host(ctx)
	if err != nil {
		return "", err
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("postgres://relaydb:relaydb@%s:%s/source?sslmode=disable", host, port.Port()), nil
}

func startMetadata(t *testing.T, ctx context.Context) sourceContainer {
	t.Helper()
	c, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("relaydb"),
		postgres.WithUsername("relaydb"),
		postgres.WithPassword("relaydb"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("start metadata container: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	return sourceContainer{connStr: func() (string, error) {
		return c.ConnectionString(ctx, "sslmode=disable")
	}}
}

func setupSourceSchema(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	pool, err := persistence.NewPool(ctx, persistence.DefaultConfig(dsn))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	stmts := []string{
		`DROP PUBLICATION IF EXISTS relaydb_pub`,
		`DROP TABLE IF EXISTS shop_orders`,
		`CREATE TABLE shop_orders (id bigint PRIMARY KEY, item text NOT NULL, qty int NOT NULL)`,
		`CREATE PUBLICATION relaydb_pub FOR TABLE shop_orders`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("source setup %q: %v", stmt, err)
		}
	}
}

func registerSource(t *testing.T, ctx context.Context, metaDSN string) string {
	t.Helper()
	pool, err := persistence.NewPool(ctx, persistence.DefaultConfig(metaDSN))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	env, err := crypto.NewEnvelope(testMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	aad := crypto.ComputeAAD("e2e-source", "source-credential")
	blob, err := env.Encrypt([]byte("unused-in-test"), aad)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	err = pool.QueryRow(ctx, `
		INSERT INTO sources (name, description, credential_blob, replication_slot, publication, status)
		VALUES ('e2e-source', 'integration test', $1, 'relaydb_slot_e2e', 'relaydb_pub', 'registered')
		RETURNING id
	`, blob).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func execute(t *testing.T, ctx context.Context, dsn, stmt string) {
	t.Helper()
	pool, err := persistence.NewPool(ctx, persistence.DefaultConfig(dsn))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, stmt); err != nil {
		t.Fatalf("execute %q: %v", stmt, err)
	}
}

func waitForOwnership(t *testing.T, pool *persistence.Pool, sourceID string) {
	t.Helper()
	mgr := checkpoint.NewManager(pool)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		cp, _ := mgr.Get(context.Background(), sourceID)
		if cp != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("capture never claimed ownership of the source")
}

func waitForEvents(t *testing.T, pool *persistence.Pool, sourceID string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if n := countEventsTotal(t, pool, sourceID); n >= want {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("only %d events captured within %s, want >=%d", countEventsTotal(t, pool, sourceID), timeout, want)
}

func countEventsTotal(t *testing.T, pool *persistence.Pool, sourceID string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM events WHERE source_id = $1`, sourceID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func assertOperationSequence(t *testing.T, pool *persistence.Pool, sourceID string, want []string) {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT operation FROM events
		WHERE source_id = $1 AND table_name = 'shop_orders'
		ORDER BY commit_end_lsn, sequence_number
	`, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var op string
		if err := rows.Scan(&op); err != nil {
			t.Fatal(err)
		}
		got = append(got, op)
	}
	if len(got) != len(want) {
		t.Fatalf("captured operations %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("captured operations %v, want %v", got, want)
		}
	}
}

var testMasterKey = base64.StdEncoding.EncodeToString([]byte("relaydb-e2e-test-master-key-32b!"))

func e2eConfig(sourceURL, metaURL string) config.Config {
	return config.Config{
		MetadataDBURL:             metaURL,
		SourceDBURL:               sourceURL,
		SourceName:                "e2e-source",
		ReplicationSlot:           "relaydb_slot_e2e",
		Publication:               "relaydb_pub",
		StandbyMessageTimeout:     5 * time.Second,
		CaptureOwnerID:            "e2e-owner",
		LeaseDuration:             3 * time.Second,
		HeartbeatInterval:         500 * time.Millisecond,
		MaxTransactionBufferBytes: 8 * 1024 * 1024,
		MaxEventBatchSize:         1000,
		MaxInflightTransactions:   100,
		MasterKey:                 testMasterKey,
	}
}
