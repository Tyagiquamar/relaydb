package replication

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// Client manages a logical replication connection.
//
// Ownership (KTD-13): *pgconn.PgConn is not safe for concurrent use, so
// exactly one goroutine — the one calling Stream — owns the connection for its
// entire lifetime. That goroutine serially performs every operation on it:
// ReceiveMessage, XLogData processing, keepalive handling, and standby status
// updates (periodic via the receive timeout and immediate on ReplyRequested).
// No second goroutine ever touches the replication connection.
type Client struct {
	config Config

	// conn is created, used, and closed entirely by the Stream goroutine.
	conn *pgconn.PgConn

	// receivedLSN is the last WAL position observed on the wire. It is never
	// acknowledged. Written and read only by the Stream goroutine.
	receivedLSN LSN

	// flushedLSN is the last handler-confirmed durable position. It is the
	// only LSN reported in standby status updates (KTD-3: never report a
	// received-only position). Owned exclusively by the Stream goroutine.
	flushedLSN LSN

	// Message handler
	handler Handler
}

// Config holds replication client configuration.
type Config struct {
	DatabaseURL       string
	SlotName          string
	Publication       string
	StandbyTimeout    time.Duration
	RetryInitialDelay time.Duration
	RetryMaxDelay     time.Duration
}

// Handler receives decoded messages.
type Handler interface {
	// OnMessage is called for each decoded message.
	// The handler returns the LSN to acknowledge (usually 0 = no ack yet).
	OnMessage(ctx context.Context, msg Message) (flushLSN LSN, err error)

	// OnKeepalive is called for keepalive messages.
	// Return true to reply immediately.
	OnKeepalive(ctx context.Context, keepalive *pglogrepl.PrimaryKeepaliveMessage) bool

	// OnError is called on connection errors.
	OnError(ctx context.Context, err error)
}

// NewClient creates a replication client.
func NewClient(cfg Config) *Client {
	return &Client{config: cfg}
}

// Stream starts the replication stream.
// This blocks until the context is cancelled or an error occurs.
func (c *Client) Stream(ctx context.Context, startLSN LSN, handler Handler) error {
	c.handler = handler

	// Create replication connection. Build the URL properly so that a
	// user-supplied RELAYDB_SOURCE_DB_URL with or without existing query
	// params gets replication=database appended correctly.
	connURL, err := url.Parse(c.config.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	q := connURL.Query()
	q.Set("replication", "database")
	connURL.RawQuery = q.Encode()

	conn, err := pgconn.Connect(ctx, connURL.String())
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	c.conn = conn

	defer func() {
		if c.conn != nil {
			_ = c.conn.Close(context.Background())
			c.conn = nil
		}
	}()

	// Identify system (verifies the replication handshake end-to-end)
	if _, err := pglogrepl.IdentifySystem(ctx, conn); err != nil {
		return fmt.Errorf("identify system: %w", err)
	}

	// Create replication slot if needed
	_, err = pglogrepl.CreateReplicationSlot(ctx, conn, c.config.SlotName, "pgoutput",
		pglogrepl.CreateReplicationSlotOptions{Temporary: false})
	if err != nil && !isDuplicateObject(err) {
		return fmt.Errorf("create slot: %w", err)
	}

	// Start replication
	err = pglogrepl.StartReplication(ctx, conn, c.config.SlotName, startLSN,
		pglogrepl.StartReplicationOptions{
			PluginArgs: []string{
				"proto_version '1'",
				"streaming 'off'",
				"two_phase 'false'",
				"publication_names '" + c.config.Publication + "'",
			},
		})
	if err != nil {
		return fmt.Errorf("start replication: %w", err)
	}

	// Run the connection-owning loop on this goroutine. Everything from here
	// on (receive, decode, handler dispatch, status updates) is serialized.
	return c.receiveLoop(ctx)
}

// receiveLoop owns the replication connection for the duration of the stream.
// It serially receives messages, processes XLogData and keepalives, and sends
// standby status updates. An idle receive window of StandbyTimeout doubles as
// the periodic status update interval, so no second goroutine is needed.
func (c *Client) receiveLoop(ctx context.Context) error {
	decoder := NewDecoder(NewRelationCache())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Receive with timeout. On deadline expiry pgconn only sets a socket
		// read deadline — the connection stays usable — so we can send the
		// periodic standby status update and continue receiving on the same
		// goroutine.
		ctxTimeout, cancel := context.WithTimeout(ctx, c.config.StandbyTimeout)
		rawMsg, err := c.conn.ReceiveMessage(ctxTimeout)
		cancel()

		if err != nil {
			if pgconn.Timeout(err) {
				// Idle for a full StandbyTimeout: report progress.
				if err := c.sendStatusUpdate(ctx); err != nil {
					return fmt.Errorf("send status update: %w", err)
				}
				continue
			}
			if _, _, ok := pglogrepl.IsErrEndTimeline(err); ok {
				// End of timeline - reconnect needed
				return fmt.Errorf("end of timeline: %w", err)
			}
			return fmt.Errorf("receive message: %w", err)
		}

		// Handle CopyData
		if cd, ok := rawMsg.(*pgproto3.CopyData); ok {
			if err := c.handleCopyData(ctx, cd, decoder); err != nil {
				return err
			}
		}
	}
}

