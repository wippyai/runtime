// SPDX-License-Identifier: MPL-2.0

// Package eventualreg implements a gossip-based, eventually-consistent
// process name registry. It complements the strongly-consistent globalreg
// (Raft) and the per-node topology PIDRegistry (sync.Map). EVENTUAL is sized
// for ~100k user-session-class names per cluster — workloads that cannot
// pay Raft cost per registration but still need cluster-wide visibility.
//
// The state is an Optimized OR-Set Without Tombstones (ORSWOT) keyed by
// name. Each registration carries a per-replica "dot" (origin node, monotonic
// counter); merge is highest-counter-per-origin with wall-clock LWW tiebreak
// and delete-wins on full equality.
package eventualreg

import (
	"sort"
	"sync"
	"sync/atomic"

	"github.com/cespare/xxhash/v2"
	"github.com/wippyai/runtime/api/pid"
)

// ShardCount is the number of shards used for digest hashing and parallel
// access. 64 shards × ~1500 names/shard ≈ 100k names with reasonable
// per-shard contention.
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

// Entry is a single registration record. Tombstones set Deleted=true and
// zero the PID. The (Node, Counter) pair is the per-replica "dot" that
// drives merge resolution.
type Entry struct {
	PID     pid.PID
	Name    string
	Counter uint64 // monotonic per-origin
	Wall    int64  // ms epoch — LWW tiebreak only
	Node    uint32 // origin node, interned via State.nodeIDs
	Deleted bool
}

// shard holds a slice of the name space.
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
	// localCounter is our local Lamport counter.
	// Other nodes' counters live only in cv.
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
	s.localNode = s.internNode(localNodeID)
	return s
}

// internNode allocates a compact ID for a string nodeID. Caller must hold cvMu.
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

