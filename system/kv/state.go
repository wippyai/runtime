// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"maps"
	"strings"

	"github.com/cespare/xxhash/v2"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// state holds the mutable KV data, accessed only from the event loop goroutine.
type state struct {
	published  *stateSnapshot
	dirty      map[string]struct{}
	entries    map[string]*entry
	leases     map[kvapi.LeaseID]*leaseState
	version    kvapi.Version // global monotonic revision counter
	applyIndex uint64        // raft log index of the command currently applying
}

// entry is a single key-value pair with metadata.
type entry struct {
	key     string
	leaseID kvapi.LeaseID
	value   []byte
	version kvapi.Version
	epoch   uint64 // raft log index at which this entry was last written
}

// leaseState tracks a lease and its attached keys.
type leaseState struct {
	keys        map[string]struct{} // keys bound to this lease
	id          kvapi.LeaseID
	ttl         int64 // original TTL in milliseconds
	expiresAtMs int64 // absolute deadline (unix ms), replicated so a new leader honors it across a leadership change instead of resetting the clock
}

func newState() *state {
	return &state{
		entries: make(map[string]*entry),
		leases:  make(map[kvapi.LeaseID]*leaseState),
	}
}

// nextVersion increments and returns the global version.
func (s *state) nextVersion() kvapi.Version {
	s.version++
	return s.version
}

// get returns an entry by key, or nil if not found.
func (s *state) get(key string) *entry {
	return s.entries[key]
}

// set stores a value unconditionally. Returns the previous entry (nil if new)
// and the new version.
func (s *state) set(key string, value []byte, leaseID kvapi.LeaseID) (*entry, kvapi.Version) {
	ver := s.nextVersion()
	prev := s.entries[key]

	// Detach from old lease if the key was bound to a different one
	if prev != nil && prev.leaseID != "" && prev.leaseID != leaseID {
		if ls, ok := s.leases[prev.leaseID]; ok {
			delete(ls.keys, key)
		}
	}

	e := &entry{
		key:     key,
		value:   value,
		version: ver,
		leaseID: leaseID,
		epoch:   s.applyIndex,
	}
	s.entries[key] = e
	s.markDirty(key)

	// Attach to lease
	if leaseID != "" {
		if ls, ok := s.leases[leaseID]; ok {
			ls.keys[key] = struct{}{}
		}
	}

	return prev, ver
}

// del removes a key. Returns the removed entry or nil.
func (s *state) del(key string) *entry {
	e, ok := s.entries[key]
	if !ok {
		return nil
	}

	delete(s.entries, key)
	s.markDirty(key)

	// Detach from lease
	if e.leaseID != "" {
		if ls, ok := s.leases[e.leaseID]; ok {
			delete(ls.keys, key)
		}
	}

	return e
}

// setIfAbsent stores only if key doesn't exist. Returns (version, true) if
// stored, or (existing version, false) if key exists.
func (s *state) setIfAbsent(key string, value []byte, leaseID kvapi.LeaseID) (kvapi.Version, bool) {
	if existing, ok := s.entries[key]; ok {
		return existing.version, false
	}

	_, ver := s.set(key, value, leaseID)
	return ver, true
}

// cas updates only if the current version matches expected.
// Returns (new version, true) on success, (actual version, false) on mismatch.
func (s *state) cas(key string, expect kvapi.Version, value []byte) (kvapi.Version, bool) {
	existing := s.entries[key]

	var actualVersion kvapi.Version
	if existing != nil {
		actualVersion = existing.version
	}

	if actualVersion != expect {
		return actualVersion, false
	}

	leaseID := kvapi.LeaseID("")
	if existing != nil {
		leaseID = existing.leaseID
	}

	_, ver := s.set(key, value, leaseID)
	return ver, true
}

// compareAndDelete removes key only if its current version matches expect.
// Returns (deleted, existed).
func (s *state) compareAndDelete(key string, expect kvapi.Version) (deleted, existed bool) {
	e, ok := s.entries[key]
	if !ok {
		return false, false
	}
	if e.version != expect {
		return false, true
	}
	s.del(key)
	return true, true
}

