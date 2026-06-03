// SPDX-License-Identifier: MPL-2.0

// Package crdt implements an Optimized OR-Set Without Tombstones (ORSWOT)
// keyed by string with opaque []byte values. It is the generic gossip-CRDT
// substrate used by `system/eventualreg` (name → PID registrations).
//
// The data structure is deliberately generic: merge resolution touches
// only the per-replica dot (origin + counter) and wall-clock tiebreak, not
// the value bytes. Two replicas with identical (key, value, dot, wall,
// deleted) tuples produce identical shard hashes and converge after
// anti-entropy.
package crdt

import (
	"bytes"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/cespare/xxhash/v2"
)

// ShardCount is the number of shards used for digest hashing and parallel
// access. 64 shards × ~1500 keys/shard ≈ 100k keys with reasonable
// per-shard contention. Higher densities (1M keys / 64 shards = 15k each)
// still hold within milliseconds for full-shard scans.
const ShardCount = 64

// MergeOutcome reports what happened during a merge.
type MergeOutcome uint8

const (
	// MergeNoop means the incoming entry was older than what we already had.
	MergeNoop MergeOutcome = iota
	// MergeApplied means the incoming entry was newer and replaced the local one.
	MergeApplied
	// MergeWallTiebreak means dots were equal; wall-clock LWW resolved it.
	MergeWallTiebreak
	// MergeDeleteWins means the incoming tombstone won on full equality.
	MergeDeleteWins
)

// Entry is a single key→value record. Tombstones set Deleted=true and
// nil-out Value. The (Node, Counter) pair is the per-replica "dot" that
// drives merge resolution.
type Entry struct {
	Key     string
	Value   []byte
	Counter uint64 // monotonic per-origin
	Wall    int64  // ms epoch — LWW tiebreak only
	Node    uint32 // origin node, interned via State.nodeIDs
	Deleted bool
}

// shard holds a slice of the keyspace.
type shard struct {
	entries map[string]*Entry
	mu      sync.RWMutex
}

// State is the in-memory ORSWOT replica.
type State struct {
	// nodeIDs maps string nodeID → compact uint32 to keep entries small.
	// Read under cvMu; write also under cvMu.
	nodeIDs map[string]uint32
	// stringIDs is the reverse intern table.
	stringIDs []string
	// cv[node] is the highest counter we have seen from origin `node`.
	// Used for tombstone GC and to detect stale incoming entries.
	cv []uint64
	// shards holds the entry maps; one RWMutex per shard for parallel access.
	shards [ShardCount]shard
	// localCounter is our local Lamport counter. Other nodes' counters
	// live only in cv.
	localCounter atomic.Uint64
	// Stats — atomic so telemetry can read without locking.
	live      atomic.Int64
	tombstone atomic.Int64
	cvMu      sync.RWMutex
	// localNode is the interned ID of the local origin node.
	localNode uint32
}

// NewState constructs an empty State for the given local node string ID.
func NewState(localNodeID string) *State {
	s := &State{
		nodeIDs: make(map[string]uint32, 8),
	}
	for i := range s.shards {
		s.shards[i].entries = make(map[string]*Entry)
	}
	s.localNode = s.InternNode(localNodeID)
	return s
}

// InternNodeLocked allocates a compact ID for a string nodeID. Caller must
// hold cvMu (write).
func (s *State) internNodeLocked(node string) uint32 {
	if id, ok := s.nodeIDs[node]; ok {
		return id
	}
	id := uint32(len(s.stringIDs))
	s.nodeIDs[node] = id
	s.stringIDs = append(s.stringIDs, node)
	if int(id) >= len(s.cv) {
		grown := make([]uint64, id+8)
		copy(grown, s.cv)
		s.cv = grown
	}
	return id
}

// InternNode returns the compact ID for a string nodeID, allocating one if
// new. Safe for concurrent use.
func (s *State) InternNode(node string) uint32 {
	s.cvMu.Lock()
	defer s.cvMu.Unlock()
	return s.internNodeLocked(node)
}

// NodeString returns the string ID for a compact node, or "" if unknown.
func (s *State) NodeString(id uint32) string {
	s.cvMu.RLock()
	defer s.cvMu.RUnlock()
	if int(id) >= len(s.stringIDs) {
		return ""
	}
	return s.stringIDs[id]
}

// LocalNode returns the compact ID of the local origin.
func (s *State) LocalNode() uint32 { return s.localNode }

