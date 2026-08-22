package replication

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/jackc/pglogrepl"
)

// RelationCache caches relation metadata for decoding.
// Keyed by relation OID; replaced when new Relation message arrives.
type RelationCache struct {
	mu        sync.RWMutex
	relations map[uint32]*Relation
}

// Relation represents a table's schema at a point in time.
type Relation struct {
	OID             uint32
	Namespace       string
	Name            string
	ReplicaIdentity ReplicaIdentity
	Columns         []*Column
	Fingerprint     string // Hash of column definitions
}

// Column describes a column in a relation.
type Column struct {
	Name     string
	TypeOID  uint32
	Nullable bool
	Position int
	IsKey    bool // Part of replica identity
}

// ReplicaIdentity represents the PostgreSQL replica identity setting.
type ReplicaIdentity uint8

const (
	ReplicaIdentityDefault ReplicaIdentity = 'd'
	ReplicaIdentityNothing ReplicaIdentity = 'n'
	ReplicaIdentityFull    ReplicaIdentity = 'f'
	ReplicaIdentityIndex   ReplicaIdentity = 'i'
)

// NewRelationCache creates an empty cache.
func NewRelationCache() *RelationCache {
	return &RelationCache{
		relations: make(map[uint32]*Relation),
	}
}

// Get retrieves a relation by OID.
func (c *RelationCache) Get(oid uint32) (*Relation, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rel, ok := c.relations[oid]
	return rel, ok
}

// Set stores a relation, replacing any existing entry.
func (c *RelationCache) Set(oid uint32, rel *Relation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.relations[oid] = rel
}

// Delete removes a relation from the cache.
func (c *RelationCache) Delete(oid uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.relations, oid)
}

// Clear removes all relations.
func (c *RelationCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.relations = make(map[uint32]*Relation)
}

// FromMessage converts a pglogrepl RelationMessage to a Relation.
func FromMessage(msg *pglogrepl.RelationMessage) *Relation {
	rel := &Relation{
		OID:             msg.RelationID,
		Namespace:       msg.Namespace,
		Name:            msg.RelationName,
		ReplicaIdentity: ReplicaIdentity(msg.ReplicaIdentity),
		Columns:         make([]*Column, len(msg.Columns)),
	}

	for i, col := range msg.Columns {
		rel.Columns[i] = &Column{
			Name:     col.Name,
			TypeOID:  col.DataType,
			Nullable: col.Flags&1 == 0, // 1 = NOT NULL
			Position: i + 1,
			IsKey:    col.Flags&2 != 0, // 2 = part of key
		}
	}

	rel.Fingerprint = computeFingerprint(rel)
	return rel
}

// computeFingerprint computes a stable fingerprint for schema versioning.
func computeFingerprint(rel *Relation) string {
	h := sha256.New()
	h.Write([]byte(rel.Namespace))
	h.Write([]byte(rel.Name))
	for _, col := range rel.Columns {
		_, _ = fmt.Fprintf(h, "%s:%d:%t:%d;", col.Name, col.TypeOID, col.Nullable, col.Position)
	}
	return fmt.Sprintf("%x", h.Sum(nil)[:16]) // 32 hex chars
}

// ValidateReplicaIdentity checks if the replica identity is usable.
// Returns error for NOTHING which cannot decode UPDATE/DELETE.
func (r *Relation) ValidateReplicaIdentity() error {
	if r.ReplicaIdentity == ReplicaIdentityNothing {
		return fmt.Errorf("table %s.%s has REPLICA IDENTITY NOTHING; cannot decode UPDATE/DELETE",
			r.Namespace, r.Name)
	}
	return nil
}

// KeyColumns returns the columns that form the replica identity key.
func (r *Relation) KeyColumns() []*Column {
	var keys []*Column
	for _, col := range r.Columns {
		if col.IsKey {
			keys = append(keys, col)
		}
	}
	return keys
}
