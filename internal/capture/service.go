package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/tyagiquamar/relaydb/internal/checkpoint"
	"github.com/tyagiquamar/relaydb/internal/config"
	"github.com/tyagiquamar/relaydb/internal/eventstore"
	"github.com/tyagiquamar/relaydb/internal/persistence"
	"github.com/tyagiquamar/relaydb/internal/replication"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

// Service is the capture service that owns a source and processes WAL.
type Service struct {
	cfg        config.Config
	pool       *persistence.Pool
	checkpoint *checkpoint.Manager
	txBuffer   *replication.TransactionBuffer
	relations  *replication.RelationCache
	decoder    *replication.Decoder
	logger     *slog.Logger

	// Current transaction state (protocol: BEGIN carries xid, DML follows, COMMIT closes)
	mu         sync.Mutex
	currentXID uint32

	// Source identity
	sourceID   string
	ownerID    string
	generation int64
	flushedLSN replication.LSN

	// Test hooks
	crashAfterCommit bool
}

// NewService creates a capture service.
func NewService(cfg config.Config, pool *persistence.Pool) *Service {
	relations := replication.NewRelationCache()
	return &Service{
		cfg:        cfg,
		pool:       pool,
		checkpoint: checkpoint.NewManager(pool),
		txBuffer: replication.NewTransactionBuffer(
			cfg.MaxTransactionBufferBytes,
			cfg.MaxEventBatchSize,
			cfg.MaxInflightTransactions,
		),
		relations: relations,
		decoder:   replication.NewDecoder(relations),
		logger:    telemetry.With("service", "capture"),
		ownerID:   cfg.CaptureOwnerID,
	}
}

// SetCrashAfterCommit enables a test hook that crashes after commit before ACK.
func (s *Service) SetCrashAfterCommit(crash bool) {
	s.crashAfterCommit = crash
}

// Run starts the capture loop with reconnect/backoff.
func (s *Service) Run(ctx context.Context, sourceID string) error {
	s.sourceID = sourceID

	// Claim ownership
	generation, err := s.checkpoint.Claim(ctx, sourceID, s.ownerID, s.cfg.LeaseDuration)
	if err != nil {
		return fmt.Errorf("claim ownership: %w", err)
	}
	s.generation = generation
	s.logger.Info("claimed source ownership",
		"source", sourceID,
		"owner", s.ownerID,
		"generation", generation,
	)

	// Start heartbeat
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	go s.heartbeatLoop(heartbeatCtx)

	// Reconnect loop with bounded backoff
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Get current checkpoint
		cp, err := s.checkpoint.Get(ctx, sourceID)
		if err != nil {
			return fmt.Errorf("get checkpoint: %w", err)
		}

		var startLSN replication.LSN
		if cp != nil {
			startLSN = cp.PersistedLSN
			s.flushedLSN = cp.PersistedLSN
		}

		s.logger.Info("starting replication", "source", sourceID, "start_lsn", pglogrepl.LSN(startLSN).String())

		err = s.streamOnce(ctx, startLSN)
		if err == nil || ctx.Err() != nil {
			return err
		}

		s.logger.Warn("replication stream ended, reconnecting",
			"error", err,
			"backoff", backoff,
			"last_persisted_lsn", pglogrepl.LSN(s.flushedLSN).String(),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// streamOnce runs a single replication stream session.
func (s *Service) streamOnce(ctx context.Context, startLSN replication.LSN) error {
	replCfg := replication.Config{
		DatabaseURL:    s.cfg.SourceDBURL,
		SlotName:       s.cfg.ReplicationSlot,
		Publication:    s.cfg.Publication,
		StandbyTimeout: s.cfg.StandbyMessageTimeout,
	}

	client := replication.NewClient(replCfg)
	handler := &captureHandler{service: s}

	return client.Stream(ctx, startLSN, handler)
}

// heartbeatLoop maintains ownership.
func (s *Service) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := s.checkpoint.Heartbeat(ctx, s.sourceID, s.ownerID, s.generation, s.cfg.LeaseDuration)
			if err != nil {
				s.logger.Error("heartbeat failed", "error", err)
			}
		}
	}
}

// captureHandler implements replication.Handler.
type captureHandler struct {
	service *Service
}