// ShardFor returns the shard index for a key. Used by callers that want to
// match the State's sharding (e.g. for batched ops).
func ShardFor(key string) int {
	return int(xxhash.Sum64String(key) % ShardCount)
}

// Lookup returns the live value bytes for a key, or (nil, false) if absent
// or tombstoned. The returned slice is owned by the State; callers must
// copy if they want to retain it past concurrent writes.
func (s *State) Lookup(key string) ([]byte, bool) {
	sh := &s.shards[ShardFor(key)]
	sh.mu.RLock()
	e, ok := sh.entries[key]
	sh.mu.RUnlock()
	if !ok || e.Deleted {
		return nil, false
	}
	return e.Value, true
}

// LookupEntry returns the full entry (including dot metadata) for a key.
// Returns (zero, false) if absent. Useful for KV exposing version numbers
// to callers.
func (s *State) LookupEntry(key string) (Entry, bool) {
	sh := &s.shards[ShardFor(key)]
	sh.mu.RLock()
	e, ok := sh.entries[key]
	sh.mu.RUnlock()
	if !ok || e.Deleted {
		return Entry{}, false
	}
	return *e, true
}

// Register creates a local registration with a fresh dot. Returns the entry
// applied (callers broadcast it via the delta queue) and whether the
// registration was accepted. If the key is already held by a DIFFERENT
// value (bytes-unequal) and the existing entry is live, returns
// (existingEntry, false) — caller decides whether to overwrite.
func (s *State) Register(key string, value []byte, wallMs int64) (Entry, bool) {
	sh := &s.shards[ShardFor(key)]
	sh.mu.Lock()
	if cur, ok := sh.entries[key]; ok && !cur.Deleted && !bytes.Equal(cur.Value, value) {
		ret := *cur
		sh.mu.Unlock()
		return ret, false
	}
	counter := s.localCounter.Add(1)
	e := &Entry{
		Key:     key,
		Value:   cloneBytes(value),
		Node:    s.localNode,
		Counter: counter,
		Wall:    wallMs,
	}
	prev, existed := sh.entries[key]
	sh.entries[key] = e
	sh.mu.Unlock()

	s.bumpCV(s.localNode, counter)
	if existed && prev.Deleted {
		s.tombstone.Add(-1)
		s.live.Add(1)
	} else if !existed {
		s.live.Add(1)
	}
	return *e, true
}

// Overwrite is like Register but does NOT check for value mismatch — it
// always succeeds and replaces the prior entry. Used by KV Put which has
// last-writer-wins semantics regardless of value identity.
func (s *State) Overwrite(key string, value []byte, wallMs int64) Entry {
	sh := &s.shards[ShardFor(key)]
	sh.mu.Lock()
	counter := s.localCounter.Add(1)
	e := &Entry{
		Key:     key,
		Value:   cloneBytes(value),
		Node:    s.localNode,
		Counter: counter,
		Wall:    wallMs,
	}
	prev, existed := sh.entries[key]
	sh.entries[key] = e
	sh.mu.Unlock()

	s.bumpCV(s.localNode, counter)
	if existed && prev.Deleted {
		s.tombstone.Add(-1)
		s.live.Add(1)
	} else if !existed {
		s.live.Add(1)
	}
	return *e
}

// Unregister tombstones a key. Returns the tombstone entry that callers
// should broadcast, or (zero, false) if the key wasn't held.
func (s *State) Unregister(key string, wallMs int64) (Entry, bool) {
	sh := &s.shards[ShardFor(key)]
	sh.mu.Lock()
	cur, ok := sh.entries[key]
	if !ok || cur.Deleted {
		sh.mu.Unlock()
		return Entry{}, false
	}
	counter := s.localCounter.Add(1)
	e := &Entry{
		Key:     key,
		Node:    s.localNode,
		Counter: counter,
		Wall:    wallMs,
		Deleted: true,
	}
	sh.entries[key] = e
	sh.mu.Unlock()

	s.bumpCV(s.localNode, counter)
	s.live.Add(-1)
	s.tombstone.Add(1)
	return *e, true
}

