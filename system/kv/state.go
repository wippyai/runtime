// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"strings"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// state holds the mutable KV data, accessed only from the event loop goroutine.
type state struct {
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
	keys map[string]struct{} // keys bound to this lease
	id   kvapi.LeaseID
	ttl  int64 // original TTL in milliseconds
	// expiresAt is managed by the lease heap in the service
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
func (s *state) addLease(id kvapi.LeaseID, ttlMs int64) {
	s.leases[id] = &leaseState{
		id:   id,
		ttl:  ttlMs,
		keys: make(map[string]struct{}),
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

// snapshot builds an immutable copy of all entries for lock-free reads.
func (s *state) snapshot() *stateSnapshot {
	snap := &stateSnapshot{
		entries: make(map[string]*kvapi.Entry, len(s.entries)),
	}
	for k, e := range s.entries {
		snap.entries[k] = &kvapi.Entry{
			Key:     e.key,
			Value:   copyBytes(e.value),
			Version: e.version,
			LeaseID: e.leaseID,
			Epoch:   e.epoch,
		}
	}
	return snap
}

// stateSnapshot is an immutable point-in-time view for lock-free reads.
type stateSnapshot struct {
	entries map[string]*kvapi.Entry
}

func (s *stateSnapshot) get(key string) *kvapi.Entry {
	if s == nil {
		return nil
	}
	return s.entries[key]
}

func (s *stateSnapshot) scan(prefix string, fn func(kvapi.Entry) bool) {
	if s == nil {
		return
	}
	for k, e := range s.entries {
		if prefix != "" && !strings.HasPrefix(k, prefix) {
			continue
		}
		if !fn(*e) {
			return
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
