package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tyagiquamar/relaydb/internal/config"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

// Demo commerce app generates realistic order traffic
// to demonstrate RelayDB CDC capabilities.

func main() {
	cfg := config.MustLoad()
	logger := telemetry.Logger()

	logger.Info("starting demo-commerce",
		"source", cfg.SourceDBURL,
	)

	// Connect to source database
	conn, err := pgx.Connect(context.Background(), cfg.SourceDBURL)
	if err != nil {
		log.Fatalf("connect to source: %v", err) //nolint:gocritic // CLI entrypoints intentionally exit via log.Fatal; process teardown releases resources
	}
	defer func() { _ = conn.Close(context.Background()) }()

	// Create tables if not exist (idempotent)
	if err := createSchema(conn); err != nil {
		log.Fatalf("create schema: %v", err) //nolint:gocritic // CLI entrypoints intentionally exit via log.Fatal; process teardown releases resources
	}

	// Start HTTP server for order API
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", handleCreateOrder(conn))
	mux.HandleFunc("POST /orders/{id}/pay", handlePayOrder(conn))
	mux.HandleFunc("POST /orders/{id}/cancel", handleCancelOrder(conn))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Self-driving traffic: real writes into the source schema so the hosted
	// demo always has fresh CDC events flowing through capture -> webhooks.
	if secs := envInt("DEMO_TRAFFIC_INTERVAL_SECS", 0); secs > 0 {
		go runTrafficLoop(conn, time.Duration(secs)*time.Second)
		logger.Info("demo traffic enabled", "interval_secs", secs)
	}

	logger.Info("demo-commerce listening", "addr", cfg.HTTPAddr)
	logger.Info("endpoints: POST /orders, POST /orders/{id}/pay, POST /orders/{id}/cancel")

	if err := http.ListenAndServe(cfg.HTTPAddr, mux); err != nil {
		log.Fatal(err)
	}
}

// runTrafficLoop writes a realistic order lifecycle straight into the source
// schema: new order -> paid or cancelled (occasionally left pending). Every
// statement is real WAL that capture must decode and persist.
func runTrafficLoop(conn *pgx.Conn, every time.Duration) {
	ctx := context.Background()
	seq := 0
	for {
		time.Sleep(every)
		seq++

		var customerID int64
		if err := conn.QueryRow(ctx,
			`SELECT id FROM customers ORDER BY id LIMIT 1 OFFSET $1`, seq%2).Scan(&customerID); err != nil {
			log.Printf("traffic: pick customer: %v", err)
			continue
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			log.Printf("traffic: begin: %v", err)
			continue
		}
		productID := int64(1 + seq%2)
		var priceCents int
		if err := tx.QueryRow(ctx, `SELECT price_cents FROM products WHERE id = $1`, productID).Scan(&priceCents); err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		qty := 1 + seq%3

		var orderID int64
		if err := tx.QueryRow(ctx,
			`INSERT INTO orders (customer_id, total_cents) VALUES ($1, $2) RETURNING id`,
			customerID, priceCents*qty).Scan(&orderID); err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO order_items (order_id, product_id, quantity, price_cents) VALUES ($1,$2,$3,$4)`,
			orderID, productID, qty, priceCents); err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		if _, err := tx.Exec(ctx,
			`UPDATE products SET inventory_count = inventory_count - $1 WHERE id = $2`, qty, productID); err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		if err := tx.Commit(ctx); err != nil {
			continue
		}

		switch seq % 4 {
		case 0, 1:
			_, _ = conn.Exec(ctx, `UPDATE orders SET status='paid', updated_at=now() WHERE id=$1 AND status='pending'`, orderID)
		case 2:
			_, _ = conn.Exec(ctx, `UPDATE orders SET status='cancelled', updated_at=now() WHERE id=$1 AND status='pending'`, orderID)
		}
		log.Printf("traffic: order #%d lifecycle written", orderID)
	}
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func createSchema(conn *pgx.Conn) error {	_, err := conn.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS customers (
			id BIGSERIAL PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		
		CREATE TABLE IF NOT EXISTS products (
			id BIGSERIAL PRIMARY KEY,
			sku TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			price_cents INT NOT NULL,
			inventory_count INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		
		CREATE TABLE IF NOT EXISTS orders (
			id BIGSERIAL PRIMARY KEY,
			customer_id BIGINT NOT NULL REFERENCES customers(id),
			status TEXT NOT NULL DEFAULT 'pending',
			total_cents INT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		
		CREATE TABLE IF NOT EXISTS order_items (
			id BIGSERIAL PRIMARY KEY,
			order_id BIGINT NOT NULL REFERENCES orders(id),
			product_id BIGINT NOT NULL REFERENCES products(id),
			quantity INT NOT NULL,
			price_cents INT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
		
		-- Seed data
		INSERT INTO customers (email, name) VALUES
			('alice@example.com', 'Alice Johnson'),
			('bob@example.com', 'Bob Smith')
		ON CONFLICT (email) DO NOTHING;
		
		INSERT INTO products (sku, name, price_cents, inventory_count) VALUES
			('WIDGET-001', 'Standard Widget', 1999, 100),
			('GADGET-001', 'Deluxe Gadget', 4999, 50)
		ON CONFLICT (sku) DO NOTHING;
	`)
	return err
}

