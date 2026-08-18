package replay

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tyagiquamar/relaydb/internal/persistence"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

// Session represents a replay session.
type Session struct {
	ID               string
	Name             string
	SourceID         string
	StartTimestamp   *time.Time
	StartLSN         *uint64
	StartEventID     []byte
	EndTimestamp     *time.Time
	EndLSN           *uint64
	EndEventID       []byte
	SchemaFilter     string
	TableFilter      string
	OperationFilter  string
	DestinationType  string
	DestinationConfig map[string]any
	Status           string
	EventsProcessed  int64
	EventsTotal      *int64
	LastProcessedLSN *uint64
	ErrorMessage     string
	CreatedAt        time.Time
	StartedAt        *time.Time
	CompletedAt      *time.Time
}

// Service manages replay sessions.
type Service struct {
	pool   *persistence.Pool
	logger *slog.Logger
}

// NewService creates a replay service.
func NewService(pool *persistence.Pool) *Service {
	return &Service{
		pool:   pool,
		logger: telemetry.With("service", "replay"),
	}
}

// Create creates a new replay session.
func (s *Service) Create(ctx context.Context, session *Session) error {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO replay_sessions (
			name, source_id, start_timestamp, start_lsn, start_event_id,
			end_timestamp, end_lsn, end_event_id,
			schema_filter, table_filter, operation_filter,
			destination_type, destination_config, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 'pending')
		RETURNING id, created_at
	`, session.Name, session.SourceID, session.StartTimestamp, session.StartLSN, session.StartEventID,
		session.EndTimestamp, session.EndLSN, session.EndEventID,
		session.SchemaFilter, session.TableFilter, session.OperationFilter,
		session.DestinationType, session.DestinationConfig).Scan(&session.ID, &session.CreatedAt)
	return err
}

// Start begins replay processing.
func (s *Service) Start(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE replay_sessions
		SET status = 'running', started_at = now()
		WHERE id = $1 AND status = 'pending'
	`, sessionID)
	return err
}

// Cancel cancels a running replay.
func (s *Service) Cancel(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE replay_sessions
		SET status = 'cancelled', completed_at = now()
		WHERE id = $1 AND status IN ('pending', 'running', 'paused')
	`, sessionID)
	return err
}

// Get retrieves a session.
func (s *Service) Get(ctx context.Context, sessionID string) (*Session, error) {
	var sess Session
	var startLSN, endLSN, lastLSN *string
	var startTs, endTs, startedAt, completedAt *time.Time

	err := s.pool.QueryRow(ctx, `
		SELECT id, name, source_id, 
		       start_timestamp, start_lsn::text, start_event_id,
		       end_timestamp, end_lsn::text, end_event_id,
		       schema_filter, table_filter, operation_filter,
		       destination_type, destination_config,
		       status, events_processed, events_total, last_processed_lsn::text,
		       error_message, created_at, started_at, completed_at
		FROM replay_sessions WHERE id = $1
	`, sessionID).Scan(
		&sess.ID, &sess.Name, &sess.SourceID,
		&startTs, &startLSN, &sess.StartEventID,
		&endTs, &endLSN, &sess.EndEventID,
		&sess.SchemaFilter, &sess.TableFilter, &sess.OperationFilter,
		&sess.DestinationType, &sess.DestinationConfig,
		&sess.Status, &sess.EventsProcessed, &sess.EventsTotal, &lastLSN,
		&sess.ErrorMessage, &sess.CreatedAt, &startedAt, &completedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("session not found")
	}

	sess.StartTimestamp = startTs
	sess.EndTimestamp = endTs
	sess.StartedAt = startedAt
	sess.CompletedAt = completedAt

	// Parse LSNs
	if startLSN != nil {
		var lsn uint64
		fmt.Sscanf(*startLSN, "%X", &lsn)
		sess.StartLSN = &lsn
	}
	if endLSN != nil {
		var lsn uint64
		fmt.Sscanf(*endLSN, "%X", &lsn)
		sess.EndLSN = &lsn
	}
	if lastLSN != nil {
		var lsn uint64
		fmt.Sscanf(*lastLSN, "%X", &lsn)
		sess.LastProcessedLSN = &lsn
	}

	return &sess, err
}

// List retrieves sessions for a source.
func (s *Service) List(ctx context.Context, sourceID string) ([]*Session, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, source_id, status, events_processed, created_at
		FROM replay_sessions
		WHERE source_id = $1
		ORDER BY created_at DESC
	`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Name, &s.SourceID, &s.Status, &s.EventsProcessed, &s.CreatedAt); err != nil {
			continue
		}
		sessions = append(sessions, &s)
	}
	return sessions, nil
}