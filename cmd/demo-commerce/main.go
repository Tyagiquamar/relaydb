package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

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
		log.Fatalf("connect to source: %v", err)
	}
	defer conn.Close(context.Background())

	// Create tables if not exist (idempotent)
	if err := createSchema(conn); err != nil {
		log.Fatalf("create schema: %v", err)
	}

	// Start HTTP server for order API
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", handleCreateOrder(conn))
	mux.HandleFunc("POST /orders/{id}/pay", handlePayOrder(conn))
	mux.HandleFunc("POST /orders/{id}/cancel", handleCancelOrder(conn))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	port := 8081
	logger.Info("demo-commerce listening", "port", port)
	logger.Info("endpoints: POST /orders, POST /orders/{id}/pay, POST /orders/{id}/cancel")

	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux); err != nil {
		log.Fatal(err)
	}
}

func createSchema(conn *pgx.Conn) error {
	_, err := conn.Exec(context.Background(), `
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
		defer tx.Rollback(r.Context())

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
			tx.QueryRow(r.Context(),
				"SELECT price_cents FROM products WHERE id = $1", item.ProductID).Scan(&price)

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
		json.NewEncoder(w).Encode(map[string]any{
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
		json.NewEncoder(w).Encode(map[string]string{"status": "paid"})
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
		defer tx.Rollback(r.Context())

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
			rows.Scan(&i.productID, &i.quantity)
			items = append(items, i)
		}
		rows.Close()

		// Restore inventory
		for _, i := range items {
			tx.Exec(r.Context(), `
				UPDATE products SET inventory_count = inventory_count + $1
				WHERE id = $2
			`, i.quantity, i.productID)
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
		json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
	}
}