// internNode is the locked variant.
func (s *State) internNode(node string) uint32 {
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

// shardFor returns the shard index for a name.
func ShardFor(name string) int {
	return int(xxhash.Sum64String(name) % ShardCount)
}

// Lookup returns the live PID for a name, if registered (and not tombstoned).
func (s *State) Lookup(name string) (pid.PID, bool) {
	sh := &s.shards[ShardFor(name)]
	sh.mu.RLock()
	e, ok := sh.entries[name]
	sh.mu.RUnlock()
	if !ok || e.Deleted {
		return pid.PID{}, false
	}
	return e.PID, true
}

// Register creates a local registration with a fresh dot. Returns the entry
// that was applied (callers will broadcast it via the delta queue). If the
// name is already held by a different PID locally, returns
// (existingEntry, false) — the registration was rejected.
func (s *State) Register(name string, p pid.PID, wallMs int64) (*Entry, bool) {
	sh := &s.shards[ShardFor(name)]
	sh.mu.Lock()
	if cur, ok := sh.entries[name]; ok && !cur.Deleted && cur.PID != p {
		sh.mu.Unlock()
		return cur, false
	}
	counter := s.localCounter.Add(1)
	e := &Entry{
		PID:     p,
		Name:    name,
		Node:    s.localNode,
		Counter: counter,
		Wall:    wallMs,
	}
	prev, existed := sh.entries[name]
	sh.entries[name] = e
	sh.mu.Unlock()

	s.bumpCV(s.localNode, counter)
	if existed && prev.Deleted {
		s.tombstone.Add(-1)
		s.live.Add(1)
	} else if !existed {
		s.live.Add(1)
	}
	return e, true
}

// Unregister tombstones a local registration. Returns the tombstone entry
// that callers should broadcast, or nil if the name wasn't held.
func (s *State) Unregister(name string, wallMs int64) *Entry {
	sh := &s.shards[ShardFor(name)]
	sh.mu.Lock()
	cur, ok := sh.entries[name]
	if !ok || cur.Deleted {
		sh.mu.Unlock()
		return nil
	}
	counter := s.localCounter.Add(1)
	e := &Entry{
		Name:    name,
		Node:    s.localNode,
		Counter: counter,
		Wall:    wallMs,
		Deleted: true,
	}
	sh.entries[name] = e
	sh.mu.Unlock()

	s.bumpCV(s.localNode, counter)
	s.live.Add(-1)
	s.tombstone.Add(1)
	return e
}

// Apply merges a remote entry. Returns the outcome and the entry that is now
// authoritative (caller can use this to decide whether to forward).
func (s *State) Apply(in *Entry) (MergeOutcome, *Entry) {
	sh := &s.shards[ShardFor(in.Name)]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	cur, existed := sh.entries[in.Name]
	if !existed {
		s.applyNew(sh, in)
		return MergeApplied, in
	}

	cmp := compareDots(cur, in)
	switch {
	case cmp > 0:
		return MergeNoop, cur
	case cmp < 0:
		s.swap(sh, cur, in)
		return MergeApplied, in
	}

	// Same (Node, Counter) — wall-clock LWW + delete-wins.
	if in.Wall > cur.Wall {
		s.swap(sh, cur, in)
		return MergeWallTiebreak, in
	}
	if in.Wall == cur.Wall && in.Deleted && !cur.Deleted {
		s.swap(sh, cur, in)
		return MergeDeleteWins, in
	}
	return MergeNoop, cur
}

func (s *State) applyNew(sh *shard, in *Entry) {
	sh.entries[in.Name] = in
	if in.Deleted {
		s.tombstone.Add(1)
	} else {
		s.live.Add(1)
	}
	s.bumpCV(in.Node, in.Counter)
}

func (s *State) swap(sh *shard, cur, in *Entry) {
	sh.entries[in.Name] = in
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

// compareDots returns >0 if a is newer, <0 if b is newer, 0 if same dot.
// Dots are total within an origin: same-origin compare counters; different
// origins are concurrent (return 0 to force LWW tiebreak in Apply).
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
// node ID. The mapping to string IDs is via NodeString.
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
func (s *State) ShardHash(i int) uint64 {
	if i < 0 || i >= ShardCount {
		return 0
	}
	sh := &s.shards[i]
	sh.mu.RLock()
	names := make([]string, 0, len(sh.entries))
	for name := range sh.entries {
		names = append(names, name)
	}
	sh.mu.RUnlock()
	sort.Strings(names)

	h := xxhash.New()
	var meta [17]byte
	sh.mu.RLock()
	for _, name := range names {
		e := sh.entries[name]
		_, _ = h.WriteString(name)
		_, _ = h.Write([]byte{0}) // name terminator
		// Resolve compact node ID to canonical string so the hash is stable
		// across replicas with different local intern tables.
		s.cvMu.RLock()
		var origin string
		if int(e.Node) < len(s.stringIDs) {
			origin = s.stringIDs[e.Node]
		}
		s.cvMu.RUnlock()
		_, _ = h.WriteString(origin)
		_, _ = h.Write([]byte{0}) // origin terminator
		putUint64(meta[0:8], e.Counter)
		putUint64(meta[8:16], uint64(e.Wall))
		if e.Deleted {
			meta[16] = 1
		} else {
			meta[16] = 0
		}
		_, _ = h.Write(meta[:])
	}
	sh.mu.RUnlock()
	return h.Sum64()
}

// ShardEntries returns a snapshot of all entries in shard `i`.
// The returned slice is owned by the caller.
func (s *State) ShardEntries(i int) []*Entry {
	if i < 0 || i >= ShardCount {
		return nil
	}
	sh := &s.shards[i]
	sh.mu.RLock()
	out := make([]*Entry, 0, len(sh.entries))
	for _, e := range sh.entries {
		cp := *e
		out = append(out, &cp)
	}
	sh.mu.RUnlock()
	return out
}

// LiveCount returns the number of non-tombstoned entries.
func (s *State) LiveCount() int64 { return s.live.Load() }

// TombstoneCount returns the number of tombstoned entries.
func (s *State) TombstoneCount() int64 { return s.tombstone.Load() }

// ReapTombstones drops tombstones whose origin counter is ≤ the safe
// counter for that origin. `safeByNode` is keyed by compact node ID.
// Tombstones with `nowMs - Wall > wallFloorMs` are dropped regardless.
// Returns counts: (gcSafe, gcWallFloor).
func (s *State) ReapTombstones(safeByNode []uint64, nowMs, wallFloorMs int64) (int, int) {
	var safe, floor int
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		for name, e := range sh.entries {
			if !e.Deleted {
				continue
			}
			if int(e.Node) < len(safeByNode) && e.Counter <= safeByNode[e.Node] {
				delete(sh.entries, name)
				s.tombstone.Add(-1)
				safe++
				continue
			}
			if nowMs-e.Wall > wallFloorMs {
				delete(sh.entries, name)
				s.tombstone.Add(-1)
				floor++
			}
		}
		sh.mu.Unlock()
	}
	return safe, floor
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
