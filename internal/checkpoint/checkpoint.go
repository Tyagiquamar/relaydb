package checkpoint

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tyagiquamar/relaydb/internal/persistence"
	"github.com/tyagiquamar/relaydb/internal/replication"
)

// Manager handles LSN checkpoint state with fencing.
// This is the source of truth for what has been durably persisted.
type Manager struct {
	pool *persistence.Pool
}

// NewManager creates a checkpoint manager.
func NewManager(pool *persistence.Pool) *Manager {
	return &Manager{pool: pool}
}

// Checkpoint represents the durable state of a source.
type Checkpoint struct {
	SourceID        string
	ReceivedLSN     replication.LSN
	PersistedLSN    replication.LSN
	AcknowledgedLSN replication.LSN
	CaptureOwner    string
	OwnerGeneration int64
	LeaseExpiresAt  time.Time
	UpdatedAt       time.Time
}

// Get retrieves the checkpoint for a source.
func (m *Manager) Get(ctx context.Context, sourceID string) (*Checkpoint, error) {
	var cp Checkpoint
	var received, persisted, acked string

	err := m.pool.QueryRow(ctx, `
		SELECT source_id, received_lsn::text, persisted_lsn::text, acknowledged_lsn::text,
		       capture_owner, owner_generation, lease_expires_at, updated_at
		FROM source_checkpoints
		WHERE source_id = $1
	`, sourceID).Scan(
		&cp.SourceID, &received, &persisted, &acked,
		&cp.CaptureOwner, &cp.OwnerGeneration, &cp.LeaseExpiresAt, &cp.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query checkpoint: %w", err)
	}

	cp.ReceivedLSN, _ = replication.ParseLSN(received)
	cp.PersistedLSN, _ = replication.ParseLSN(persisted)
	cp.AcknowledgedLSN, _ = replication.ParseLSN(acked)

	return &cp, nil
}

// Claim attempts to claim ownership of a source.
// Returns the new generation if successful, or an error if another owner holds it.
func (m *Manager) Claim(ctx context.Context, sourceID, owner string, leaseDuration time.Duration) (int64, error) {
	var generation int64

	err := persistence.WithTx(ctx, m.pool, func(tx pgx.Tx) error {
		// Try to claim with fencing
		err := tx.QueryRow(ctx, `
			INSERT INTO source_checkpoints (source_id, capture_owner, owner_generation, lease_expires_at)
			VALUES ($1, $2, 1, now() + $3::interval)
			ON CONFLICT (source_id) DO UPDATE
			SET capture_owner = $2,
			    owner_generation = source_checkpoints.owner_generation + 1,
			    lease_expires_at = now() + $3::interval
			WHERE source_checkpoints.lease_expires_at < now()
			   OR source_checkpoints.capture_owner = $2
			RETURNING owner_generation
		`, sourceID, owner, leaseDuration).Scan(&generation)

		if err == pgx.ErrNoRows {
			return fmt.Errorf("source %s is owned by another capture instance", sourceID)
		}
		return err
	})

	if err != nil {
		return 0, err
	}

	return generation, nil
}

// Heartbeat renews the lease for the current owner.
func (m *Manager) Heartbeat(ctx context.Context, sourceID, owner string, generation int64, leaseDuration time.Duration) error {
	result, err := m.pool.Exec(ctx, `
		UPDATE source_checkpoints
		SET lease_expires_at = now() + $4::interval,
		    updated_at = now()
		WHERE source_id = $1
		  AND capture_owner = $2
		  AND owner_generation = $3
		  AND lease_expires_at > now()
	`, sourceID, owner, generation, leaseDuration)

	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("fencing violation: owner=%s generation=%d not current", owner, generation)
	}

	return nil
}

// UpdatePersisted advances the persisted LSN with fencing.
// This is called after events are durably committed.
func (m *Manager) UpdatePersisted(ctx context.Context, sourceID, owner string, generation int64, lsn replication.LSN) error {
	result, err := m.pool.Exec(ctx, `
		UPDATE source_checkpoints
		SET persisted_lsn = $4,
		    updated_at = now()
		WHERE source_id = $1
		  AND capture_owner = $2
		  AND owner_generation = $3
		  AND lease_expires_at > now()
		  AND persisted_lsn <= $4  -- LSN monotonicity
	`, sourceID, owner, generation, lsn.String())

	if err != nil {
		return fmt.Errorf("update persisted: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("fencing violation or LSN regression: owner=%s gen=%d lsn=%s",
			owner, generation, lsn)
	}

	return nil
}

// UpdateAcknowledged advances the acknowledged LSN.
// This is called after the source has confirmed receipt.
func (m *Manager) UpdateAcknowledged(ctx context.Context, sourceID string, lsn replication.LSN) error {
	_, err := m.pool.Exec(ctx, `
		UPDATE source_checkpoints
		SET acknowledged_lsn = $2,
		    updated_at = now()
		WHERE source_id = $1
	`, sourceID, lsn.String())

	return err
}

// UpdateReceived tracks the received LSN (for monitoring, not persisted).
func (m *Manager) UpdateReceived(ctx context.Context, sourceID string, lsn replication.LSN) error {
	_, err := m.pool.Exec(ctx, `
		UPDATE source_checkpoints
		SET received_lsn = $2,
		    updated_at = now()
		WHERE source_id = $1
	`, sourceID, lsn.String())

	return err
}

// LagMetrics computes lag between received and persisted.
type LagMetrics struct {
	ReceivedLSN     replication.LSN
	PersistedLSN    replication.LSN
	AcknowledgedLSN replication.LSN
	LagBytes        int64
	LagSeconds      float64
}

// GetLagMetrics returns current lag metrics for a source.
func (m *Manager) GetLagMetrics(ctx context.Context, sourceID string) (*LagMetrics, error) {
	cp, err := m.Get(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if cp == nil {
		return nil, fmt.Errorf("no checkpoint for source %s", sourceID)
	}

	metrics := &LagMetrics{
		ReceivedLSN:     cp.ReceivedLSN,
		PersistedLSN:    cp.PersistedLSN,
		AcknowledgedLSN: cp.AcknowledgedLSN,
		LagBytes:        int64(cp.ReceivedLSN) - int64(cp.PersistedLSN),
	}

	return metrics, nil
}
