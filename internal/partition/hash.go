package partition

import (
	"encoding/binary"
	"hash/fnv"
	"sort"
)

// HashVersion is the current partition hash version.
const HashVersion = 1

// Hasher computes partition assignments.
type Hasher struct {
	partitionCount int
}

// NewHasher creates a hasher for the given partition count.
func NewHasher(partitionCount int) *Hasher {
	return &Hasher{partitionCount: partitionCount}
}

// Partition returns the partition for a key.
func (h *Hasher) Partition(key string) int {
	if h.partitionCount <= 0 {
		return 0
	}
	return int(h.hash(key) % uint32(h.partitionCount))
}

// PartitionBytes returns the partition for key bytes.
func (h *Hasher) PartitionBytes(key []byte) int {
	if h.partitionCount <= 0 {
		return 0
	}
	return int(h.hashBytes(key) % uint32(h.partitionCount))
}

// hash computes FNV-1a hash of a string.
func (h *Hasher) hash(key string) uint32 {
	return h.hashBytes([]byte(key))
}

// hashBytes computes FNV-1a hash of bytes.
func (h *Hasher) hashBytes(key []byte) uint32 {
	h2 := fnv.New32a()
	_, _ = h2.Write(key)
	return h2.Sum32()
}

// PartitionCount returns the partition count.
func (h *Hasher) PartitionCount() int {
	return h.partitionCount
}

// AssignPartitions divides partitions among members.
// Returns map of member ID to assigned partitions.
func AssignPartitions(partitions int, members []string) map[string][]int {
	if len(members) == 0 || partitions <= 0 {
		return nil
	}

	sort.Strings(members) // Deterministic assignment
	assignment := make(map[string][]int, len(members))
	for _, m := range members {
		assignment[m] = []int{}
	}

	for p := 0; p < partitions; p++ {
		member := members[p%len(members)]
		assignment[member] = append(assignment[member], p)
	}

	return assignment
}

// NormalizeKey normalizes an event key for consistent hashing.
// Handles composite keys by sorting and concatenating.
func NormalizeKey(key map[string]any) string {
	if len(key) == 0 {
		return ""
	}

	keys := make([]string, 0, len(key))
	for k := range key {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf []byte
	for _, k := range keys {
		buf = append(buf, k...)
		buf = append(buf, '=')
		switch v := key[k].(type) {
		case string:
			buf = append(buf, v...)
		case []byte:
			buf = append(buf, v...)
		default:
			// Fallback to binary encoding
			b := make([]byte, 8)
			binary.BigEndian.PutUint64(b, uint64(len(k)))
			buf = append(buf, b...)
		}
		buf = append(buf, ';')
	}

	return string(buf)
}