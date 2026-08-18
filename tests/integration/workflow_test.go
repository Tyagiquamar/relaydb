//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMain(m *testing.M) {
	// Setup testcontainers
	m.Run()
}

func setupPostgres(t *testing.T, ctx context.Context) (string, func()) {
	t.Helper()

	pg, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get connection string: %v", err)
	}

	return connStr, func() {
		pg.Terminate(ctx)
	}
}

func TestCaptureEndToEnd(t *testing.T) {
	ctx := context.Background()

	sourceConnStr, cleanupSource := setupPostgres(t, ctx)
	defer cleanupSource()

	metadataConnStr, cleanupMetadata := setupPostgres(t, ctx)
	defer cleanupMetadata()

	// Setup source with logical replication
	sourceConn, err := pgx.Connect(ctx, sourceConnStr)
	if err != nil {
		t.Fatalf("connect source: %v", err)
	}
	defer sourceConn.Close(ctx)

	_, err = sourceConn.Exec(ctx, "ALTER SYSTEM SET wal_level = logical")
	if err != nil {
		t.Fatalf("set wal_level: %v", err)
	}

	// Create test table
	_, err = sourceConn.Exec(ctx, `
		CREATE TABLE test_events (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT now()
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert test data
	_, err = sourceConn.Exec(ctx, "INSERT INTO test_events (name) VALUES ('test1'), ('test2')")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// TODO: Start capture service and verify events
	t.Log("Integration test placeholder - capture verification pending")
}