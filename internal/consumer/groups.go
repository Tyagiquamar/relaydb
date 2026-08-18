package consumer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tyagiquamar/relaydb/internal/eventstore"
	"github.com/tyagiquamar/relaydb/internal/lease"
	"github.com/tyagiquamar/relaydb/internal/partition"
	"github.com/tyagiquamar/relaydb/internal/persistence"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

// Group represents a consumer group.
type Group struct {
	ID               string
	ConsumerID       string
	SourceID         string
	Name             string
	PartitionCount   int
	HashVersion      int
	PoisonPolicy     string // dlq or halt
	MaxAttempts      int
	CreatedAt        time.Time
}

// Offset represents a consumer offset.
type Offset struct {
	GroupID        string
	Partition      int
	CommitEndLSN   uint64
	SequenceNumber int
	LastEventID    []byte
	UpdatedAt      time.Time
}

// Service handles consumer operations.
type Service struct {
	pool      *persistence.Pool
	leaseMgr  *lease.Manager
	logger    *slog.Logger
}

// NewService creates a consumer service.
func NewService(pool *persistence.Pool) *Service {
	return &Service{
		pool:     pool,
		leaseMgr: lease.NewManager(pool),
		logger:   telemetry.With("service", "consumer"),
	}
}

// CreateGroup creates a consumer group.
func (s *Service) CreateGroup(ctx context.Context, consumerID, sourceID, name string, partitionCount int) (*Group, error) {
	var group Group
	err := s.pool.QueryRow(ctx, `
		INSERT INTO consumer_groups (consumer_id, source_id, name, partition_count, partition_hash_version)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, consumer_id, source_id, name, partition_count, partition_hash_version, 
		          poison_event_policy, max_attempts, created_at
	`, consumerID, sourceID, name, partitionCount, partition.HashVersion).Scan(
		&group.ID, &group.ConsumerID, &group.SourceID, &group.Name,
		&group.PartitionCount, &group.HashVersion,
		&group.PoisonPolicy, &group.MaxAttempts, &group.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}
	return &group, nil
}

// Poll requests events for a consumer member.
func (s *Service) Poll(ctx context.Context, groupID string, memberID string, maxEvents int, maxWait time.Duration) ([]*eventstore.Event, *lease.Lease, error) {
	// Get group info
	group, err := s.GetGroup(ctx, groupID)
	if err != nil {
		return nil, nil, err
	}

	// Try to claim partitions
	assigned, err := s.claimPartitions(ctx, group, memberID)
	if err != nil {
		return nil, nil, err
	}
	if len(assigned) == 0 {
		return nil, nil, fmt.Errorf("no partitions available")
	}

	// For now, use first assigned partition
	// TODO: Proper round-robin across assigned partitions
	partition := assigned[0]

	// Get current offset
	offset, err := s.GetOffset(ctx, groupID, partition)
	if err != nil {
		return nil, nil, err
	}

	// Poll for events
	events, err := s.pollEvents(ctx, group.SourceID, partition, offset, maxEvents, maxWait)
	if err != nil {
		return nil, nil, err
	}

	// Return lease for ACK validation
	leaseObj := &lease.Lease{
		GroupID:   groupID,
		Partition: partition,
		Owner:     memberID,
	}

	return events, leaseObj, nil
}

