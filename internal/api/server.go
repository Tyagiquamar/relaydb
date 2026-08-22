package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tyagiquamar/relaydb/internal/config"
	"github.com/tyagiquamar/relaydb/internal/crypto"
	"github.com/tyagiquamar/relaydb/internal/persistence"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

// Server is the REST API server.
type Server struct {
	cfg    config.Config
	pool   *persistence.Pool
	logger *slog.Logger

	// Auth
	adminKeyID  string
	adminKey    string
	readerKeyID string
	readerKey   string
}

// NewServer creates the API server.
func NewServer(cfg config.Config, pool *persistence.Pool) *Server {
	return &Server{
		cfg:         cfg,
		pool:        pool,
		logger:      telemetry.With("service", "api"),
		adminKeyID:  cfg.AdminAPIKeyID,
		adminKey:    cfg.AdminAPIKey,
		readerKeyID: cfg.ReaderAPIKeyID,
		readerKey:   cfg.ReaderAPIKey,
	}
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health (no auth)
	mux.HandleFunc("GET /health/live", s.handleLive)
	mux.HandleFunc("GET /health/ready", s.handleReady)
	mux.Handle("GET /metrics", telemetry.MetricsHandler())

	// API v1 (authenticated)
	mux.HandleFunc("POST /api/v1/sources", s.authAdmin(s.handleCreateSource))
	mux.HandleFunc("GET /api/v1/sources", s.authReader(s.handleListSources))
	mux.HandleFunc("GET /api/v1/sources/{id}", s.authReader(s.handleGetSource))
	mux.HandleFunc("GET /api/v1/sources/{id}/status", s.authReader(s.handleSourceStatus))
	mux.HandleFunc("GET /api/v1/events", s.authReader(s.handleListEvents))
	mux.HandleFunc("GET /api/v1/events/{id}", s.authReader(s.handleGetEvent))
	mux.HandleFunc("GET /api/v1/transactions/{xid}", s.authReader(s.handleGetTransaction))
	mux.HandleFunc("GET /api/v1/stats", s.authReader(s.handleStats))
	mux.HandleFunc("GET /api/v1/dlq", s.authReader(s.handleListDLQ))

	return mux
}

// Auth middleware
func (s *Server) authAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyID, key := s.parseAuth(r)
		if !s.matchAdmin(keyID, key) {
			s.writeError(w, http.StatusUnauthorized, "invalid admin credentials")
			return
		}
		next(w, r)
	}
}

func (s *Server) authReader(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyID, key := s.parseAuth(r)
		if s.matchAdmin(keyID, key) || s.matchReader(keyID, key) {
			next(w, r)
			return
		}
		s.writeError(w, http.StatusUnauthorized, "invalid credentials")
	}
}

// matchAdmin reports constant-time equality with the admin credential pair.
func (s *Server) matchAdmin(keyID, key string) bool {
	return subtle.ConstantTimeCompare([]byte(keyID), []byte(s.adminKeyID)) == 1 &&
		subtle.ConstantTimeCompare([]byte(key), []byte(s.adminKey)) == 1
}

// matchReader reports constant-time equality with the reader credential pair.
func (s *Server) matchReader(keyID, key string) bool {
	return subtle.ConstantTimeCompare([]byte(keyID), []byte(s.readerKeyID)) == 1 &&
		subtle.ConstantTimeCompare([]byte(key), []byte(s.readerKey)) == 1
}

func (s *Server) parseAuth(r *http.Request) (keyID, key string) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return "", ""
	}

	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", ""
	}

	// Format: keyID:key
	cred := parts[1]
	if idx := strings.Index(cred, ":"); idx > 0 {
		return cred[:idx], cred[idx+1:]
	}
	return "", cred
}

