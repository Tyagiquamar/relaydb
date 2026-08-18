package replication

import (
	"fmt"

	"github.com/jackc/pglogrepl"
)

// LSN is a PostgreSQL Log Sequence Number.
type LSN = pglogrepl.LSN

// ParseLSN parses a string LSN.
func ParseLSN(s string) (LSN, error) {
	return pglogrepl.ParseLSN(s)
}

// Position represents a canonical stream position.
// This is the ordering primitive for all RelayDB operations.
type Position struct {
	CommitEndLSN  LSN `json:"commit_end_lsn"`
	SequenceNumber int `json:"sequence_number"`
}

// Compare returns -1, 0, or 1 comparing to another position.
func (p Position) Compare(other Position) int {
	if p.CommitEndLSN < other.CommitEndLSN {
		return -1
	}
	if p.CommitEndLSN > other.CommitEndLSN {
		return 1
	}
	if p.SequenceNumber < other.SequenceNumber {
		return -1
	}
	if p.SequenceNumber > other.SequenceNumber {
		return 1
	}
	return 0
}

// IsZero returns true if this is the zero position.
func (p Position) IsZero() bool {
	return p.CommitEndLSN == 0 && p.SequenceNumber == 0
}

// String returns a string representation.
func (p Position) String() string {
	return fmt.Sprintf("%s:%d", p.CommitEndLSN, p.SequenceNumber)
}