// condHolds evaluates a txn precondition against the current entry (nil=absent).
func condHolds(cond kvapi.TxnCond, expect kvapi.Version, e *entry) bool {
	switch cond {
	case kvapi.CondAny:
		return true
	case kvapi.CondAbsent:
		return e == nil
	case kvapi.CondExists:
		return e != nil
	case kvapi.CondVersion:
		return e != nil && e.version == expect
	default:
		return false
	}
}

// addLease registers a new lease.
func (s *state) addLease(id kvapi.LeaseID, ttlMs, expiresAtMs int64) {
	s.leases[id] = &leaseState{
		id:          id,
		ttl:         ttlMs,
		expiresAtMs: expiresAtMs,
		keys:        make(map[string]struct{}),
	}
}

// removeLease removes a lease and returns the keys that were bound to it.
func (s *state) removeLease(id kvapi.LeaseID) []string {
	ls, ok := s.leases[id]
	if !ok {
		return nil
	}

	keys := make([]string, 0, len(ls.keys))
	for k := range ls.keys {
		keys = append(keys, k)
	}

	delete(s.leases, id)
	return keys
}

// Snapshot publication copies only shards containing changed keys. Unchanged
// maps and entries remain immutable and are shared with older readers.
const snapshotShards = 256

func snapshotShard(key string) uint64 { return xxhash.Sum64String(key) % snapshotShards }

func (s *state) markDirty(key string) {
	if s.dirty == nil {
		s.dirty = make(map[string]struct{})
	}
	s.dirty[key] = struct{}{}
}

func (s *state) snapshot() *stateSnapshot {
	snap := &stateSnapshot{index: s.applyIndex, version: s.version}
	if s.published != nil {
		snap.shards = s.published.shards
	} else {
		for key := range s.entries {
			s.markDirty(key)
		}
	}
	var cloned [snapshotShards]bool
	for key := range s.dirty {
		shard := snapshotShard(key)
		if !cloned[shard] {
			snap.shards[shard] = maps.Clone(snap.shards[shard])
			if snap.shards[shard] == nil {
				snap.shards[shard] = make(map[string]*kvapi.Entry)
			}
			cloned[shard] = true
		}
		if e := s.entries[key]; e != nil {
			snap.shards[shard][key] = &kvapi.Entry{Key: e.key, Value: copyBytes(e.value), Version: e.version, LeaseID: e.leaseID, Epoch: e.epoch}
		} else {
			delete(snap.shards[shard], key)
		}
	}
	clear(s.dirty)
	s.published = snap
	return snap
}

// stateSnapshot carries the applied KV index with precisely the entries that
// were published at that index. It never samples Raft's possibly newer commit.
type stateSnapshot struct {
	shards  [snapshotShards]map[string]*kvapi.Entry
	version uint64
	index   uint64
}

func (s *stateSnapshot) getMany(keys []string) (map[string]kvapi.Entry, error) {
	if s == nil {
		return nil, kvapi.ErrKVClosed
	}
	out := make(map[string]kvapi.Entry, len(keys))
	for _, key := range keys {
		if e := s.get(key); e != nil {
			out[key] = *e
		}
	}
	return out, nil
}

func (s *stateSnapshot) get(key string) *kvapi.Entry {
	if s == nil {
		return nil
	}
	e := s.shards[snapshotShard(key)][key]
	if e == nil {
		return nil
	}
	out := *e
	out.Value = copyBytes(e.Value)
	return &out
}

func (s *stateSnapshot) scan(prefix string, fn func(kvapi.Entry) bool) {
	if s == nil {
		return
	}
	for _, shard := range s.shards {
		for k, e := range shard {
			if prefix != "" && !strings.HasPrefix(k, prefix) {
				continue
			}
			out := *e
			out.Value = copyBytes(e.Value)
			if !fn(out) {
				return
			}
		}
	}
}

func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}