// Health handlers
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	// Check metadata DB
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.pool.QueryRow(ctx, "SELECT 1").Scan(new(int)); err != nil {
		s.writeError(w, http.StatusServiceUnavailable, "metadata database unreachable")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// Source handlers
type CreateSourceRequest struct {
	Name             string `json:"name"`
	Description      string `json:"description"`
	ConnectionString string `json:"connection_string"` // Will be encrypted
	ReplicationSlot  string `json:"replication_slot"`
	Publication      string `json:"publication"`
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	var req CreateSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" || req.ConnectionString == "" {
		s.writeError(w, http.StatusBadRequest, "name and connection_string are required")
		return
	}

	// Validate connection and check replica identity
	if err := s.validateSource(r.Context(), req.ConnectionString); err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("source validation failed: %v", err))
		return
	}

	slot := req.ReplicationSlot
	if slot == "" {
		slot = "relaydb_slot"
	}
	pub := req.Publication
	if pub == "" {
		pub = "relaydb_pub"
	}

	// Encrypt connection string with the crypto envelope (KTD-2).
	// AAD binds the ciphertext to this source name.
	env, err := crypto.NewEnvelope(s.cfg.MasterKey)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "crypto not configured")
		return
	}
	aad := crypto.ComputeAAD(req.Name, "source-credential")
	blob, err := env.Encrypt([]byte(req.ConnectionString), aad)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "credential encryption failed")
		return
	}

	var id string
	err = s.pool.QueryRow(r.Context(), `
		INSERT INTO sources (name, description, credential_blob, replication_slot, publication, status)
		VALUES ($1, $2, $3, $4, $5, 'registered')
		RETURNING id
	`, req.Name, req.Description, blob, slot, pub).Scan(&id)
	if err != nil {
		s.writeError(w, http.StatusConflict, fmt.Sprintf("insert source failed: %v", err))
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]any{
		"id":     id,
		"name":   req.Name,
		"status": "registered",
	})
}

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, name, COALESCE(description, ''), replication_slot, publication, status, created_at
		FROM sources
		ORDER BY created_at DESC
	`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var sources []map[string]any
	for rows.Next() {
		var id, name, desc, slot, pub, status string
		var createdAt time.Time
		if err := rows.Scan(&id, &name, &desc, &slot, &pub, &status, &createdAt); err != nil {
			continue
		}
		sources = append(sources, map[string]any{
			"id":               id,
			"name":             name,
			"description":      desc,
			"replication_slot": slot,
			"publication":      pub,
			"status":           status,
			"created_at":       createdAt,
			// credential_blob intentionally omitted
		})
	}

	s.writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (s *Server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var source map[string]any
	var name, desc, slot, pub, status string
	var createdAt, updatedAt time.Time

	err := s.pool.QueryRow(r.Context(), `
		SELECT name, COALESCE(description, ''), replication_slot, publication, status, created_at, updated_at
		FROM sources WHERE id = $1
	`, id).Scan(&name, &desc, &slot, &pub, &status, &createdAt, &updatedAt)

	if err == pgx.ErrNoRows {
		s.writeError(w, http.StatusNotFound, "source not found")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	source = map[string]any{
		"id":               id,
		"name":             name,
		"description":      desc,
		"replication_slot": slot,
		"publication":      pub,
		"status":           status,
		"created_at":       createdAt,
		"updated_at":       updatedAt,
	}

	s.writeJSON(w, http.StatusOK, source)
}

func (s *Server) handleSourceStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var cp struct {
		ReceivedLSN     string
		PersistedLSN    string
		AcknowledgedLSN string
		Owner           string
		Generation      int64
		LeaseExpires    time.Time
	}

	err := s.pool.QueryRow(r.Context(), `
		SELECT received_lsn::text, persisted_lsn::text, acknowledged_lsn::text,
		       capture_owner, owner_generation, lease_expires_at
		FROM source_checkpoints WHERE source_id = $1
	`, id).Scan(&cp.ReceivedLSN, &cp.PersistedLSN, &cp.AcknowledgedLSN,
		&cp.Owner, &cp.Generation, &cp.LeaseExpires)

	if err == pgx.ErrNoRows {
		s.writeError(w, http.StatusNotFound, "no checkpoint found")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"source_id":        id,
		"received_lsn":     cp.ReceivedLSN,
		"persisted_lsn":    cp.PersistedLSN,
		"acknowledged_lsn": cp.AcknowledgedLSN,
		"capture_owner":    cp.Owner,
		"owner_generation": cp.Generation,
		"lease_expires_at": cp.LeaseExpires,
	})
}

// Event handlers
func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	// Parse filters
	sourceID := r.URL.Query().Get("source_id")
	schema := r.URL.Query().Get("schema")
	table := r.URL.Query().Get("table")
	operation := r.URL.Query().Get("operation")
	limit := 100

	// Build query (id is a 16-byte ULID bytea — return it hex-encoded)
	query := `
		SELECT encode(id, 'hex'), source_id, transaction_id, commit_end_lsn::text, sequence_number,
		       schema_name, table_name, operation, "before", "after", key_columns,
		       payload_hash, created_at
		FROM events
		WHERE 1=1
	`
	args := []any{}
	argIdx := 1

	if sourceID != "" {
		query += fmt.Sprintf(" AND source_id = $%d", argIdx)
		args = append(args, sourceID)
		argIdx++
	}
	if schema != "" {
		query += fmt.Sprintf(" AND schema_name = $%d", argIdx)
		args = append(args, schema)
		argIdx++
	}
	if table != "" {
		query += fmt.Sprintf(" AND table_name = $%d", argIdx)
		args = append(args, table)
		argIdx++
	}
	if operation != "" {
		query += fmt.Sprintf(" AND operation = $%d", argIdx)
		args = append(args, operation)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY commit_end_lsn DESC, sequence_number DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := s.pool.Query(r.Context(), query, args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var events []map[string]any
	for rows.Next() {
		var id, sourceID, txID, lsn, schema, table, op, hash string
		var seq int
		var before, after, key []byte
		var createdAt time.Time

		if err := rows.Scan(&id, &sourceID, &txID, &lsn, &seq, &schema, &table, &op,
			&before, &after, &key, &hash, &createdAt); err != nil {
			s.logger.Warn("scan event row failed", "error", err)
			continue
		}

		events = append(events, map[string]any{
			"id":              id,
			"source_id":       sourceID,
			"transaction_id":  txID,
			"commit_end_lsn":  lsn,
			"sequence_number": seq,
			"schema_name":     schema,
			"table_name":      table,
			"operation":       op,
			"before":          json.RawMessage(before),
			"after":           json.RawMessage(after),
			"key_columns":     json.RawMessage(key),
			"payload_hash":    hash,
			"created_at":      createdAt,
		})
	}

	s.writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var event map[string]any
	var sourceID, txID, lsn, schema, table, op, hash string
	var seq int
	var before, after, key []byte
	var createdAt time.Time

	err := s.pool.QueryRow(r.Context(), `
		SELECT encode(id, 'hex'), source_id, transaction_id, commit_end_lsn::text, sequence_number,
		       schema_name, table_name, operation, "before", "after", key_columns,
		       payload_hash, created_at
		FROM events WHERE id = decode($1, 'hex')
	`, id).Scan(&id, &sourceID, &txID, &lsn, &seq, &schema, &table, &op,
		&before, &after, &key, &hash, &createdAt)

	if err == pgx.ErrNoRows {
		s.writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	event = map[string]any{
		"id":              id,
		"source_id":       sourceID,
		"transaction_id":  txID,
		"commit_end_lsn":  lsn,
		"sequence_number": seq,
		"schema_name":     schema,
		"table_name":      table,
		"operation":       op,
		"before":          json.RawMessage(before),
		"after":           json.RawMessage(after),
		"key_columns":     json.RawMessage(key),
		"payload_hash":    hash,
		"created_at":      createdAt,
	}

	s.writeJSON(w, http.StatusOK, event)
}

func (s *Server) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	xid := r.PathValue("xid")

	// Get transaction
	var tx struct {
		ID              string
		SourceID        string
		CommitLSN       string
		CommitTimestamp time.Time
		EventCount      int
	}

	err := s.pool.QueryRow(r.Context(), `
		SELECT id, source_id, commit_end_lsn::text, commit_timestamp, event_count
		FROM cdc_transactions WHERE xid = $1::text::xid8
	`, xid).Scan(&tx.ID, &tx.SourceID, &tx.CommitLSN, &tx.CommitTimestamp, &tx.EventCount)

	if err == pgx.ErrNoRows {
		s.writeError(w, http.StatusNotFound, "transaction not found")
		return
	}
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Get events
	rows, err := s.pool.Query(r.Context(), `
		SELECT encode(id, 'hex'), sequence_number, schema_name, table_name, operation, "before", "after"
		FROM events
		WHERE transaction_id = $1
		ORDER BY sequence_number
	`, tx.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	var events []map[string]any
	for rows.Next() {
		var id, schema, table, op string
		var seq int
		var before, after []byte

		if err := rows.Scan(&id, &seq, &schema, &table, &op, &before, &after); err != nil {
			continue
		}

		events = append(events, map[string]any{
			"id":              id,
			"sequence_number": seq,
			"schema_name":     schema,
			"table_name":      table,
			"operation":       op,
			"before":          json.RawMessage(before),
			"after":           json.RawMessage(after),
		})
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"id":               tx.ID,
		"xid":              xid,
		"source_id":        tx.SourceID,
		"commit_end_lsn":   tx.CommitLSN,
		"commit_timestamp": tx.CommitTimestamp,
		"event_count":      tx.EventCount,
		"events":           events,
	})
}

// handleStats returns platform-wide metrics for the dashboard overview.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var stats struct {
		Sources         int     `json:"sources"`
		EventsPerSecond float64 `json:"eventsPerSecond"`
		CaptureLag      string  `json:"captureLag"`
		Consumers       int     `json:"consumers"`
		DLQDepth        int     `json:"dlqDepth"`
	}

	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM sources`).Scan(&stats.Sources)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM consumer_groups`).Scan(&stats.Consumers)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM dead_letter_events WHERE status = 'pending'`).Scan(&stats.DLQDepth)

	var lastMinute int
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE created_at > now() - interval '1 minute'`).Scan(&lastMinute)
	stats.EventsPerSecond = float64(lastMinute) / 60.0

	var lagSeconds *float64
	_ = s.pool.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM (now() - max(created_at))) FROM events`).Scan(&lagSeconds)
	if lagSeconds == nil {
		stats.CaptureLag = "n/a"
	} else {
		stats.CaptureLag = fmt.Sprintf("%.1fs", *lagSeconds)
	}

	s.writeJSON(w, http.StatusOK, stats)
}

