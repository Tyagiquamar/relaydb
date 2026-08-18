package dlq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tyagiquamar/relaydb/internal/persistence"
)

// Entry represents a dead-lettered event.
type Entry struct {
	ID              int64
	EventID         []byte
	SourceID        string
	SinkID          *string
	ConsumerGroupID *string
	FailureReason   string
	AttemptHistory  []Attempt
	Status          string
	CreatedAt       time.Time
	ResolvedAt      *time.Time
}

// Attempt records a delivery attempt.
type Attempt struct {
	Number    int       `json:"number"`
	Timestamp time.Time `json:"timestamp"`
	Error     string    `json:"error"`
	Duration  string    `json:"duration"`
}

// Store manages the dead-letter queue.
type Store struct {
	pool *persistence.Pool
}

// NewStore creates a DLQ store.
func NewStore(pool *persistence.Pool) *Store {
	return &Store{pool: pool}
}

// Add adds an event to the DLQ.
func (s *Store) Add(ctx context.Context, entry *Entry) error {
	historyJSON, _ := json.Marshal(entry.AttemptHistory)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO dead_letter_events (event_id, source_id, sink_id, consumer_group_id,
		                               failure_reason, attempt_history, status)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending')
		ON CONFLICT (event_id, sink_id, consumer_group_id) DO UPDATE
		SET failure_reason = $5, attempt_history = $6, status = 'pending'
	`, entry.EventID, entry.SourceID, entry.SinkID, entry.ConsumerGroupID,
		entry.FailureReason, historyJSON)
	return err
}

// Get retrieves a DLQ entry.
func (s *Store) Get(ctx context.Context, id int64) (*Entry, error) {
	var e Entry
	var historyJSON []byte
	var sinkID, groupID *string

	err := s.pool.QueryRow(ctx, `
		SELECT id, event_id, source_id, sink_id, consumer_group_id,
		       failure_reason, attempt_history, status, created_at, resolved_at
		FROM dead_letter_events WHERE id = $1
	`, id).Scan(&e.ID, &e.EventID, &e.SourceID, &sinkID, &groupID,
		&e.FailureReason, &historyJSON, &e.Status, &e.CreatedAt, &e.ResolvedAt)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("entry not found")
	}

	e.SinkID = sinkID
	e.ConsumerGroupID = groupID
	json.Unmarshal(historyJSON, &e.AttemptHistory)

	return &e, err
}

// List retrieves pending DLQ entries.
func (s *Store) List(ctx context.Context, status string, limit int) ([]*Entry, error) {
	query := "SELECT id, event_id, source_id, failure_reason, status, created_at FROM dead_letter_events"
	args := []any{}

	if status != "" {
		query += " WHERE status = $1"
		args = append(args, status)
	}

	query += " ORDER BY created_at DESC"
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.EventID, &e.SourceID, &e.FailureReason, &e.Status, &e.CreatedAt); err != nil {
			continue
		}
		entries = append(entries, &e)
	}
	return entries, nil
}

// MarkRetried marks an entry as retried.
func (s *Store) MarkRetried(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE dead_letter_events SET status = 'retried', resolved_at = now()
		WHERE id = $1
	`, id)
	return err
}

// MarkDiscarded marks an entry as discarded.
func (s *Store) MarkDiscarded(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE dead_letter_events SET status = 'discarded', resolved_at = now()
		WHERE id = $1
	`, id)
	return err
}