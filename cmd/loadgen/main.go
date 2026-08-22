package main

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tyagiquamar/relaydb/internal/config"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

var (
	ordersCreated int64
	itemsCreated  int64
	bytesWritten  int64
)

func main() {
	cfg := config.MustLoad()
	logger := telemetry.Logger()

	logger.Info("starting loadgen",
		"source", cfg.SourceDBURL,
	)

	conn, err := pgx.Connect(context.Background(), cfg.SourceDBURL)
	if err != nil {
		log.Fatalf("connect: %v", err) //nolint:gocritic // CLI entrypoints intentionally exit via log.Fatal; process teardown releases resources
	}
	defer func() { _ = conn.Close(context.Background()) }()

	// Ensure schema
	if err := createSchema(conn); err != nil {
		log.Fatalf("create schema: %v", err) //nolint:gocritic // CLI entrypoints intentionally exit via log.Fatal; process teardown releases resources
	}

	// Start metrics server
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", telemetry.MetricsHandler())
		mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"orders_created": atomic.LoadInt64(&ordersCreated),
				"items_created":  atomic.LoadInt64(&itemsCreated),
				"bytes_written":  atomic.LoadInt64(&bytesWritten),
			})
		})
		_ = http.ListenAndServe(":8082", mux)
	}()

	// Run load scenarios
	scenario := envOr("SCENARIO", "medium")
	duration := envDurationOr("DURATION", 60*time.Second)

	logger.Info("running scenario", "name", scenario, "duration", duration)

	switch scenario {
	case "light":
		runLoad(conn, 1, 10, 10, duration)
	case "medium":
		runLoad(conn, 10, 100, 50, duration)
	case "heavy":
		runLoad(conn, 50, 500, 100, duration)
	case "burst":
		runBurst(conn, 100000, duration)
	default:
		log.Fatalf("unknown scenario: %s", scenario) //nolint:gocritic // CLI entrypoints intentionally exit via log.Fatal; process teardown releases resources
	}

	logger.Info("loadgen complete",
		"orders", atomic.LoadInt64(&ordersCreated),
		"items", atomic.LoadInt64(&itemsCreated),
	)
}

func runLoad(conn *pgx.Conn, writers, txPerSec, rowsPerTx int, duration time.Duration) {
	ctx := context.Background()
	ticker := time.NewTicker(time.Second / time.Duration(txPerSec))
	defer ticker.Stop()
	deadline := time.Now().Add(duration)

	for time.Now().Before(deadline) {
		<-ticker.C
		for w := 0; w < writers; w++ {
			go func() {
				tx, err := conn.Begin(ctx)
				if err != nil {
					return
				}
				defer func() { _ = tx.Rollback(ctx) }()

				var orderID int64
				err = tx.QueryRow(ctx, `
					INSERT INTO orders (customer_id, total_cents)
					VALUES ($1, $2) RETURNING id
				`, rand.Int63n(100)+1, rand.Intn(10000)+100).Scan(&orderID) //nolint:gosec // load generator: statistical randomness only
				if err != nil {
					return
				}
				atomic.AddInt64(&ordersCreated, 1)

				for i := 0; i < rowsPerTx; i++ {
					_, err = tx.Exec(ctx, `
						INSERT INTO order_items (order_id, product_id, quantity, price_cents)
						VALUES ($1, $2, $3, $4)
					`, orderID, rand.Int63n(10)+1, rand.Intn(5)+1, rand.Intn(1000)+10) //nolint:gosec // load generator: statistical randomness only
					if err != nil {
						return
					}
					atomic.AddInt64(&itemsCreated, 1)
				}
				// Best-effort commit; failed txs simply don't count toward totals.
				_ = tx.Commit(ctx)
			}()
		}
	}
}

func runBurst(conn *pgx.Conn, totalRows int, duration time.Duration) {
	ctx := context.Background()
	telemetry.Info("starting burst", "rows", totalRows)

	tx, err := conn.Begin(ctx)
	if err != nil {
		log.Fatalf("begin: %v", err) //nolint:gocritic // CLI entrypoints intentionally exit via log.Fatal; process teardown releases resources
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var orderID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO orders (customer_id, total_cents)
		VALUES (1, $1) RETURNING id
	`, totalRows*100).Scan(&orderID)
	if err != nil {
		log.Fatalf("create order: %v", err) //nolint:gocritic // CLI entrypoints intentionally exit via log.Fatal; process teardown releases resources
	}

	start := time.Now()
	for i := 0; i < totalRows; i++ {
		_, err = tx.Exec(ctx, `
			INSERT INTO order_items (order_id, product_id, quantity, price_cents)
			VALUES ($1, $2, 1, 100)
		`, orderID, (i%10)+1)
		if err != nil {
			log.Fatalf("insert %d: %v", i, err) //nolint:gocritic // CLI entrypoints intentionally exit via log.Fatal; process teardown releases resources
		}
		if i%10000 == 0 {
			telemetry.Info("burst progress", "rows", i, "elapsed", time.Since(start))
		}
	}

	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit: %v", err) //nolint:gocritic // CLI entrypoints intentionally exit via log.Fatal; process teardown releases resources
	}
	telemetry.Info("burst complete", "rows", totalRows, "duration", time.Since(start))
}

func createSchema(conn *pgx.Conn) error {
	_, err := conn.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS customers (
			id BIGSERIAL PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS products (
			id BIGSERIAL PRIMARY KEY,
			sku TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			price_cents INT NOT NULL,
			inventory_count INT NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS orders (
			id BIGSERIAL PRIMARY KEY,
			customer_id BIGINT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			total_cents INT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		CREATE TABLE IF NOT EXISTS order_items (
			id BIGSERIAL PRIMARY KEY,
			order_id BIGINT NOT NULL,
			product_id BIGINT NOT NULL,
			quantity INT NOT NULL,
			price_cents INT NOT NULL
		);
	`)
	return err
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDurationOr(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}