// Apply merges a remote entry. Returns the outcome and the entry now
// authoritative locally. The returned entry's Value slice is owned by the
// State.
func (s *State) Apply(in Entry) (MergeOutcome, Entry) {
	sh := &s.shards[ShardFor(in.Key)]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	cur, existed := sh.entries[in.Key]
	if !existed {
		s.applyNew(sh, &in)
		return MergeApplied, in
	}

	cmp := compareDots(cur, &in)
	switch {
	case cmp > 0:
		return MergeNoop, *cur
	case cmp < 0:
		s.swap(sh, cur, &in)
		return MergeApplied, in
	}

	// Concurrent (compareDots==0: different origin, or identical dot) — resolve
	// by wall-clock LWW with a deterministic total order so every replica
	// converges on the same winner regardless of arrival order.
	if in.Wall > cur.Wall {
		s.swap(sh, cur, &in)
		return MergeWallTiebreak, in
	}
	if in.Wall < cur.Wall {
		return MergeNoop, *cur
	}
	// Equal wall: a delete deterministically beats a live write (symmetric on
	// both replicas).
	if in.Deleted != cur.Deleted {
		if in.Deleted {
			s.swap(sh, cur, &in)
			return MergeDeleteWins, in
		}
		return MergeNoop, *cur
	}
	// Same deleted-ness and equal wall: break the tie by global origin identity,
	// then counter. Identical dots are a no-op (idempotent). Comparing the origin
	// STRING (not the replica-local interned id) keeps the order identical on
	// every replica.
	inOrigin, curOrigin := s.NodeString(in.Node), s.NodeString(cur.Node)
	if inOrigin > curOrigin || (inOrigin == curOrigin && in.Counter > cur.Counter) {
		s.swap(sh, cur, &in)
		return MergeWallTiebreak, in
	}
	return MergeNoop, *cur
}

func (s *State) applyNew(sh *shard, in *Entry) {
	cp := *in
	cp.Value = cloneBytes(in.Value)
	sh.entries[in.Key] = &cp
	if in.Deleted {
		s.tombstone.Add(1)
	} else {
		s.live.Add(1)
	}
	s.bumpCV(in.Node, in.Counter)
}

func (s *State) swap(sh *shard, cur, in *Entry) {
	cp := *in
	cp.Value = cloneBytes(in.Value)
	sh.entries[in.Key] = &cp
	switch {
	case cur.Deleted && !in.Deleted:
		s.tombstone.Add(-1)
		s.live.Add(1)
	case !cur.Deleted && in.Deleted:
		s.live.Add(-1)
		s.tombstone.Add(1)
	}
	s.bumpCV(in.Node, in.Counter)
}

// compareDots returns >0 if a is newer, <0 if b is newer, 0 if same dot or
// concurrent (different origin = forced LWW tiebreak in Apply).
func compareDots(a, b *Entry) int {
	if a.Node != b.Node {
		return 0
	}
	switch {
	case a.Counter > b.Counter:
		return 1
	case a.Counter < b.Counter:
		return -1
	}
	return 0
}

// bumpCV advances cv[node] to max(cv[node], counter).
func (s *State) bumpCV(node uint32, counter uint64) {
	s.cvMu.Lock()
	if int(node) >= len(s.cv) {
		grown := make([]uint64, node+8)
		copy(grown, s.cv)
		s.cv = grown
	}
	if counter > s.cv[node] {
		s.cv[node] = counter
	}
	s.cvMu.Unlock()
}

// CVSnapshot returns a copy of the current version vector indexed by compact
// node ID.
func (s *State) CVSnapshot() []uint64 {
	s.cvMu.RLock()
	out := make([]uint64, len(s.cv))
	copy(out, s.cv)
	s.cvMu.RUnlock()
	return out
}

