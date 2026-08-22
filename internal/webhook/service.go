package webhook

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tyagiquamar/relaydb/internal/crypto"
	"github.com/tyagiquamar/relaydb/internal/persistence"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

// Service runs the webhook delivery loop: enqueue attempts for matching
// events, claim pending attempts (SKIP LOCKED), deliver, retry with backoff,
// and write to the DLQ on exhaustion.
type Service struct {
	pool      *persistence.Pool
	deliverer *Deliverer
	envelope  *crypto.Envelope
	backoff   *Backoff
	logger    *slog.Logger
}

// NewService creates the delivery service. masterKeyB64 decrypts sink secrets;
// it may be empty if no sinks are configured.
func NewService(pool *persistence.Pool, masterKeyB64 string) (*Service, error) {
	var env *crypto.Envelope
	if masterKeyB64 != "" {
		e, err := crypto.NewEnvelope(masterKeyB64)
		if err != nil {
			return nil, fmt.Errorf("init crypto envelope: %w", err)
		}
		env = e
	}
	return &Service{
		pool:      pool,
		deliverer: NewDeliverer(),
		envelope:  env,
		backoff:   DefaultBackoff(),
		logger:    telemetry.With("service", "delivery"),
	}, nil
}

// Run loops until ctx is cancelled.
func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		// Enqueue attempt-1 rows for events matching enabled sinks.
		if n, err := s.enqueue(ctx); err != nil {
			s.logger.Error("enqueue failed", "error", err)
		} else if n > 0 {
			s.logger.Info("enqueued deliveries", "count", n)
		}

		// Claim and deliver a batch of due attempts.
		if err := s.deliverBatch(ctx, 10); err != nil {
			s.logger.Error("deliver batch failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// enqueue creates attempt-1 delivery rows for enabled sinks and matching events.
func (s *Service) enqueue(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO delivery_attempts (sink_id, event_id, attempt_number, status, idempotency_key, next_retry_at)
		SELECT w.id, e.id, 1, 'pending', w.id::text || ':' || encode(e.id, 'hex'), now()
		FROM webhook_sinks w
		JOIN events e ON (w.source_id IS NULL OR e.source_id = w.source_id)
		WHERE w.enabled
		  AND (w.schema_filter IS NULL OR e.schema_name = w.schema_filter)
		  AND (w.table_filter IS NULL OR e.table_name = w.table_filter)
		  AND (w.operation_filter IS NULL OR position(e.operation in w.operation_filter) > 0)
		  AND NOT EXISTS (
		      SELECT 1 FROM delivery_attempts da
		      WHERE da.sink_id = w.id AND da.event_id = e.id
		  )
		LIMIT 500
	`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

type claimedAttempt struct {
	attemptID      int64
	sinkID         string
	eventID        []byte
	attemptNumber  int
	maxAttempts    int
	idempotencyKey string
	url            string
	secretEnc      []byte
	payload        []byte
}

// deliverBatch claims up to n due attempts and delivers each in its own tx.
func (s *Service) deliverBatch(ctx context.Context, n int) error {
	for i := 0; i < n; i++ {
		done, err := s.deliverOne(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return nil
}

// deliverOne claims a single due attempt (SKIP LOCKED) and processes it.
// Returns done=true when no work is available.
func (s *Service) deliverOne(ctx context.Context) (bool, error) {
	err := persistence.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		var a claimedAttempt
		var before, after, keyCols []byte
		var schema, table, op string
		var seq int
		var lsn string

		err := tx.QueryRow(ctx, `
			SELECT da.id, da.sink_id, da.event_id, da.attempt_number, da.idempotency_key,
			       w.url, w.secret_encrypted, w.max_attempts,
			       e.before, e.after, e.key_columns, e.schema_name, e.table_name, e.operation,
			       e.sequence_number, e.commit_end_lsn::text
			FROM delivery_attempts da
			JOIN webhook_sinks w ON w.id = da.sink_id
			JOIN events e ON e.id = da.event_id
			WHERE da.status IN ('pending', 'retryable')
			  AND (da.next_retry_at IS NULL OR da.next_retry_at <= now())
			ORDER BY da.id
			LIMIT 1
			FOR UPDATE OF da SKIP LOCKED
		`).Scan(&a.attemptID, &a.sinkID, &a.eventID, &a.attemptNumber, &a.idempotencyKey,
			&a.url, &a.secretEnc, &a.maxAttempts,
			&before, &after, &keyCols, &schema, &table, &op, &seq, &lsn)

		if err == pgx.ErrNoRows {
			return errNoWork
		}
		if err != nil {
			return fmt.Errorf("claim attempt: %w", err)
		}

		a.payload = buildEventPayload(a.eventID, schema, table, op, seq, lsn, before, after, keyCols)

		secret := ""
		if s.envelope != nil && len(a.secretEnc) > 0 {
			plain, err := s.envelope.Decrypt(a.secretEnc, crypto.ComputeAAD(a.sinkID, "webhook-secret"))
			if err != nil {
				s.logger.Warn("secret decrypt failed, delivering unsigned", "sink", a.sinkID, "error", err)
			} else {
				secret = string(plain)
			}
		}

		result := s.deliverer.Deliver(ctx, &Delivery{
			ID:             fmt.Sprintf("%d", a.attemptID),
			SinkID:         a.sinkID,
			EventID:        a.eventID,
			URL:            a.url,
			Secret:         secret,
			Payload:        a.payload,
			Attempt:        a.attemptNumber,
			MaxAttempts:    a.maxAttempts,
			IdempotencyKey: a.idempotencyKey,
		})

		return s.recordResult(ctx, tx, &a, result)
	})
	if err == errNoWork {
		return true, nil
	}
	return false, err
}

var errNoWork = fmt.Errorf("no work")

// recordResult writes the outcome and schedules retry or DLQ.
func (s *Service) recordResult(ctx context.Context, tx pgx.Tx, a *claimedAttempt, res *Result) error {
	eventIDHex := hex.EncodeToString(a.eventID)

	switch {
	case res.Error == nil && res.StatusCode >= 200 && res.StatusCode < 300:
		_, err := tx.Exec(ctx, `
			UPDATE delivery_attempts
			SET status = 'success', http_status = $2, response_body = $3, completed_at = now()
			WHERE id = $1
		`, a.attemptID, res.StatusCode, truncate(res.Body, 1024))
		if err != nil {
			return err
		}
		s.logger.Debug("delivered", "attempt", a.attemptID, "status", res.StatusCode)
		return nil

	case res.Retryable && s.backoff.ShouldRetry(a.attemptNumber, a.maxAttempts):
		// Mark this attempt retryable and enqueue the next attempt.
		if _, err := tx.Exec(ctx, `
			UPDATE delivery_attempts
			SET status = 'retryable', http_status = $2, error_message = $3, completed_at = now()
			WHERE id = $1
		`, a.attemptID, res.StatusCode, errString(res)); err != nil {
			return err
		}
		delay := s.backoff.Delay(a.attemptNumber)
		_, err := tx.Exec(ctx, `
			INSERT INTO delivery_attempts (sink_id, event_id, attempt_number, status, idempotency_key, next_retry_at)
			VALUES ($1, $2, $3, 'pending', $4, now() + $5::interval)
			ON CONFLICT (sink_id, event_id, attempt_number) DO NOTHING
		`, a.sinkID, a.eventID, a.attemptNumber+1, a.idempotencyKey, delay.String())
		return err

	default:
		// Permanent failure or exhausted retries: mark + DLQ.
		if _, err := tx.Exec(ctx, `
			UPDATE delivery_attempts
			SET status = 'permanent_failure', http_status = $2, error_message = $3, completed_at = now()
			WHERE id = $1
		`, a.attemptID, res.StatusCode, errString(res)); err != nil {
			return err
		}

		var sourceID string
		if err := tx.QueryRow(ctx, `SELECT source_id FROM events WHERE id = $1`, a.eventID).Scan(&sourceID); err != nil {
			return fmt.Errorf("lookup event source: %w", err)
		}

		history, _ := json.Marshal([]map[string]any{{
			"attempt":     a.attemptNumber,
			"timestamp":   time.Now().UTC(),
			"error":       errString(res),
			"http_status": res.StatusCode,
		}})

		_, err := tx.Exec(ctx, `
			INSERT INTO dead_letter_events (event_id, source_id, sink_id, failure_reason, attempt_history)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (event_id, sink_id, consumer_group_id) DO NOTHING
		`, a.eventID, sourceID, a.sinkID,
			fmt.Sprintf("webhook delivery failed after %d attempts: %s", a.attemptNumber, errString(res)),
			history)
		if err != nil {
			return err
		}
		s.logger.Warn("event dead-lettered", "event", eventIDHex, "sink", a.sinkID, "attempts", a.attemptNumber)
		return nil
	}
}

// buildEventPayload constructs the webhook JSON body for an event.
func buildEventPayload(eventID []byte, schema, table, op string, seq int, lsn string, before, after, keyCols []byte) []byte {
	payload := map[string]any{
		"id":              hex.EncodeToString(eventID),
		"schema_name":     schema,
		"table_name":      table,
		"operation":       op,
		"sequence_number": seq,
		"commit_end_lsn":  lsn,
		"before":          json.RawMessage(nullable(before)),
		"after":           json.RawMessage(nullable(after)),
		"key_columns":     json.RawMessage(nullable(keyCols)),
	}
	b, _ := json.Marshal(payload)
	return b
}

func nullable(b []byte) []byte {
	if len(b) == 0 {
		return []byte("null")
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func errString(res *Result) string {
	if res.Error != nil {
		return res.Error.Error()
	}
	return fmt.Sprintf("http status %d", res.StatusCode)
}
