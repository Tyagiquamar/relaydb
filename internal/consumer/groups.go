package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
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
	leaseObj, err := s.claimPartition(ctx, group, memberID)
	if err != nil {
		return nil, nil, err
	}
	if leaseObj == nil {
		return nil, nil, fmt.Errorf("no partitions available")
	}

	// Get current offset
	if err := s.ensurePartitionActive(ctx, groupID, leaseObj.Partition); err != nil {
		return nil, nil, err
	}
	offset, err := s.GetOffset(ctx, groupID, leaseObj.Partition)
	if err != nil {
		return nil, nil, err
	}

	// Poll for events
	events, err := s.pollEvents(ctx, group.SourceID, group.PartitionCount, leaseObj.Partition, offset, maxEvents, maxWait)
	if err != nil {
		return nil, nil, err
	}
	if err := s.recordIssued(ctx, groupID, leaseObj, events); err != nil {
		return nil, nil, err
	}

	return events, leaseObj, nil
}

// Heartbeat renews a consumer member's partition lease.
func (s *Service) Heartbeat(ctx context.Context, groupID string, partition int, owner string, generation int64, duration time.Duration) (*lease.Lease, error) {
	leaseObj := &lease.Lease{
		GroupID:    groupID,
		Partition:  partition,
		Owner:      owner,
		Generation: generation,
	}
	if err := s.leaseMgr.Heartbeat(ctx, leaseObj, duration); err != nil {
		return nil, err
	}
	return leaseObj, nil
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
	if len(eventIDs) != 1 {
		return fmt.Errorf("nack requires exactly one head event")
	}
	if retryAfter < 0 {
		return fmt.Errorf("retry delay must not be negative")
	}

	return persistence.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var poisonPolicy, sourceID string
		var maxAttempts int
		if err := tx.QueryRow(ctx, `
			SELECT poison_event_policy, max_attempts, source_id
			FROM consumer_groups WHERE id = $1
			FOR SHARE
		`, groupID).Scan(&poisonPolicy, &maxAttempts, &sourceID); err != nil {
			return fmt.Errorf("load consumer group: %w", err)
		}

		var valid bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM partition_leases
				WHERE group_id = $1 AND partition = $2
				  AND lease_owner = $3 AND lease_generation = $4
				  AND lease_expires_at > now()
				FOR UPDATE
			)
		`, groupID, partition, owner, generation).Scan(&valid); err != nil {
			return fmt.Errorf("lock lease: %w", err)
		}
		if !valid {
			return fmt.Errorf("stale lease")
		}

		for _, eventID := range eventIDs {
			var attempts int
			err := tx.QueryRow(ctx, `
				UPDATE consumer_delivery_attempts
				SET state = 'retry_scheduled',
				    nack_attempts = nack_attempts + 1,
				    next_delivery_at = now() + $6::interval,
				    updated_at = now()
				WHERE group_id = $1 AND partition = $2 AND event_id = $3
				  AND lease_owner = $4 AND lease_generation = $5
				  AND state = 'issued'
				RETURNING nack_attempts
			`, groupID, partition, eventID, owner, generation, retryAfter.String()).Scan(&attempts)
			if err != nil {
				if err == pgx.ErrNoRows {
					return fmt.Errorf("event was not issued to the current lease")
				}
				return fmt.Errorf("schedule redelivery: %w", err)
			}
			if attempts < maxAttempts {
				continue
			}

			switch poisonPolicy {
			case "dlq":
				if err := s.deadLetterConsumerEvent(ctx, tx, groupID, partition, eventID, sourceID, attempts); err != nil {
					return err
				}
			case "halt":
				if _, err := tx.Exec(ctx, `
					INSERT INTO consumer_partition_states (group_id, partition, status, halted_event_id, halt_reason)
					VALUES ($1, $2, 'halted', $3, $4)
					ON CONFLICT (group_id, partition) DO UPDATE
					SET status = 'halted', halted_event_id = EXCLUDED.halted_event_id,
					    halt_reason = EXCLUDED.halt_reason, updated_at = now()
				`, groupID, partition, eventID, fmt.Sprintf("poison event reached %d NACK attempts", attempts)); err != nil {
					return fmt.Errorf("halt consumer partition: %w", err)
				}
			default:
				return fmt.Errorf("unknown poison event policy %q", poisonPolicy)
			}
		}
		return nil
	})
}

func (s *Service) recordIssued(ctx context.Context, groupID string, leaseObj *lease.Lease, events []*eventstore.Event) error {
	if len(events) == 0 {
		return nil
	}

	return persistence.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		for _, event := range events {
			result, err := tx.Exec(ctx, `
				INSERT INTO consumer_delivery_attempts (
					group_id, partition, event_id, lease_owner, lease_generation, state
				) VALUES ($1, $2, $3, $4, $5, 'issued')
				ON CONFLICT (group_id, partition, event_id) DO UPDATE
				SET lease_owner = EXCLUDED.lease_owner,
				    lease_generation = EXCLUDED.lease_generation,
				    state = 'issued',
				    updated_at = now()
				WHERE consumer_delivery_attempts.state = 'issued'
				   OR consumer_delivery_attempts.next_delivery_at <= now()
			`, groupID, leaseObj.Partition, event.ID[:], leaseObj.Owner, leaseObj.Generation)
			if err != nil {
				return fmt.Errorf("record issued event: %w", err)
			}
			if result.RowsAffected() != 1 {
				return fmt.Errorf("event is not eligible for delivery")
			}
		}
		return nil
	})
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
	if err != nil {
		return nil, err
	}

	offset.CommitEndLSN, err = parseLSN(lsnStr)
	if err != nil {
		return nil, fmt.Errorf("parse consumer offset LSN: %w", err)
	}
	return &offset, nil
}

func parseLSN(value string) (uint64, error) {
	high, low, ok := strings.Cut(value, "/")
	if !ok || high == "" || low == "" {
		return 0, fmt.Errorf("invalid LSN %q", value)
	}

	highValue, err := strconv.ParseUint(high, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("parse LSN high value: %w", err)
	}
	lowValue, err := strconv.ParseUint(low, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("parse LSN low value: %w", err)
	}
	return highValue<<32 | lowValue, nil
}

func (s *Service) ensurePartitionActive(ctx context.Context, groupID string, partition int) error {
	var status string
	err := s.pool.QueryRow(ctx, `
		SELECT status FROM consumer_partition_states
		WHERE group_id = $1 AND partition = $2
	`, groupID, partition).Scan(&status)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get consumer partition state: %w", err)
	}
	if status == "halted" {
		return fmt.Errorf("consumer partition is halted by poison event policy")
	}
	return nil
}

// claimPartition attempts to claim one partition for a member.
func (s *Service) claimPartition(ctx context.Context, group *Group, memberID string) (*lease.Lease, error) {
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
			return leaseObj, nil
		}
	}

	return nil, nil
}

// pollEvents reads events for a partition after the offset.
func (s *Service) pollEvents(ctx context.Context, sourceID string, partitionCount, partitionNum int, offset *Offset, maxEvents int, maxWait time.Duration) ([]*eventstore.Event, error) {
	hasher := partition.NewHasher(partitionCount)

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
		if err := json.Unmarshal(key, &e.Key); err != nil {
			return nil, fmt.Errorf("decode event key: %w", err)
		}

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

func (s *Service) deadLetterConsumerEvent(ctx context.Context, tx pgx.Tx, groupID string, partition int, eventID []byte, sourceID string, attempts int) error {
	var lsn string
	var sequence int
	if err := tx.QueryRow(ctx, `
		SELECT commit_end_lsn::text, sequence_number
		FROM events WHERE id = $1
	`, eventID).Scan(&lsn, &sequence); err != nil {
		return fmt.Errorf("load poison event: %w", err)
	}

	history, err := json.Marshal([]map[string]any{{
		"attempt": attempts,
		"timestamp": time.Now().UTC(),
		"error": "consumer NACK attempt limit reached",
	}})
	if err != nil {
		return fmt.Errorf("encode poison attempt history: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO dead_letter_events (event_id, source_id, consumer_group_id, failure_reason, attempt_history)
		VALUES ($1, $2, $3, $4, $5)
	`, eventID, sourceID, groupID,
		fmt.Sprintf("consumer poison event reached %d NACK attempts", attempts), history); err != nil {
		return fmt.Errorf("insert consumer dead letter: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE consumer_delivery_attempts
		SET state = 'dead_lettered', updated_at = now()
		WHERE group_id = $1 AND partition = $2 AND event_id = $3
	`, groupID, partition, eventID); err != nil {
		return fmt.Errorf("mark consumer dead letter: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO consumer_offsets (group_id, partition, commit_end_lsn, sequence_number, last_event_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (group_id, partition) DO UPDATE
		SET commit_end_lsn = EXCLUDED.commit_end_lsn,
		    sequence_number = EXCLUDED.sequence_number,
		    last_event_id = EXCLUDED.last_event_id,
		    updated_at = now()
		WHERE consumer_offsets.commit_end_lsn < EXCLUDED.commit_end_lsn
		   OR (consumer_offsets.commit_end_lsn = EXCLUDED.commit_end_lsn
		       AND consumer_offsets.sequence_number < EXCLUDED.sequence_number)
	`, groupID, partition, lsn, sequence, eventID); err != nil {
		return fmt.Errorf("advance offset past consumer dead letter: %w", err)
	}
	return nil
}