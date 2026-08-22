package replication

import (
	"testing"
	"time"

	"github.com/tyagiquamar/relaydb/internal/eventstore"
)

func TestTransactionBuffer_BasicFlow(t *testing.T) {
	buf := NewTransactionBuffer(1024*1024, 1000, 10)

	// Begin transaction
	err := buf.Begin(123, LSN(1000))
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	// Add events
	event1 := &eventstore.Event{
		SchemaName: "public",
		TableName:  "orders",
		Operation:  eventstore.OperationInsert,
	}
	event2 := &eventstore.Event{
		SchemaName: "public",
		TableName:  "order_items",
		Operation:  eventstore.OperationInsert,
	}

	err = buf.AddEvent(123, event1, 100)
	if err != nil {
		t.Fatalf("AddEvent() error = %v", err)
	}
	err = buf.AddEvent(123, event2, 150)
	if err != nil {
		t.Fatalf("AddEvent() error = %v", err)
	}

	// Commit
	events, err := buf.Commit(123, LSN(2000), time.Now())
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	if len(events) != 2 {
		t.Errorf("Commit() returned %d events, want 2", len(events))
	}

	// Check sequence numbers
	if events[0].SequenceNumber != 1 || events[1].SequenceNumber != 2 {
		t.Errorf("sequence numbers = %d, %d, want 1, 2",
			events[0].SequenceNumber, events[1].SequenceNumber)
	}

	// Check ULIDs assigned
	if events[0].ID.IsZero() || events[1].ID.IsZero() {
		t.Error("ULIDs should be assigned")
	}

	// Check ordering (ULIDs should be monotonic)
	if events[0].ID.Compare(events[1].ID) >= 0 {
		t.Error("ULIDs should be monotonically increasing")
	}
}

func TestTransactionBuffer_Abort(t *testing.T) {
	buf := NewTransactionBuffer(1024*1024, 1000, 10)

	buf.Begin(123, LSN(1000))
	buf.AddEvent(123, &eventstore.Event{}, 100)

	// Abort
	buf.Abort(123)

	stats := buf.Stats()
	if stats.InflightTransactions != 0 {
		t.Errorf("InflightTransactions = %d, want 0", stats.InflightTransactions)
	}
	if stats.TotalBytes != 0 {
		t.Errorf("TotalBytes = %d, want 0", stats.TotalBytes)
	}
}

func TestTransactionBuffer_Limits(t *testing.T) {
	buf := NewTransactionBuffer(200, 2, 2) // Very small limits

	buf.Begin(123, LSN(1000))

	// Add first event (should succeed)
	err := buf.AddEvent(123, &eventstore.Event{}, 100)
	if err != nil {
		t.Fatalf("AddEvent() error = %v", err)
	}

	// Add second event (should succeed)
	err = buf.AddEvent(123, &eventstore.Event{}, 100)
	if err != nil {
		t.Fatalf("AddEvent() error = %v", err)
	}

	// Add third event (exceeds event limit)
	err = buf.AddEvent(123, &eventstore.Event{}, 100)
	if err == nil {
		t.Error("AddEvent() should fail when exceeding event limit")
	}
}

func TestTransactionBuffer_ByteLimit(t *testing.T) {
	buf := NewTransactionBuffer(250, 1000, 10) // 250 byte limit

	buf.Begin(123, LSN(1000))

	buf.AddEvent(123, &eventstore.Event{}, 100)
	buf.AddEvent(123, &eventstore.Event{}, 100)

	// Third event exceeds byte limit
	err := buf.AddEvent(123, &eventstore.Event{}, 100)
	if err == nil {
		t.Error("AddEvent() should fail when exceeding byte limit")
	}
}

func TestTransactionBuffer_InflightLimit(t *testing.T) {
	buf := NewTransactionBuffer(1024*1024, 1000, 2) // Max 2 inflight

	buf.Begin(1, LSN(1000))
	buf.Begin(2, LSN(2000))

	// Third should fail
	err := buf.Begin(3, LSN(3000))
	if err == nil {
		t.Error("Begin() should fail when exceeding inflight limit")
	}
}

func TestTransactionBuffer_MultipleTransactions(t *testing.T) {
	buf := NewTransactionBuffer(1024*1024, 1000, 10)

	// Transaction 1
	buf.Begin(1, LSN(1000))
	buf.AddEvent(1, &eventstore.Event{TableName: "t1"}, 100)

	// Transaction 2 (interleaved)
	buf.Begin(2, LSN(2000))
	buf.AddEvent(2, &eventstore.Event{TableName: "t2"}, 100)
	buf.AddEvent(1, &eventstore.Event{TableName: "t1"}, 100)

	// Commit 1
	events1, _ := buf.Commit(1, LSN(1500), time.Now())
	if len(events1) != 2 {
		t.Errorf("Commit(1) returned %d events, want 2", len(events1))
	}
	if events1[0].TableName != "t1" || events1[1].TableName != "t1" {
		t.Error("events from wrong transaction")
	}

	// Commit 2
	events2, _ := buf.Commit(2, LSN(2500), time.Now())
	if len(events2) != 1 {
		t.Errorf("Commit(2) returned %d events, want 1", len(events2))
	}
}