// handleCopyData processes CopyData messages (XLogData or keepalive).
func (c *Client) handleCopyData(ctx context.Context, cd *pgproto3.CopyData, decoder *Decoder) error {
	if len(cd.Data) == 0 {
		return nil
	}

	switch cd.Data[0] {
	case pglogrepl.XLogDataByteID: // 'w'
		return c.handleXLogData(ctx, cd.Data[1:], decoder)
	case pglogrepl.PrimaryKeepaliveMessageByteID: // 'k'
		return c.handleKeepalive(ctx, cd.Data[1:])
	default:
		return fmt.Errorf("unknown copy data type: %c", cd.Data[0])
	}
}

// handleXLogData processes XLogData (WAL messages).
func (c *Client) handleXLogData(ctx context.Context, data []byte, decoder *Decoder) error {
	xld, err := pglogrepl.ParseXLogData(data)
	if err != nil {
		return fmt.Errorf("parse xlog data: %w", err)
	}

	// Advance received LSN (never acked until flushed)
	c.receivedLSN = xld.WALStart

	// Decode and handle message
	msg, err := decoder.Decode(xld.WALData)
	if err != nil {
		return fmt.Errorf("decode message: %w", err)
	}

	// Call handler
	flushLSN, err := c.handler.OnMessage(ctx, msg)
	if err != nil {
		return err
	}

	// Update flushed LSN only after the handler confirms persistence
	if flushLSN > 0 {
		c.flushedLSN = flushLSN
	}

	return nil
}

// handleKeepalive processes keepalive messages. When a reply is requested —
// by the server or the handler — the standby status update is sent right
// here, on the connection-owning goroutine.
func (c *Client) handleKeepalive(ctx context.Context, data []byte) error {
	keepalive, err := pglogrepl.ParsePrimaryKeepaliveMessage(data)
	if err != nil {
		return fmt.Errorf("parse keepalive: %w", err)
	}

	// Ask handler if we should reply
	if c.handler.OnKeepalive(ctx, &keepalive) || keepalive.ReplyRequested {
		return c.sendStatusUpdate(ctx)
	}

	return nil
}

// sendStatusUpdate sends a standby status update.
// Only reports flushed LSN (KTD-3: never report received-only LSN).
// Must only be called from the connection-owning goroutine.
func (c *Client) sendStatusUpdate(ctx context.Context) error {
	if c.conn == nil {
		return fmt.Errorf("no connection")
	}

	update := pglogrepl.StandbyStatusUpdate{
		WALWritePosition: c.flushedLSN,
		WALFlushPosition: c.flushedLSN,
		WALApplyPosition: c.flushedLSN,
		ClientTime:       time.Now(),
		ReplyRequested:   false,
	}

	return pglogrepl.SendStandbyStatusUpdate(ctx, c.conn, update)
}

// isDuplicateObject checks if the error is "already exists".
func isDuplicateObject(err error) bool {
	if pgErr, ok := err.(*pgconn.PgError); ok {
		return pgErr.Code == "42710" // duplicate_object
	}
	return false
}