// OnMessage processes decoded replication messages.
func (h *captureHandler) OnMessage(ctx context.Context, msg replication.Message) (replication.LSN, error) {
	s := h.service

	switch m := msg.(type) {
	case *pglogrepl.BeginMessage:
		s.mu.Lock()
		s.currentXID = m.Xid
		s.mu.Unlock()
		return 0, s.txBuffer.Begin(m.Xid, replication.LSN(m.FinalLSN))

	case *pglogrepl.InsertMessage:
		return 0, s.handleRowChange(m.RelationID, m.Tuple, nil, eventstore.OperationInsert)

	case *pglogrepl.UpdateMessage:
		return 0, s.handleRowChange(m.RelationID, m.NewTuple, m.OldTuple, eventstore.OperationUpdate)

	case *pglogrepl.DeleteMessage:
		return 0, s.handleRowChange(m.RelationID, nil, m.OldTuple, eventstore.OperationDelete)

	case *pglogrepl.CommitMessage:
		if err := s.persistTransaction(m.TransactionEndLSN, m.CommitTime); err != nil {
			return 0, err
		}

		if s.crashAfterCommit {
			panic("simulated crash after commit")
		}

		return replication.LSN(m.TransactionEndLSN), nil

	default:
		return 0, nil
	}
}

// OnKeepalive handles keepalive messages.
func (h *captureHandler) OnKeepalive(ctx context.Context, keepalive *pglogrepl.PrimaryKeepaliveMessage) bool {
	return keepalive.ReplyRequested
}

// OnError handles errors.
func (h *captureHandler) OnError(ctx context.Context, err error) {
	h.service.logger.Error("replication error", "error", err)
}

// handleRowChange builds an event from decoded tuple data.
func (s *Service) handleRowChange(relationID uint32, newTuple, oldTuple *pglogrepl.TupleData, op eventstore.Operation) error {
	s.mu.Lock()
	xid := s.currentXID
	s.mu.Unlock()

	if xid == 0 {
		return fmt.Errorf("row change outside transaction context")
	}

	rel, ok := s.relations.Get(relationID)
	if !ok {
		return fmt.Errorf("unknown relation OID %d (no Relation message received)", relationID)
	}

	event := &eventstore.Event{
		SourceID:   s.sourceID,
		SchemaName: rel.Namespace,
		TableName:  rel.Name,
		Operation:  op,
	}

	// Decode new tuple (INSERT/UPDATE)
	if newTuple != nil {
		values, err := s.decoder.DecodeTuple(rel, newTuple)
		if err != nil {
			return fmt.Errorf("decode new tuple: %w", err)
		}
		event.After = tupleToMap(values)
	}

	// Decode old tuple (UPDATE/DELETE)
	if oldTuple != nil {
		values, err := s.decoder.DecodeTuple(rel, oldTuple)
		if err != nil {
			return fmt.Errorf("decode old tuple: %w", err)
		}
		event.Before = tupleToMap(values)
	}

	// Extract key columns for partitioning
	event.Key = extractKey(rel, event.After, event.Before)

	return s.txBuffer.AddEvent(xid, event, estimateBytes(event))
}

// tupleToMap converts decoded tuple values to the event column map.
func tupleToMap(values []replication.TupleValue) map[string]eventstore.ColumnValue {
	result := make(map[string]eventstore.ColumnValue, len(values))
	for _, v := range values {
		cv := eventstore.ColumnValue{
			State: eventstore.ColumnState(v.State),
		}
		if v.State == replication.ColumnStateValue && v.Value != nil {
			cv.Value = v.Value
		}
		result[v.Column.Name] = cv
	}
	return result
}

// extractKey extracts key column values for partitioning.
func extractKey(rel *replication.Relation, after, before map[string]eventstore.ColumnValue) map[string]any {
	key := make(map[string]any)
	source := after
	if source == nil {
		source = before
	}
	if source == nil {
		return key
	}
	for _, col := range rel.KeyColumns() {
		if cv, ok := source[col.Name]; ok && cv.State == eventstore.ColumnStateValue {
			key[col.Name] = string(cv.Value)
		}
	}
	return key
}

// estimateBytes provides a rough size estimate for buffer accounting.
func estimateBytes(e *eventstore.Event) int64 {
	var size int64 = 200 // Base envelope
	for _, cv := range e.After {
		size += int64(len(cv.Value)) + 16
	}
	for _, cv := range e.Before {
		size += int64(len(cv.Value)) + 16
	}
	return size
}

// persistTransaction persists all events for a committed transaction atomically.
func (s *Service) persistTransaction(commitEndLSN replication.LSN, commitTime time.Time) error {
	s.mu.Lock()
	xid := s.currentXID
	s.currentXID = 0
	s.mu.Unlock()

	events, err := s.txBuffer.Commit(xid, commitEndLSN, commitTime)
	if err != nil {
		return fmt.Errorf("commit buffer: %w", err)
	}

	if len(events) == 0 {
		s.flushedLSN = commitEndLSN
		return nil
	}

	// One metadata transaction per source transaction (KTD-7)
	err = persistence.WithTx(context.Background(), s.pool, func(tx pgx.Tx) error {
		return s.ingestCommittedTransaction(context.Background(), tx, xid, commitEndLSN, commitTime, events)
	})
	if err != nil {
		return fmt.Errorf("persist transaction: %w", err)
	}

	s.flushedLSN = commitEndLSN
	return nil
}

