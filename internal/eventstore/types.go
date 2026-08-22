package eventstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/oklog/ulid/v2"
)

// Operation represents the type of change.
type Operation string

const (
	OperationInsert Operation = "insert"
	OperationUpdate Operation = "update"
	OperationDelete Operation = "delete"
)

// ColumnState represents the state of a column value in a CDC event.
type ColumnState string

const (
	ColumnStateValue          ColumnState = "value"           // Normal value present
	ColumnStateNull           ColumnState = "null"            // Explicit NULL
	ColumnStateUnchangedToast ColumnState = "unchanged_toast" // TOAST column not sent (UPDATE)
	ColumnStateAbsent         ColumnState = "absent"          // Column not in replica identity
)

// ColumnValue wraps a column value with its state.
type ColumnValue struct {
	State ColumnState     `json:"_state"`
	Value json.RawMessage `json:"value,omitempty"`
}

// Event represents a CDC event with full context.
type Event struct {
	// Identity
	ID ulid.ULID `json:"id"` // 16-byte ULID

	// Source context
	SourceID      string `json:"source_id"`
	TransactionID string `json:"transaction_id"`

	// Ordering (canonical position)
	CommitEndLSN   uint64 `json:"commit_end_lsn"`
	SequenceNumber int    `json:"sequence_number"`

	// Event metadata
	SchemaName string    `json:"schema_name"`
	TableName  string    `json:"table_name"`
	Operation  Operation `json:"operation"`

	// Schema version at decode time
	RelationVersionID int64 `json:"relation_version_id"`

	// Payload with column-state markers
	Before map[string]ColumnValue `json:"before,omitempty"` // null for INSERT
	After  map[string]ColumnValue `json:"after,omitempty"`  // null for DELETE
	Key    map[string]any         `json:"key,omitempty"`    // primary/replica key values

	// Idempotency
	PayloadHash string `json:"payload_hash"` // SHA-256 of canonical JSON

	// Timestamps
	CommitTimestamp time.Time `json:"commit_timestamp"`
	CreatedAt       time.Time `json:"created_at"`
}

// Transaction represents a committed database transaction.
type Transaction struct {
	ID              string    `json:"id"`
	SourceID        string    `json:"source_id"`
	XID             uint64    `json:"xid"` // PostgreSQL transaction ID
	CommitEndLSN    uint64    `json:"commit_end_lsn"`
	CommitTimestamp time.Time `json:"commit_timestamp"`
	EventCount      int       `json:"event_count"`
	TotalBytes      int64     `json:"total_bytes"`
	CreatedAt       time.Time `json:"created_at"`
}

// RelationVersion represents a schema version for a table.
type RelationVersion struct {
	ID              int64           `json:"id"`
	SourceID        string          `json:"source_id"`
	RelationOID     uint32          `json:"relation_oid"`
	SchemaName      string          `json:"schema_name"`
	TableName       string          `json:"table_name"`
	Fingerprint     string          `json:"fingerprint"`
	ColumnDefs      []ColumnDef     `json:"column_definitions"`
	ReplicaIdentity ReplicaIdentity `json:"replica_identity"`
	CreatedAt       time.Time       `json:"created_at"`
}

// ColumnDef describes a column in a relation.
type ColumnDef struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Position int    `json:"position"`
}

// ReplicaIdentity represents the PostgreSQL replica identity setting.
type ReplicaIdentity string

const (
	ReplicaIdentityDefault ReplicaIdentity = "default" // Use primary key
	ReplicaIdentityNothing ReplicaIdentity = "nothing" // No old values (dangerous)
	ReplicaIdentityFull    ReplicaIdentity = "full"    // Full before-image
	ReplicaIdentityIndex   ReplicaIdentity = "index"   // Use specified index
)

// NewEventID generates a new ULID for an event.
func NewEventID() ulid.ULID {
	return ulid.Make()
}

// EventIDFromBytes converts bytes to ULID.
func EventIDFromBytes(b []byte) (ulid.ULID, error) {
	return ulid.Parse(string(b))
}

// ComputePayloadHash computes a deterministic SHA-256 hash of the event payload.
// This is used for idempotency verification under WAL replay.
func ComputePayloadHash(e *Event) string {
	// Create canonical representation for hashing
	canonical := struct {
		SourceID     string                 `json:"source_id"`
		CommitEndLSN uint64                 `json:"commit_end_lsn"`
		Sequence     int                    `json:"sequence_number"`
		Schema       string                 `json:"schema_name"`
		Table        string                 `json:"table_name"`
		Operation    Operation              `json:"operation"`
		Before       map[string]ColumnValue `json:"before"`
		After        map[string]ColumnValue `json:"after"`
		Key          map[string]any         `json:"key"`
	}{
		SourceID:     e.SourceID,
		CommitEndLSN: e.CommitEndLSN,
		Sequence:     e.SequenceNumber,
		Schema:       e.SchemaName,
		Table:        e.TableName,
		Operation:    e.Operation,
		Before:       e.Before,
		After:        e.After,
		Key:          e.Key,
	}

	data, _ := json.Marshal(canonical)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// IsDuplicate checks if an event with the same identity already exists.
// This is called before insert to implement idempotency.
func (e *Event) Identity() string {
	return e.SourceID + ":" + string(rune(e.CommitEndLSN)) + ":" + string(rune(e.SequenceNumber))
}