// ShardHash returns an xxhash64 of the sorted entries in shard `i`. The hash
// uses the canonical string nodeID (not the local compact ID) so two
// replicas with identical entries produce identical hashes — the basis of
// digest-based anti-entropy.
//
// Snapshotting and hashing happen in a single lock window. An earlier
// implementation released the shard lock after grabbing keys, sorted, and
// re-locked for lookups — that races with concurrent Unregister/Overwrite,
// because a delete between the two locks leaves sh.entries[key] == nil and
// the next field access panics with a nil-pointer dereference. Triggered
// reliably by the chaos workload's mixed put/delete stream once anti-entropy
// (MergeRemoteState) starts pulling shards.
func (s *State) ShardHash(i int) uint64 {
	if i < 0 || i >= ShardCount {
		return 0
	}
	sh := &s.shards[i]

	type snap struct {
		key     string
		value   []byte
		counter uint64
		wall    int64
		node    uint32
		deleted bool
	}

	sh.mu.RLock()
	snaps := make([]snap, 0, len(sh.entries))
	for k, e := range sh.entries {
		if e == nil {
			continue
		}
		snaps = append(snaps, snap{
			key:     k,
			node:    e.Node,
			counter: e.Counter,
			wall:    e.Wall,
			deleted: e.Deleted,
			value:   e.Value,
		})
	}
	sh.mu.RUnlock()

	sort.Slice(snaps, func(a, b int) bool { return snaps[a].key < snaps[b].key })

	h := xxhash.New()
	var meta [17]byte
	for _, sn := range snaps {
		_, _ = h.WriteString(sn.key)
		_, _ = h.Write([]byte{0})
		s.cvMu.RLock()
		var origin string
		if int(sn.node) < len(s.stringIDs) {
			origin = s.stringIDs[sn.node]
		}
		s.cvMu.RUnlock()
		_, _ = h.WriteString(origin)
		_, _ = h.Write([]byte{0})
		putUint64(meta[0:8], sn.counter)
		putUint64(meta[8:16], uint64(sn.wall))
		if sn.deleted {
			meta[16] = 1
		} else {
			meta[16] = 0
		}
		_, _ = h.Write(meta[:])
		// Include value bytes so two replicas with same dot but
		// pre-merge divergent values still detect mismatch. The value
		// slice in the snap aliases the entry's slice; entry values are
		// only ever replaced via Overwrite (never mutated in place), so
		// reading the alias after the lock is safe.
		_, _ = h.Write(sn.value)
	}
	return h.Sum64()
}

// ShardEntries returns a deep-copy snapshot of all entries in shard `i`.
func (s *State) ShardEntries(i int) []Entry {
	if i < 0 || i >= ShardCount {
		return nil
	}
	sh := &s.shards[i]
	sh.mu.RLock()
	out := make([]Entry, 0, len(sh.entries))
	for _, e := range sh.entries {
		cp := *e
		cp.Value = cloneBytes(e.Value)
		out = append(out, cp)
	}
	sh.mu.RUnlock()
	return out
}

// LiveCount returns the number of non-tombstoned entries.
func (s *State) LiveCount() int64 { return s.live.Load() }

// TombstoneCount returns the number of tombstoned entries.
func (s *State) TombstoneCount() int64 { return s.tombstone.Load() }

// Range walks ALL live entries in `start <= key < end` lexical order.
// `end == ""` means "to the end of the keyspace". Walks each shard,
// collects matching keys, sorts them, and invokes `fn`. Stops early when
// `fn` returns false. Snapshot semantics: a single pass over current state.
func (s *State) Range(start, end string, fn func(Entry) bool) {
	all := make([]string, 0, 128)
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.RLock()
		for k, e := range sh.entries {
			if e.Deleted {
				continue
			}
			if start != "" && k < start {
				continue
			}
			if end != "" && k >= end {
				continue
			}
			all = append(all, k)
		}
		sh.mu.RUnlock()
	}
	sort.Strings(all)
	for _, k := range all {
		sh := &s.shards[ShardFor(k)]
		sh.mu.RLock()
		e, ok := sh.entries[k]
		var ent Entry
		if ok && !e.Deleted {
			ent = *e
			ent.Value = cloneBytes(e.Value)
		}
		sh.mu.RUnlock()
		if !ok {
			continue
		}
		if !fn(ent) {
			return
		}
	}
}

// ReapTombstones drops tombstones whose origin counter is ≤ the safe
// counter for that origin. `safeByNode` is keyed by compact node ID.
// Tombstones with `nowMs - Wall > wallFloorMs` are dropped regardless.
// Returns counts: (gcSafe, gcWallFloor).
func (s *State) ReapTombstones(safeByNode []uint64, nowMs, wallFloorMs int64) (int, int) {
	var safe, floor int
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		for key, e := range sh.entries {
			if !e.Deleted {
				continue
			}
			if int(e.Node) < len(safeByNode) && e.Counter <= safeByNode[e.Node] {
				delete(sh.entries, key)
				s.tombstone.Add(-1)
				safe++
				continue
			}
			if nowMs-e.Wall > wallFloorMs {
				delete(sh.entries, key)
				s.tombstone.Add(-1)
				floor++
			}
		}
		sh.mu.Unlock()
	}
	return safe, floor
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// putUint64 is a little-endian writer used in ShardHash. Inlined to avoid
// pulling encoding/binary into the hot hash path.
func putUint64(b []byte, v uint64) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
	b[4] = byte(v >> 32)
	b[5] = byte(v >> 40)
	b[6] = byte(v >> 48)
	b[7] = byte(v >> 56)
}