// ingestCommittedTransaction performs the atomic ingest: COPY-to-staging, guarded insert,
// transaction record upsert, and fenced checkpoint update (KTD-7).
func (s *Service) ingestCommittedTransaction(ctx context.Context, tx pgx.Tx, xid uint32, commitEndLSN replication.LSN, commitTime time.Time, events []*eventstore.Event) error {
	// 1. Create transaction-local staging table
	if _, err := tx.Exec(ctx, `
		CREATE TEMPORARY TABLE IF NOT EXISTS events_staging (
			LIKE events INCLUDING DEFAULTS
		) ON COMMIT DROP
	`); err != nil {
		return fmt.Errorf("create staging: %w", err)
	}

	// 2. CopyFrom events into staging
	rows := make([][]any, len(events))
	for i, e := range events {
		beforeJSON, _ := json.Marshal(e.Before)
		afterJSON, _ := json.Marshal(e.After)
		keyJSON, _ := json.Marshal(e.Key)
		rows[i] = []any{
			e.ID[:],
			e.SourceID,
			nil, // transaction_id set below
			pglogrepl.LSN(e.CommitEndLSN).String(),
			e.SequenceNumber,
			e.SchemaName,
			e.TableName,
			string(e.Operation),
			e.RelationVersionID,
			nullIfEmpty(beforeJSON),
			nullIfEmpty(afterJSON),
			nullIfEmpty(keyJSON),
			e.PayloadHash,
			e.CommitTimestamp,
		}
	}

	_, err := tx.CopyFrom(ctx, pgx.Identifier{"events_staging"}, []string{
		"id", "source_id", "transaction_id", "commit_end_lsn", "sequence_number",
		"schema_name", "table_name", "operation", "relation_version_id",
		"before", "after", "key_columns", "payload_hash", "created_at",
	}, pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("copy to staging: %w", err)
	}

	// 3. Upsert transaction record
	var txID string
	err = tx.QueryRow(ctx, `
		INSERT INTO cdc_transactions (source_id, xid, commit_end_lsn, commit_timestamp, event_count)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (source_id, commit_end_lsn) DO UPDATE SET xid = cdc_transactions.xid
		RETURNING id
	`, s.sourceID, fmt.Sprintf("%d", xid), pglogrepl.LSN(commitEndLSN).String(), commitTime, len(events)).Scan(&txID)
	if err != nil {
		return fmt.Errorf("upsert transaction: %w", err)
	}

	// 4. Guarded set-based insert with identity conflict rule (KTD-5, KTD-7)
	tag, err := tx.Exec(ctx, `
		INSERT INTO events (id, source_id, transaction_id, commit_end_lsn, sequence_number,
		                    schema_name, table_name, operation, relation_version_id,
		                    before, after, key_columns, payload_hash, created_at)
		SELECT id, source_id, $2::uuid, commit_end_lsn, sequence_number,
		       schema_name, table_name, operation, relation_version_id,
		       before, after, key_columns, payload_hash, created_at
		FROM events_staging
		ON CONFLICT (source_id, commit_end_lsn, sequence_number) DO NOTHING
	`, txID)
	if err != nil {
		return fmt.Errorf("guarded insert: %w", err)
	}

	// Verify no payload-hash mismatch (corruption detection)
	var mismatchCount int
	err = tx.QueryRow(ctx, `
		SELECT count(*) FROM events_staging st
		JOIN events e ON e.source_id = st.source_id 
			AND e.commit_end_lsn = st.commit_end_lsn 
			AND e.sequence_number = st.sequence_number
		WHERE e.payload_hash != st.payload_hash
	`).Scan(&mismatchCount)
	if err != nil {
		return fmt.Errorf("check payload hash: %w", err)
	}
	if mismatchCount > 0 {
		return fmt.Errorf("payload hash mismatch on %d replayed events: possible corruption", mismatchCount)
	}

	inserted := int(tag.RowsAffected())
	s.logger.Debug("ingested transaction",
		"xid", xid,
		"commit_lsn", pglogrepl.LSN(commitEndLSN).String(),
		"events", len(events),
		"inserted", inserted,
		"replayed", len(events)-inserted,
	)

	// 5. Fenced checkpoint update (KTD-16)
	result, err := tx.Exec(ctx, `
		UPDATE source_checkpoints
		SET persisted_lsn = $4,
		    updated_at = now()
		WHERE source_id = $1
		  AND capture_owner = $2
		  AND owner_generation = $3
		  AND lease_expires_at > now()
		  AND persisted_lsn <= $4
	`, s.sourceID, s.ownerID, s.generation, pglogrepl.LSN(commitEndLSN).String())
	if err != nil {
		return fmt.Errorf("fenced checkpoint update: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("fencing violation: owner=%s generation=%d lost ownership",
			s.ownerID, s.generation)
	}

	return nil
}

func nullIfEmpty(b []byte) any {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return string(b)
}