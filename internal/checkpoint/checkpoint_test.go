package checkpoint

import (
	"testing"
	"time"

	"github.com/tyagiquamar/relaydb/internal/replication"
)

func TestPositionCompare(t *testing.T) {
	tests := []struct {
		name string
		a, b replication.Position
		want int
	}{
		{"equal", replication.Position{CommitEndLSN: 1000, SequenceNumber: 1}, replication.Position{CommitEndLSN: 1000, SequenceNumber: 1}, 0},
		{"lsn less", replication.Position{CommitEndLSN: 999, SequenceNumber: 5}, replication.Position{CommitEndLSN: 1000, SequenceNumber: 1}, -1},
		{"lsn greater", replication.Position{CommitEndLSN: 1001, SequenceNumber: 1}, replication.Position{CommitEndLSN: 1000, SequenceNumber: 5}, 1},
		{"seq less", replication.Position{CommitEndLSN: 1000, SequenceNumber: 1}, replication.Position{CommitEndLSN: 1000, SequenceNumber: 2}, -1},
		{"seq greater", replication.Position{CommitEndLSN: 1000, SequenceNumber: 3}, replication.Position{CommitEndLSN: 1000, SequenceNumber: 2}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Compare(tt.b); got != tt.want {
				t.Errorf("Compare() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPositionIsZero(t *testing.T) {
	zero := replication.Position{}
	if !zero.IsZero() {
		t.Error("zero position should be zero")
	}

	nonZero := replication.Position{CommitEndLSN: 1}
	if nonZero.IsZero() {
		t.Error("non-zero position should not be zero")
	}
}

func TestPositionString(t *testing.T) {
	p := replication.Position{CommitEndLSN: 268435456, SequenceNumber: 5} // 0x10000000
	s := p.String()
	if s == "" {
		t.Error("String() should not be empty")
	}
}

// Integration tests would go here with testcontainers
// For now, we test the structure
func TestManagerStructure(t *testing.T) {
	// Verify Manager can be created
	m := NewManager(nil)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestCheckpointFields(t *testing.T) {
	cp := &Checkpoint{
		SourceID:        "test-source",
		ReceivedLSN:     replication.LSN(1000),
		PersistedLSN:    replication.LSN(900),
		AcknowledgedLSN: replication.LSN(800),
		CaptureOwner:    "capture-1",
		OwnerGeneration: 5,
		LeaseExpiresAt:  time.Now().Add(time.Minute),
		UpdatedAt:       time.Now(),
	}

	if cp.SourceID != "test-source" {
		t.Error("SourceID mismatch")
	}
	// Lag is computed as received - persisted
	lag := int64(cp.ReceivedLSN) - int64(cp.PersistedLSN)
	if lag != 100 {
		t.Errorf("lag = %d, want 100", lag)
	}
}
