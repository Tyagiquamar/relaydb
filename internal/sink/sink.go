package sink

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tyagiquamar/relaydb/internal/persistence"
)

// Sink represents a webhook sink.
type Sink struct {
	ID          string
	Name        string
	Description string
	URL         string
	SourceID    *string
	SchemaFilter string
	TableFilter  string
	OperationFilter string
	MaxAttempts int
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Registry manages webhook sinks.
type Registry struct {
	pool *persistence.Pool
}

// NewRegistry creates a sink registry.
func NewRegistry(pool *persistence.Pool) *Registry {
	return &Registry{pool: pool}
}

// Create creates a sink.
func (r *Registry) Create(ctx context.Context, sink *Sink, secretEncrypted []byte) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO webhook_sinks (name, description, url, secret_encrypted, source_id,
		                          schema_filter, table_filter, operation_filter, max_attempts)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at
	`, sink.Name, sink.Description, sink.URL, secretEncrypted, sink.SourceID,
		sink.SchemaFilter, sink.TableFilter, sink.OperationFilter, sink.MaxAttempts).Scan(
		&sink.ID, &sink.CreatedAt)
	return err
}

// Get retrieves a sink by ID.
func (r *Registry) Get(ctx context.Context, id string) (*Sink, error) {
	var s Sink
	var sourceID *string

	err := r.pool.QueryRow(ctx, `
		SELECT id, name, description, url, source_id, schema_filter, table_filter,
		       operation_filter, max_attempts, enabled, created_at, updated_at
		FROM webhook_sinks WHERE id = $1
	`, id).Scan(&s.ID, &s.Name, &s.Description, &s.URL, &sourceID,
		&s.SchemaFilter, &s.TableFilter, &s.OperationFilter, &s.MaxAttempts,
		&s.Enabled, &s.CreatedAt, &s.UpdatedAt)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("sink not found")
	}

	s.SourceID = sourceID
	return &s, err
}

// List retrieves all enabled sinks.
func (r *Registry) List(ctx context.Context) ([]*Sink, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, description, url, source_id, schema_filter, table_filter,
		       operation_filter, max_attempts, enabled, created_at
		FROM webhook_sinks WHERE enabled = true
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sinks []*Sink
	for rows.Next() {
		var s Sink
		var sourceID *string
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.URL, &sourceID,
			&s.SchemaFilter, &s.TableFilter, &s.OperationFilter, &s.MaxAttempts,
			&s.Enabled, &s.CreatedAt); err != nil {
			continue
		}
		s.SourceID = sourceID
		sinks = append(sinks, &s)
	}
	return sinks, nil
}

// Matches checks if an event matches the sink's filters.
func (s *Sink) Matches(sourceID, schema, table, operation string) bool {
	if s.SourceID != nil && *s.SourceID != sourceID {
		return false
	}
	if s.SchemaFilter != "" && s.SchemaFilter != schema {
		return false
	}
	if s.TableFilter != "" && s.TableFilter != table {
		return false
	}
	if s.OperationFilter != "" {
		// Comma-separated list
		ops := splitAndTrim(s.OperationFilter, ",")
		found := false
		for _, op := range ops {
			if op == operation {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}