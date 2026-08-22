package lease

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tyagiquamar/relaydb/internal/persistence"
)

// Lease represents a partition lease with fencing token.
type Lease struct {
	GroupID    string
	Partition  int
	Owner      string
	Generation int64
	ExpiresAt  time.Time
}

// Manager handles partition leases.
type Manager struct {
	pool *persistence.Pool
}

// NewManager creates a lease manager.
func NewManager(pool *persistence.Pool) *Manager {
	return &Manager{pool: pool}
}

// Claim attempts to claim a partition lease.
// Uses FOR UPDATE SKIP LOCKED for atomic claiming.
func (m *Manager) Claim(ctx context.Context, groupID string, partition int, owner string, duration time.Duration) (*Lease, error) {
	var lease Lease

	err := persistence.WithTx(ctx, m.pool, func(tx pgx.Tx) error {
		// Try to claim or renew
		err := tx.QueryRow(ctx, `
			INSERT INTO partition_leases (group_id, partition, lease_owner, lease_generation, lease_expires_at)
			VALUES ($1, $2, $3, 1, now() + $4::interval)
			ON CONFLICT (group_id, partition) DO UPDATE
			SET lease_owner = $3,
			    lease_generation = partition_leases.lease_generation + 1,
			    lease_expires_at = now() + $4::interval,
			    heartbeat_at = now()
			WHERE partition_leases.lease_expires_at < now()
			   OR partition_leases.lease_owner = $3
			RETURNING group_id, partition, lease_owner, lease_generation, lease_expires_at
		`, groupID, partition, owner, duration).Scan(
			&lease.GroupID, &lease.Partition, &lease.Owner, &lease.Generation, &lease.ExpiresAt,
		)

		if err == pgx.ErrNoRows {
			return fmt.Errorf("partition %d is held by another member", partition)
		}
		return err
	})

	if err != nil {
		return nil, err
	}
	return &lease, nil
}

// Heartbeat renews a lease.
func (m *Manager) Heartbeat(ctx context.Context, lease *Lease, duration time.Duration) error {
	err := m.pool.QueryRow(ctx, `
		UPDATE partition_leases
		SET lease_expires_at = now() + $4::interval,
		    heartbeat_at = now()
		WHERE group_id = $1
		  AND partition = $2
		  AND lease_owner = $3
		  AND lease_generation = $5
		  AND lease_expires_at > now()
		RETURNING lease_expires_at
	`, lease.GroupID, lease.Partition, lease.Owner, duration, lease.Generation).Scan(&lease.ExpiresAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("fencing violation: lease expired or stolen")
		}
		return fmt.Errorf("heartbeat: %w", err)
	}
	return nil
}

// Release releases a lease.
func (m *Manager) Release(ctx context.Context, lease *Lease) error {
	_, err := m.pool.Exec(ctx, `
		DELETE FROM partition_leases
		WHERE group_id = $1 AND partition = $2 AND lease_owner = $3 AND lease_generation = $4
	`, lease.GroupID, lease.Partition, lease.Owner, lease.Generation)
	return err
}

// Validate checks if a lease is still valid for fencing.
func (m *Manager) Validate(ctx context.Context, groupID string, partition int, owner string, generation int64) (bool, error) {
	var valid bool
	err := m.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM partition_leases
			WHERE group_id = $1 AND partition = $2 
			  AND lease_owner = $3 AND lease_generation = $4
			  AND lease_expires_at > now()
		)
	`, groupID, partition, owner, generation).Scan(&valid)
	return valid, err
}

// ValidateForUpdate is the transaction-aware form of Validate: it must be run
// inside an open transaction. It checks that the partition lease is currently
// held by owner/generation and takes FOR UPDATE on the lease row until that
// transaction commits or rolls back. A concurrent takeover therefore cannot
// interleave between this check and any dependent write performed later in
// the same transaction (fencing TOCTOU fix).
func (m *Manager) ValidateForUpdate(ctx context.Context, tx pgx.Tx, groupID string, partition int, owner string, generation int64) (bool, error) {
	var valid bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM partition_leases
			WHERE group_id = $1 AND partition = $2
			  AND lease_owner = $3 AND lease_generation = $4
			  AND lease_expires_at > now()
			FOR UPDATE
		)
	`, groupID, partition, owner, generation).Scan(&valid)
	if err != nil {
		return false, fmt.Errorf("lock lease: %w", err)
	}
	return valid, nil
}
