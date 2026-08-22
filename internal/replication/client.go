package replication

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// Client manages a logical replication connection.
// The connection is owned by a single goroutine (KTD-13).
type Client struct {
	config Config

	// Connection (owned by conn goroutine)
	conn   *pgconn.PgConn
	connMu sync.Mutex // Protects conn state changes

	// State
	receivedLSN LSN // Written only by the receive loop

	// flushedLSN is the last handler-confirmed flush position. It is written
	// by the receive loop after OnMessage and read by sendStatusUpdate, which
	// runs on both the receive loop and the connection loop, so every access
	// goes through the atomic.
	flushedLSN atomic.Uint64

	// Channels for communication with conn goroutine
	errCh    chan error
	doneCh   chan struct{}

	// Message handler
	handler Handler
}

// Config holds replication client configuration.
type Config struct {
	DatabaseURL        string
	SlotName           string
	Publication        string
	StandbyTimeout     time.Duration
	RetryInitialDelay  time.Duration
	RetryMaxDelay      time.Duration
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
	return &Client{
		config: cfg,
		errCh:  make(chan error, 1),
		doneCh: make(chan struct{}),
	}
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
	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	defer func() {
		c.connMu.Lock()
		if c.conn != nil {
			c.conn.Close(context.Background())
			c.conn = nil
		}
		c.connMu.Unlock()
	}()

	// Identify system
	sysident, err := pglogrepl.IdentifySystem(ctx, conn)
	if err != nil {
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

	// Start connection-owning goroutine
	go c.connLoop(ctx, sysident)

	// Main receive loop
	return c.receiveLoop(ctx, sysident)
}

// connLoop owns the replication connection.
// It handles standby status updates and keepalive replies.
func (c *Client) connLoop(ctx context.Context, sysident pglogrepl.IdentifySystemResult) {
	ticker := time.NewTicker(c.config.StandbyTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.doneCh:
			return
		case <-ticker.C:
			// Send periodic status update
			c.sendStatusUpdate(ctx)
		}
	}
}

// receiveLoop receives messages from the replication stream.
func (c *Client) receiveLoop(ctx context.Context, sysident pglogrepl.IdentifySystemResult) error {
	decoder := NewDecoder(NewRelationCache())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Receive with timeout
		ctxTimeout, cancel := context.WithTimeout(ctx, c.config.StandbyTimeout)
		rawMsg, err := c.conn.ReceiveMessage(ctxTimeout)
		cancel()

		if err != nil {
			if pgconn.Timeout(err) {
				// Timeout - send status update and continue
				c.sendStatusUpdate(ctx)
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

	// Update flushed LSN if provided
	if flushLSN > 0 {
		c.flushedLSN.Store(uint64(flushLSN))
	}

	return nil
}

// handleKeepalive processes keepalive messages.
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
func (c *Client) sendStatusUpdate(ctx context.Context) error {
	c.connMu.Lock()
	conn := c.conn
	c.connMu.Unlock()

	if conn == nil {
		return fmt.Errorf("no connection")
	}

	flushed := LSN(c.flushedLSN.Load())

	update := pglogrepl.StandbyStatusUpdate{
		WALWritePosition: flushed,
		WALFlushPosition: flushed,
		WALApplyPosition: flushed,
		ClientTime:       time.Now(),
		ReplyRequested:   false,
	}

	return pglogrepl.SendStandbyStatusUpdate(ctx, conn, update)
}

// isDuplicateObject checks if the error is "already exists".
func isDuplicateObject(err error) bool {
	if pgErr, ok := err.(*pgconn.PgError); ok {
		return pgErr.Code == "42710" // duplicate_object
	}
	return false
}