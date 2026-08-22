package capture

import (
	"testing"
	"time"

	"github.com/tyagiquamar/relaydb/internal/config"
	"github.com/tyagiquamar/relaydb/internal/eventstore"
	"github.com/tyagiquamar/relaydb/internal/replication"
)

func TestTupleToMap(t *testing.T) {
	rel := &replication.Relation{
		Columns: []*replication.Column{
			{Name: "id", Position: 1},
			{Name: "name", Position: 2},
		},
	}

	values := []replication.TupleValue{
		{Column: rel.Columns[0], State: replication.ColumnStateValue, Value: []byte("123")},
		{Column: rel.Columns[1], State: replication.ColumnStateNull, Value: nil},
	}

	result := tupleToMap(values)

	if len(result) != 2 {
		t.Errorf("tupleToMap returned %d columns, want 2", len(result))
	}

	if result["id"].State != eventstore.ColumnStateValue {
		t.Errorf("id state = %v, want value", result["id"].State)
	}
	if string(result["id"].Value) != `"123"` {
		t.Errorf("id value = %q, want JSON string %q", result["id"].Value, `"123"`)
	}

	if result["name"].State != eventstore.ColumnStateNull {
		t.Errorf("name state = %v, want null", result["name"].State)
	}
}

func TestExtractKey(t *testing.T) {
	rel := &replication.Relation{
		Columns: []*replication.Column{
			{Name: "id", Position: 1, IsKey: true},
			{Name: "name", Position: 2, IsKey: false},
		},
	}

	after := map[string]eventstore.ColumnValue{
		"id":   {State: eventstore.ColumnStateValue, Value: []byte("123")},
		"name": {State: eventstore.ColumnStateValue, Value: []byte("test")},
	}

	key := extractKey(rel, after, nil)

	if len(key) != 1 {
		t.Errorf("extractKey returned %d keys, want 1", len(key))
	}
	if key["id"] != "123" {
		t.Errorf("key[id] = %v, want %v", key["id"], "123")
	}
}

func TestEstimateBytes(t *testing.T) {
	e := &eventstore.Event{
		After: map[string]eventstore.ColumnValue{
			"data": {Value: make([]byte, 1000)},
		},
		Before: map[string]eventstore.ColumnValue{
			"old": {Value: make([]byte, 500)},
		},
	}

	size := estimateBytes(e)
	if size < 1700 || size > 2000 {
		t.Errorf("estimateBytes() = %d, want ~1700-2000", size)
	}
}

func TestServiceCreation(t *testing.T) {
	// Verify service can be created with nil pool (for unit tests)
	cfg := configForTest()
	svc := NewService(cfg, nil)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.ownerID == "" {
		t.Error("ownerID should be set")
	}
}

func configForTest() config.Config {
	return config.Config{
		CaptureOwnerID:            "test-owner",
		LeaseDuration:             30 * time.Second,
		HeartbeatInterval:         10 * time.Second,
		MaxTransactionBufferBytes: 1024 * 1024,
		MaxEventBatchSize:         1000,
		MaxInflightTransactions:   10,
	}
}
