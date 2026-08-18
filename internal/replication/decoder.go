package replication

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/jackc/pglogrepl"
)

// MessageType is the type of a replication message.
type MessageType = pglogrepl.MessageType

const (
	MessageTypeRelation               = pglogrepl.MessageTypeRelation
	MessageTypeType                   = pglogrepl.MessageTypeType
	MessageTypeInsert                 = pglogrepl.MessageTypeInsert
	MessageTypeUpdate                 = pglogrepl.MessageTypeUpdate
	MessageTypeDelete                 = pglogrepl.MessageTypeDelete
	MessageTypeTruncate               = pglogrepl.MessageTypeTruncate
	MessageTypeBegin                  = pglogrepl.MessageTypeBegin
	MessageTypeCommit                 = pglogrepl.MessageTypeCommit
	MessageTypeOrigin                 = pglogrepl.MessageTypeOrigin
	MessageTypeLogicalDecodingMessage = pglogrepl.MessageTypeMessage
)

// Message is the interface for all replication messages.
type Message = pglogrepl.Message

// Decoder decodes pgoutput protocol messages.
type Decoder struct {
	relationCache *RelationCache
	typeCache     map[uint32]string // OID -> type name for custom types
}

// NewDecoder creates a decoder.
func NewDecoder(cache *RelationCache) *Decoder {
	return &Decoder{
		relationCache: cache,
		typeCache:     make(map[uint32]string),
	}
}

// Decode parses a WAL data message.
// Returns the decoded message and whether it's a transactional message.
func (d *Decoder) Decode(data []byte) (Message, error) {
	msg, err := pglogrepl.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}

	switch m := msg.(type) {
	case *pglogrepl.RelationMessage:
		// Update relation cache
		rel := FromMessage(m)
		d.relationCache.Set(m.RelationID, rel)
		return m, nil

	case *pglogrepl.TypeMessage:
		// Cache custom type names
		d.typeCache[m.DataType] = m.Name
		return m, nil

	case *pglogrepl.InsertMessage:
		return m, nil

	case *pglogrepl.UpdateMessage:
		return m, nil

	case *pglogrepl.DeleteMessage:
		return m, nil

	case *pglogrepl.TruncateMessage:
		// TRUNCATE is not supported in v1
		return nil, fmt.Errorf("TRUNCATE not supported")

	case *pglogrepl.BeginMessage:
		return m, nil

	case *pglogrepl.CommitMessage:
		return m, nil

	default:
		return nil, fmt.Errorf("unknown message type: %T", msg)
	}
}

// DecodeTuple decodes tuple data into column values.
// Handles the three states: value, null, unchanged-TOAST.
func (d *Decoder) DecodeTuple(rel *Relation, tuple *pglogrepl.TupleData) ([]TupleValue, error) {
	if tuple == nil {
		return nil, nil
	}

	if len(tuple.Columns) > len(rel.Columns) {
		return nil, fmt.Errorf("tuple has %d columns, relation has %d", 
			len(tuple.Columns), len(rel.Columns))
	}

	values := make([]TupleValue, len(tuple.Columns))
	for i, col := range tuple.Columns {
		relCol := rel.Columns[i]
		values[i] = TupleValue{
			Column: relCol,
			State:  d.decodeColumnState(col),
			Value:  col.Data,
		}
	}

	return values, nil
}

// decodeColumnState determines the column state from the data type discriminator.
func (d *Decoder) decodeColumnState(col *pglogrepl.TupleDataColumn) ColumnState {
	switch col.DataType {
	case pglogrepl.TupleDataTypeNull:
		return ColumnStateNull
	case pglogrepl.TupleDataTypeToast:
		return ColumnStateUnchangedToast
	case pglogrepl.TupleDataTypeText:
		return ColumnStateValue
	case pglogrepl.TupleDataTypeBinary:
		return ColumnStateValue // Binary format (not used by pgoutput v1)
	default:
		return ColumnStateAbsent
	}
}

// TupleValue represents a decoded column value.
type TupleValue struct {
	Column *Column
	State  ColumnState
	Value  []byte // Text-encoded value, nil for null/unchanged
}

// ColumnState represents the state of a column in a CDC event.
type ColumnState string

const (
	ColumnStateValue          ColumnState = "value"
	ColumnStateNull           ColumnState = "null"
	ColumnStateUnchangedToast ColumnState = "unchanged_toast"
	ColumnStateAbsent         ColumnState = "absent"
)

// XLogData wraps pglogrepl.XLogData with type safety.
type XLogData struct {
	WALStart     LSN
	ServerWALEnd LSN
	ServerTime   time.Time
	Message      Message // Decoded message
}

// ParseXLogData parses raw XLogData and decodes the message.
func (d *Decoder) ParseXLogData(data []byte) (*XLogData, error) {
	xld, err := pglogrepl.ParseXLogData(data)
	if err != nil {
		return nil, fmt.Errorf("parse xlog data: %w", err)
	}

	msg, err := d.Decode(xld.WALData)
	if err != nil {
		return nil, fmt.Errorf("decode message: %w", err)
	}

	return &XLogData{
		WALStart:     xld.WALStart,
		ServerWALEnd: xld.ServerWALEnd,
		ServerTime:   xld.ServerTime,
		Message:      msg,
	}, nil
}

// ParseKeepalive parses a primary keepalive message.
func ParseKeepalive(data []byte) (pglogrepl.PrimaryKeepaliveMessage, error) {
	return pglogrepl.ParsePrimaryKeepaliveMessage(data)
}

// IsEndTimeline checks if the error indicates end of timeline.
func IsEndTimeline(err error) (int64, LSN, bool) {
	return pglogrepl.IsErrEndTimeline(err)
}

// MarshalLSN converts LSN to bytes for network transmission.
func MarshalLSN(lsn LSN) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(lsn))
	return buf
}

// UnmarshalLSN parses LSN from bytes.
func UnmarshalLSN(data []byte) LSN {
	if len(data) != 8 {
		return 0
	}
	return LSN(binary.BigEndian.Uint64(data))
}