// Ack acknowledges events up to a position.
func (s *Service) Ack(ctx context.Context, groupID string, partition int, owner string, generation int64, commitLSN uint64, sequence int, lastEventID []byte) error {
	// Validate lease
	valid, err := s.leaseMgr.Validate(ctx, groupID, partition, owner, generation)
	if err != nil {
		return fmt.Errorf("validate lease: %w", err)
	}
	if !valid {
		return fmt.Errorf("stale lease: generation mismatch or expired")
	}

	// Update offset with fencing
	result, err := s.pool.Exec(ctx, `
		INSERT INTO consumer_offsets (group_id, partition, commit_end_lsn, sequence_number, last_event_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (group_id, partition) DO UPDATE
		SET commit_end_lsn = $3,
		    sequence_number = $4,
		    last_event_id = $5,
		    updated_at = now()
		WHERE consumer_offsets.commit_end_lsn < $3
		   OR (consumer_offsets.commit_end_lsn = $3 AND consumer_offsets.sequence_number < $4)
	`, groupID, partition, commitLSN, sequence, lastEventID)

	if err != nil {
		return fmt.Errorf("ack: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("offset regression rejected")
	}

	return nil
}

// Nack negatively acknowledges events for redelivery.
func (s *Service) Nack(ctx context.Context, groupID string, partition int, owner string, generation int64, eventIDs [][]byte, retryAfter time.Duration) error {
	// Validate lease
	valid, err := s.leaseMgr.Validate(ctx, groupID, partition, owner, generation)
	if err != nil {
		return fmt.Errorf("validate lease: %w", err)
	}
	if !valid {
		return fmt.Errorf("stale lease")
	}

	// TODO: Track NACK attempts for poison detection
	// For now, just log
	s.logger.Info("nack received", 
		"group", groupID, 
		"partition", partition, 
		"events", len(eventIDs),
		"retry_after", retryAfter,
	)

	return nil
}

// GetGroup retrieves a group by ID.
func (s *Service) GetGroup(ctx context.Context, groupID string) (*Group, error) {
	var group Group
	err := s.pool.QueryRow(ctx, `
		SELECT id, consumer_id, source_id, name, partition_count, partition_hash_version,
		       poison_event_policy, max_attempts, created_at
		FROM consumer_groups WHERE id = $1
	`, groupID).Scan(
		&group.ID, &group.ConsumerID, &group.SourceID, &group.Name,
		&group.PartitionCount, &group.HashVersion,
		&group.PoisonPolicy, &group.MaxAttempts, &group.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("group not found")
	}
	return &group, err
}

// GetOffset retrieves the current offset for a partition.
func (s *Service) GetOffset(ctx context.Context, groupID string, partition int) (*Offset, error) {
	var offset Offset
	var lsnStr string

	err := s.pool.QueryRow(ctx, `
		SELECT group_id, partition, commit_end_lsn::text, sequence_number, last_event_id, updated_at
		FROM consumer_offsets
		WHERE group_id = $1 AND partition = $2
	`, groupID, partition).Scan(
		&offset.GroupID, &offset.Partition, &lsnStr,
		&offset.SequenceNumber, &offset.LastEventID, &offset.UpdatedAt,
	)

	if err == pgx.ErrNoRows {
		// Start from beginning
		return &Offset{GroupID: groupID, Partition: partition}, nil
	}
	return &offset, err
}

// claimPartitions attempts to claim partitions for a member.
func (s *Service) claimPartitions(ctx context.Context, group *Group, memberID string) ([]int, error) {
	// Get all partitions for this group
	rows, err := s.pool.Query(ctx, `
		SELECT partition, lease_owner, lease_expires_at > now() as active
		FROM partition_leases
		WHERE group_id = $1
		ORDER BY partition
	`, group.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	leased := make(map[int]bool)
	for rows.Next() {
		var p int
		var owner string
		var active bool
		if err := rows.Scan(&p, &owner, &active); err != nil {
			continue
		}
		if active {
			leased[p] = true
		}
	}

	// Find unclaimed partitions
	var unclaimed []int
	for p := 0; p < group.PartitionCount; p++ {
		if !leased[p] {
			unclaimed = append(unclaimed, p)
		}
	}

	// Try to claim one
	for _, p := range unclaimed {
		leaseObj, err := s.leaseMgr.Claim(ctx, group.ID, p, memberID, 30*time.Second)
		if err == nil {
			s.logger.Info("claimed partition",
				"group", group.ID,
				"partition", p,
				"owner", memberID,
				"generation", leaseObj.Generation,
			)
			return []int{p}, nil
		}
	}

	return nil, nil
}

// pollEvents reads events for a partition after the offset.
func (s *Service) pollEvents(ctx context.Context, sourceID string, partitionNum int, offset *Offset, maxEvents int, maxWait time.Duration) ([]*eventstore.Event, error) {
	hasher := partition.NewHasher(16) // TODO: Get from group config

	// For now, read all events after offset and filter by partition
	// TODO: Optimize with partition-aware query
	query := `
		SELECT id, source_id, transaction_id, commit_end_lsn, sequence_number,
		       schema_name, table_name, operation, before, after, key_columns,
		       payload_hash, created_at
		FROM events
		WHERE source_id = $1
	`
	args := []any{sourceID}
	argIdx := 2

	if offset.CommitEndLSN > 0 {
		query += fmt.Sprintf(" AND (commit_end_lsn > $%d OR (commit_end_lsn = $%d AND sequence_number > $%d))",
			argIdx, argIdx, argIdx+1)
		args = append(args, offset.CommitEndLSN, offset.SequenceNumber)
		argIdx += 2
	}

	query += fmt.Sprintf(" ORDER BY commit_end_lsn, sequence_number LIMIT $%d", argIdx)
	args = append(args, maxEvents*2) // Over-fetch for partition filter

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*eventstore.Event
	for rows.Next() {
		var e eventstore.Event
		var before, after, key []byte
		var lsn uint64

		if err := rows.Scan(&e.ID, &e.SourceID, &e.TransactionID, &lsn, &e.SequenceNumber,
			&e.SchemaName, &e.TableName, &e.Operation, &before, &after, &key,
			&e.PayloadHash, &e.CreatedAt); err != nil {
			continue
		}

		e.CommitEndLSN = lsn

		// Filter by partition
		keyStr := partition.NormalizeKey(e.Key)
		if hasher.Partition(keyStr) == partitionNum {
			events = append(events, &e)
			if len(events) >= maxEvents {
				break
			}
		}
	}

	return events, nil
}