package replication

import (
	"fmt"
	"sync"
	"time"

	"github.com/tyagiquamar/relaydb/internal/eventstore"
)

// TransactionBuffer accumulates decoded events for a single transaction.
// Emits the full event set only at COMMIT.
type TransactionBuffer struct {
	mu sync.Mutex

	// Configuration
	maxBytes    int64
	maxEvents   int
	maxInflight int

	// State
	transactions map[uint32]*TxContext // keyed by xid
	totalBytes   int64
}

// TxContext holds the state for one in-flight transaction.
type TxContext struct {
	XID        uint32
	FinalLSN   LSN
	CommitLSN  LSN
	CommitTime time.Time
	Events     []*eventstore.Event
	TotalBytes int64
	Sequence   int
}

// NewTransactionBuffer creates a buffer with the given limits.
func NewTransactionBuffer(maxBytes int64, maxEvents, maxInflight int) *TransactionBuffer {
	return &TransactionBuffer{
		maxBytes:     maxBytes,
		maxEvents:    maxEvents,
		maxInflight:  maxInflight,
		transactions: make(map[uint32]*TxContext),
	}
}

// Begin starts tracking a new transaction.
func (b *TransactionBuffer) Begin(xid uint32, finalLSN LSN) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.transactions) >= b.maxInflight {
		return fmt.Errorf("too many in-flight transactions: %d >= %d",
			len(b.transactions), b.maxInflight)
	}

	b.transactions[xid] = &TxContext{
		XID:      xid,
		FinalLSN: finalLSN,
		Events:   make([]*eventstore.Event, 0),
	}

	return nil
}

// AddEvent adds an event to the current transaction.
// Returns error if limits would be exceeded.
func (b *TransactionBuffer) AddEvent(xid uint32, event *eventstore.Event, eventBytes int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	tx, ok := b.transactions[xid]
	if !ok {
		return fmt.Errorf("unknown transaction: %d", xid)
	}

	// Check limits
	if tx.TotalBytes+eventBytes > b.maxBytes {
		return fmt.Errorf("transaction %d exceeds byte limit: %d + %d > %d",
			xid, tx.TotalBytes, eventBytes, b.maxBytes)
	}

	if len(tx.Events) >= b.maxEvents {
		return fmt.Errorf("transaction %d exceeds event limit: %d >= %d",
			xid, len(tx.Events), b.maxEvents)
	}

	// Assign sequence number
	tx.Sequence++
	event.SequenceNumber = tx.Sequence
	event.CommitEndLSN = uint64(tx.FinalLSN)

	tx.Events = append(tx.Events, event)
	tx.TotalBytes += eventBytes
	b.totalBytes += eventBytes

	return nil
}

// Commit finalizes a transaction and returns all events.
// The returned events have ULIDs assigned and are ready for persistence.
func (b *TransactionBuffer) Commit(xid uint32, commitLSN LSN, commitTime time.Time) ([]*eventstore.Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	tx, ok := b.transactions[xid]
	if !ok {
		return nil, fmt.Errorf("unknown transaction: %d", xid)
	}

	// Assign final metadata
	for _, event := range tx.Events {
		event.ID = eventstore.NewEventID()
		event.CommitTimestamp = commitTime
		event.CommitEndLSN = uint64(commitLSN)
		event.PayloadHash = eventstore.ComputePayloadHash(event)
	}

	events := tx.Events
	b.totalBytes -= tx.TotalBytes
	delete(b.transactions, xid)

	return events, nil
}

// Abort discards a transaction's events (connection drop, ROLLBACK).
func (b *TransactionBuffer) Abort(xid uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if tx, ok := b.transactions[xid]; ok {
		b.totalBytes -= tx.TotalBytes
		delete(b.transactions, xid)
	}
}

// Stats returns buffer statistics.
func (b *TransactionBuffer) Stats() BufferStats {
	b.mu.Lock()
	defer b.mu.Unlock()

	var txCount int
	var totalEvents int
	for _, tx := range b.transactions {
		txCount++
		totalEvents += len(tx.Events)
	}

	return BufferStats{
		InflightTransactions: txCount,
		TotalEvents:          totalEvents,
		TotalBytes:           b.totalBytes,
		MaxBytes:             b.maxBytes,
		MaxEvents:            b.maxEvents,
	}
}

// BufferStats holds buffer metrics.
type BufferStats struct {
	InflightTransactions int
	TotalEvents          int
	TotalBytes           int64
	MaxBytes             int64
	MaxEvents            int
}

// Reset clears all state (for testing).
func (b *TransactionBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.transactions = make(map[uint32]*TxContext)
	b.totalBytes = 0
}
