package consumer

import (
	"context"
	"fmt"

	"github.com/tyagiquamar/relaydb/internal/persistence"
)

// OffsetManager handles consumer offset persistence.
type OffsetManager struct {
	pool *persistence.Pool
}

// NewOffsetManager creates an offset manager.
func NewOffsetManager(pool *persistence.Pool) *OffsetManager {
	return &OffsetManager{pool: pool}
}

// Get retrieves an offset.
func (m *OffsetManager) Get(ctx context.Context, groupID string, partition int) (*Offset, error) {
	var offset Offset
	var lsnStr string

	err := m.pool.QueryRow(ctx, `
		SELECT group_id, partition, commit_end_lsn::text, sequence_number, last_event_id, updated_at
		FROM consumer_offsets
		WHERE group_id = $1 AND partition = $2
	`, groupID, partition).Scan(
		&offset.GroupID, &offset.Partition, &lsnStr,
		&offset.SequenceNumber, &offset.LastEventID, &offset.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	// Parse LSN
	var commitLSN uint64
	fmt.Sscanf(lsnStr, "%X", &commitLSN)
	offset.CommitEndLSN = commitLSN

	return &offset, nil
}

// Set updates an offset.
func (m *OffsetManager) Set(ctx context.Context, offset *Offset) error {
	_, err := m.pool.Exec(ctx, `
		INSERT INTO consumer_offsets (group_id, partition, commit_end_lsn, sequence_number, last_event_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (group_id, partition) DO UPDATE
		SET commit_end_lsn = $3,
		    sequence_number = $4,
		    last_event_id = $5,
		    updated_at = now()
	`, offset.GroupID, offset.Partition, 
	   fmt.Sprintf("%X", offset.CommitEndLSN), offset.SequenceNumber, offset.LastEventID)
	return err
}

// List retrieves all offsets for a group.
func (m *OffsetManager) List(ctx context.Context, groupID string) ([]*Offset, error) {
	rows, err := m.pool.Query(ctx, `
		SELECT group_id, partition, commit_end_lsn::text, sequence_number, last_event_id, updated_at
		FROM consumer_offsets
		WHERE group_id = $1
		ORDER BY partition
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var offsets []*Offset
	for rows.Next() {
		var offset Offset
		var lsnStr string
		if err := rows.Scan(&offset.GroupID, &offset.Partition, &lsnStr,
			&offset.SequenceNumber, &offset.LastEventID, &offset.UpdatedAt); err != nil {
			continue
		}
		fmt.Sscanf(lsnStr, "%X", &offset.CommitEndLSN)
		offsets = append(offsets, &offset)
	}
	return offsets, nil
}