// handleListDLQ lists dead-letter events for the dashboard.
func (s *Server) handleListDLQ(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT d.id, encode(d.event_id, 'hex'), d.source_id,
		       COALESCE(d.sink_id::text, ''), d.failure_reason, d.status, d.created_at
		FROM dead_letter_events d
		ORDER BY d.created_at DESC
		LIMIT 100
	`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()

	entries := []map[string]any{}
	for rows.Next() {
		var id int64
		var eventID, sourceID, sinkID, reason, status string
		var createdAt time.Time
		if err := rows.Scan(&id, &eventID, &sourceID, &sinkID, &reason, &status, &createdAt); err != nil {
			continue
		}
		entries = append(entries, map[string]any{
			"id":             id,
			"event_id":       eventID,
			"source_id":      sourceID,
			"sink_id":        sinkID,
			"failure_reason": reason,
			"status":         status,
			"created_at":     createdAt,
		})
	}

	s.writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

// validateSource checks that a source connection is valid and replication-ready.
func (s *Server) validateSource(ctx context.Context, connStr string) error {
	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(connCtx, connStr)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(context.Background())

	var walLevel string
	if err := conn.QueryRow(connCtx, "SHOW wal_level").Scan(&walLevel); err != nil {
		return fmt.Errorf("read wal_level: %w", err)
	}
	if walLevel != "logical" {
		return fmt.Errorf("wal_level is %q, must be 'logical'", walLevel)
	}

	var slots string
	if err := conn.QueryRow(connCtx, "SHOW max_replication_slots").Scan(&slots); err != nil {
		return fmt.Errorf("read max_replication_slots: %w", err)
	}
	if slots == "0" {
		return fmt.Errorf("max_replication_slots is 0")
	}

	return nil
}

// Helper methods
func (s *Server) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{
		"error":   http.StatusText(status),
		"message": message,
	})
}
