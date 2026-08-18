package replication

import (
	"testing"

	"github.com/jackc/pglogrepl"
)

func TestColumnStateDecoding(t *testing.T) {
	tests := []struct {
		name     string
		dataType uint8
		want     ColumnState
	}{
		{"null", pglogrepl.TupleDataTypeNull, ColumnStateNull},
		{"toast", pglogrepl.TupleDataTypeToast, ColumnStateUnchangedToast},
		{"text", pglogrepl.TupleDataTypeText, ColumnStateValue},
		{"binary", pglogrepl.TupleDataTypeBinary, ColumnStateValue},
		{"unknown", 255, ColumnStateAbsent},
	}

	decoder := NewDecoder(NewRelationCache())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := &pglogrepl.TupleDataColumn{DataType: tt.dataType}
			got := decoder.decodeColumnState(col)
			if got != tt.want {
				t.Errorf("decodeColumnState(%c) = %v, want %v", tt.dataType, got, tt.want)
			}
		})
	}
}

func TestRelationCache(t *testing.T) {
	cache := NewRelationCache()

	// Test set and get
	rel := &Relation{
		OID:       12345,
		Namespace: "public",
		Name:      "users",
	}
	cache.Set(12345, rel)

	got, ok := cache.Get(12345)
	if !ok {
		t.Fatal("relation not found")
	}
	if got.Name != "users" {
		t.Errorf("Name = %q, want %q", got.Name, "users")
	}

	// Test replace
	rel2 := &Relation{
		OID:       12345,
		Namespace: "public",
		Name:      "users_v2",
	}
	cache.Set(12345, rel2)

	got, _ = cache.Get(12345)
	if got.Name != "users_v2" {
		t.Errorf("Name = %q, want %q (replaced)", got.Name, "users_v2")
	}

	// Test delete
	cache.Delete(12345)
	_, ok = cache.Get(12345)
	if ok {
		t.Error("relation should be deleted")
	}
}

func TestReplicaIdentityValidation(t *testing.T) {
	tests := []struct {
		name     string
		identity ReplicaIdentity
		wantErr  bool
	}{
		{"default", ReplicaIdentityDefault, false},
		{"full", ReplicaIdentityFull, false},
		{"index", ReplicaIdentityIndex, false},
		{"nothing", ReplicaIdentityNothing, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := &Relation{
				Namespace:       "public",
				Name:            "test",
				ReplicaIdentity: tt.identity,
			}
			err := rel.ValidateReplicaIdentity()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateReplicaIdentity() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestKeyColumns(t *testing.T) {
	rel := &Relation{
		Columns: []*Column{
			{Name: "id", Position: 1, IsKey: true},
			{Name: "name", Position: 2, IsKey: false},
			{Name: "email", Position: 3, IsKey: true},
		},
	}

	keys := rel.KeyColumns()
	if len(keys) != 2 {
		t.Errorf("KeyColumns() returned %d, want 2", len(keys))
	}
	if keys[0].Name != "id" || keys[1].Name != "email" {
		t.Errorf("KeyColumns() = %v, want [id, email]", keys)
	}
}

func TestFingerprint(t *testing.T) {
	rel1 := &Relation{
		Namespace: "public",
		Name:      "users",
		Columns: []*Column{
			{Name: "id", TypeOID: 23, Position: 1},
			{Name: "name", TypeOID: 25, Position: 2},
		},
	}

	rel2 := &Relation{
		Namespace: "public",
		Name:      "users",
		Columns: []*Column{
			{Name: "id", TypeOID: 23, Position: 1},
			{Name: "name", TypeOID: 25, Position: 2},
		},
	}

	rel3 := &Relation{
		Namespace: "public",
		Name:      "users",
		Columns: []*Column{
			{Name: "id", TypeOID: 23, Position: 1},
			{Name: "email", TypeOID: 25, Position: 2}, // Different column
		},
	}

	fp1 := computeFingerprint(rel1)
	fp2 := computeFingerprint(rel2)
	fp3 := computeFingerprint(rel3)

	if fp1 != fp2 {
		t.Error("same schema should have same fingerprint")
	}
	if fp1 == fp3 {
		t.Error("different schema should have different fingerprint")
	}
}