type CreateOrderRequest struct {
	CustomerID int64 `json:"customer_id"`
	Items      []struct {
		ProductID int64 `json:"product_id"`
		Quantity  int   `json:"quantity"`
	} `json:"items"`
}

func handleCreateOrder(conn *pgx.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateOrderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Calculate total
		var total int
		for _, item := range req.Items {
			var price int
			err := conn.QueryRow(r.Context(),
				"SELECT price_cents FROM products WHERE id = $1", item.ProductID).Scan(&price)
			if err != nil {
				http.Error(w, "product not found", http.StatusBadRequest)
				return
			}
			total += price * item.Quantity
		}

		// Create order in transaction
		tx, err := conn.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()

		var orderID int64
		err = tx.QueryRow(r.Context(), `
			INSERT INTO orders (customer_id, total_cents)
			VALUES ($1, $2)
			RETURNING id
		`, req.CustomerID, total).Scan(&orderID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Add items
		for _, item := range req.Items {
			var price int
			if err := tx.QueryRow(r.Context(),
				"SELECT price_cents FROM products WHERE id = $1", item.ProductID).Scan(&price); err != nil {
				_ = tx.Rollback(r.Context())
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			_, err = tx.Exec(r.Context(), `
				INSERT INTO order_items (order_id, product_id, quantity, price_cents)
				VALUES ($1, $2, $3, $4)
			`, orderID, item.ProductID, item.Quantity, price)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Update inventory
			_, err = tx.Exec(r.Context(), `
				UPDATE products SET inventory_count = inventory_count - $1
				WHERE id = $2
			`, item.Quantity, item.ProductID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if err := tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"order_id":    orderID,
			"total_cents": total,
			"status":      "pending",
		})
	}
}

func handlePayOrder(conn *pgx.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		_, err := conn.Exec(r.Context(), `
			UPDATE orders SET status = 'paid', updated_at = now()
			WHERE id = $1 AND status = 'pending'
		`, id)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "paid"})
	}
}

func handleCancelOrder(conn *pgx.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		tx, err := conn.Begin(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()

		// Get order items to restore inventory
		rows, err := tx.Query(r.Context(), `
			SELECT product_id, quantity FROM order_items WHERE order_id = $1
		`, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		type item struct {
			productID int64
			quantity  int
		}
		var items []item
		for rows.Next() {
			var i item
			if err := rows.Scan(&i.productID, &i.quantity); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			items = append(items, i)
		}
		rows.Close()

		// Restore inventory
		for _, i := range items {
			if _, err := tx.Exec(r.Context(), `
				UPDATE products SET inventory_count = inventory_count + $1
				WHERE id = $2
			`, i.quantity, i.productID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// Cancel order
		_, err = tx.Exec(r.Context(), `
			UPDATE orders SET status = 'cancelled', updated_at = now()
			WHERE id = $1 AND status = 'pending'
		`, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
	}
}