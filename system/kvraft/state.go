// SPDX-License-Identifier: MPL-2.0

package kvraft

import (
	"hash/fnv"
	"sync"
)

const shardCount = 16

// shardKey is the per-shard key — concatenation of space + nul + key gives
// uniqueness across spaces.
type shardKey struct {
	Space string
	Key   string
}

// entry holds one stored value plus FSM metadata.
type entry struct {
	Value     []byte
	TTL       int64  // ms epoch; 0 = no TTL
	AppliedAt uint64 // raft log index
}

// shard is one slice of the keyspace.
type shard struct {
	entries map[shardKey]*entry
	mu      sync.RWMutex
}

// state is the in-memory store. All write paths go through FSM Apply
// (single-threaded), so locking is needed only for concurrent reads.
type state struct {
	shards [shardCount]shard
	count  int64
}

func newState() *state {
	s := &state{}
	for i := range s.shards {
		s.shards[i].entries = make(map[shardKey]*entry)
	}
	return s
}

func shardFor(sk shardKey) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(sk.Space))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(sk.Key))
	return int(h.Sum32() % shardCount)
}

// get returns (value-copy, version, ttl, true) or (nil, 0, 0, false).
func (s *state) get(sk shardKey, nowMs int64) ([]byte, uint64, int64, bool) {
	sh := &s.shards[shardFor(sk)]
	sh.mu.RLock()
	e, ok := sh.entries[sk]
	if !ok {
		sh.mu.RUnlock()
		return nil, 0, 0, false
	}
	if e.TTL > 0 && nowMs >= e.TTL {
		sh.mu.RUnlock()
		return nil, 0, 0, false
	}
	value := make([]byte, len(e.Value))
	copy(value, e.Value)
	version := e.AppliedAt
	ttl := e.TTL
	sh.mu.RUnlock()
	return value, version, ttl, true
}

// has returns whether a key exists (and is not expired).
func (s *state) has(sk shardKey, nowMs int64) bool {
	_, _, _, ok := s.get(sk, nowMs) //nolint:dogsled // public Get returns 4 values; this consumer only needs the boolean
	return ok
}

// put stores value unconditionally. Caller is FSM Apply (single-threaded);
// this still takes per-shard write lock to coordinate with concurrent reads.
func (s *state) put(sk shardKey, value []byte, ttl int64, appliedAt uint64) {
	sh := &s.shards[shardFor(sk)]
	sh.mu.Lock()
	if _, existed := sh.entries[sk]; !existed {
		s.count++
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	sh.entries[sk] = &entry{Value: cp, TTL: ttl, AppliedAt: appliedAt}
	sh.mu.Unlock()
}

// delete removes a key. Returns true if it existed.
func (s *state) delete(sk shardKey) bool {
	sh := &s.shards[shardFor(sk)]
	sh.mu.Lock()
	_, existed := sh.entries[sk]
	if existed {
		delete(sh.entries, sk)
		s.count--
	}
	sh.mu.Unlock()
	return existed
}

// reapExpired walks all shards and removes entries with `TTL > 0 && TTL <= now`.
// Returns the count removed. FSM-Apply path.
func (s *state) reapExpired(nowMs int64) int {
	removed := 0
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		for sk, e := range sh.entries {
			if e.TTL > 0 && e.TTL <= nowMs {
				delete(sh.entries, sk)
				removed++
			}
		}
		sh.mu.Unlock()
	}
	s.count -= int64(removed)
	return removed
}

// scanPair is the deterministic ordered-row holder used by scan. Defined at
// package scope so the sort helper can name it cleanly.
type scanPair struct {
	key     string
	value   []byte
	version uint64
}

// scan walks live entries in `space` whose key is in [start, end).
// `end == ""` means "no upper bound". Snapshot semantics.
func (s *state) scan(space, start, end string, nowMs int64, fn func(key string, value []byte, version uint64) bool) {
	var matches []scanPair
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.RLock()
		for sk, e := range sh.entries {
			if sk.Space != space {
				continue
			}
			if e.TTL > 0 && nowMs >= e.TTL {
				continue
			}
			if start != "" && sk.Key < start {
				continue
			}
			if end != "" && sk.Key >= end {
				continue
			}
			cp := make([]byte, len(e.Value))
			copy(cp, e.Value)
			matches = append(matches, scanPair{sk.Key, cp, e.AppliedAt})
		}
		sh.mu.RUnlock()
	}
	sortScanPairs(matches)
	for _, p := range matches {
		if !fn(p.key, p.value, p.version) {
			return
		}
	}
}

// sortScanPairs uses insertion sort — typical scan results are small, and
// pulling sort.Slice would add an alloc per scan call.
func sortScanPairs(p []scanPair) {
	for i := 1; i < len(p); i++ {
		j := i
		for j > 0 && p[j-1].key > p[j].key {
			p[j-1], p[j] = p[j], p[j-1]
			j--
		}
	}
}

// snapshot returns all entries as a deterministic slice for FSM persistence.
func (s *state) snapshot() []snapshotEntry {
	for i := range s.shards {
		s.shards[i].mu.RLock()
	}
	var out []snapshotEntry
	for i := range s.shards {
		for sk, e := range s.shards[i].entries {
			out = append(out, snapshotEntry{
				Space:     sk.Space,
				Key:       sk.Key,
				Value:     e.Value,
				TTL:       e.TTL,
				AppliedAt: e.AppliedAt,
			})
		}
	}
	for i := range s.shards {
		s.shards[i].mu.RUnlock()
	}
	return out
}

// restore replaces all state from a snapshot. Used on FSM Restore.
func (s *state) restore(entries []snapshotEntry) {
	for i := range s.shards {
		s.shards[i].mu.Lock()
		s.shards[i].entries = make(map[shardKey]*entry, 16)
	}
	s.count = 0
	for _, se := range entries {
		sk := shardKey{Space: se.Space, Key: se.Key}
		sh := &s.shards[shardFor(sk)]
		sh.entries[sk] = &entry{
			Value:     se.Value,
			TTL:       se.TTL,
			AppliedAt: se.AppliedAt,
		}
		s.count++
	}
	for i := range s.shards {
		s.shards[i].mu.Unlock()
	}
}

// Len returns the live (non-expired) entry count. Approximate during
// concurrent applies; exact under FSM-Apply quiescence.
func (s *state) Len() int { return int(s.count) }

// snapshotEntry is the serializable form of a single entry.
type snapshotEntry struct {
	Space     string `codec:"s"`
	Key       string `codec:"k"`
	Value     []byte `codec:"v,omitempty"`
	TTL       int64  `codec:"t,omitempty"`
	AppliedAt uint64 `codec:"a"`
}
