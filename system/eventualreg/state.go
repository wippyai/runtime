// SPDX-License-Identifier: MPL-2.0

// Package eventualreg implements a gossip-based, eventually-consistent
// process name registry. It complements the strongly-consistent globalreg
// (Raft) and the per-node topology PIDRegistry (sync.Map). EVENTUAL is sized
// for ~100k user-session-class names per cluster — workloads that cannot
// pay Raft cost per registration but still need cluster-wide visibility.
//
// The state is an Optimized OR-Set Without Tombstones (ORSWOT) keyed by
// name. Each registration carries a per-replica "dot" (origin node, monotonic
// counter). Merge is causal within an origin (highest counter wins, same-dot
// delete-wins) and, for concurrent cross-origin conflicts, a deterministic
// join keyed by (Priority, FNV64(name, origin)) — commutative, associative,
// idempotent. Wall is a GC retention floor only, never a resolution discriminator.
package eventualreg

import (
	"hash/fnv"
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
	// MergeNoop means the incoming entry lost to what we already had.
	MergeNoop MergeOutcome = iota
	// MergeApplied means the incoming entry was causally newer (same origin,
	// higher counter, or first-seen) and replaced the local one.
	MergeApplied
	// MergeConflictResolved means a concurrent cross-origin conflict was
	// resolved deterministically by the (Priority, FNV64(name,origin)) key.
	MergeConflictResolved
	// MergeDeleteWins means a same-origin tombstone won on equal dot.
	MergeDeleteWins
)

// Entry is a single registration record. Tombstones set Deleted=true and
// zero the PID. The (Node, Counter) pair is the per-replica "dot" that
// drives merge resolution.
type Entry struct {
	PID      pid.PID
	Name     string
	Counter  uint64 // monotonic per-origin
	Wall     int64  // ms epoch — GC retention backstop only, never a resolution discriminator
	Node     uint32 // origin node, interned via State.nodeIDs
	Priority uint32 // cross-origin conflict precedence; higher wins
	Deleted  bool
}

// nameRecord is the per-name CRDT state: one causally-reduced dot per origin
// that has ever claimed the name. The visible winner is derived, not stored —
// see winnerOf. Storing per-origin (rather than a single dot per name) is what
// makes the join convergent under observed-remove: a cross-origin tombstone
// cannot erase a different origin's live dot, because each origin's dot is kept
// independently and the winner is recomputed from all of them.
//
// The common case (no cross-origin conflict) holds exactly one dot, so the map
// is tiny; only names that were concurrently claimed by multiple origins grow.
type nameRecord struct {
	dots map[uint32]*Entry // origin compact ID → reduced dot
}

// shard holds a slice of the name space.
type shard struct {
	entries map[string]*nameRecord
	mu      sync.RWMutex
}

// State is the in-memory ORSWOT replica.
type State struct {
	// nodeIDs maps string nodeID → compact uint32 to keep entries small.
	// Read under cvMu; write also under cvMu.
	nodeIDs map[string]uint32
	// stringIDs is the reverse intern table. A reclaimed slot is "".
	stringIDs []string
	// cv[node] is the highest counter we have seen from origin `node`.
	// Used for tombstone GC and to detect stale incoming entries.
	cv []uint64
	// nodeRefs[node] counts records currently holding a dot from origin `node`
	// (rec.dots[node] present, live OR tombstone). An id with refs > 0 is never
	// reclaimed. Guarded by cvMu; mutated only inside a shard lock (so the count
	// stays consistent with the per-shard dot maps), matching the shard → cvMu
	// lock order of bumpCV.
	nodeRefs []uint64
	// freeIDs is the LIFO free-list of reclaimed compact ids available for reuse.
	// Reuse bounds the intern slices to the high-water-mark of concurrently
	// referenced identities rather than the all-time distinct count. Guarded by
	// cvMu.
	freeIDs []uint32
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
		s.shards[i].entries = make(map[string]*nameRecord)
	}
	s.localNode = s.internNode(localNodeID)
	return s
}

// internNode allocates a compact ID for a string nodeID. Caller must hold cvMu.
// A reclaimed slot from freeIDs is reused before a fresh slot is appended,
// which bounds the intern slices to the concurrent-identity high-water-mark.
func (s *State) internNodeLocked(node string) uint32 {
	if id, ok := s.nodeIDs[node]; ok {
		return id
	}
	if n := len(s.freeIDs); n > 0 {
		id := s.freeIDs[n-1]
		s.freeIDs = s.freeIDs[:n-1]
		s.nodeIDs[node] = id
		s.stringIDs[id] = node
		s.cv[id] = 0
		s.nodeRefs[id] = 0
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
	if int(id) >= len(s.nodeRefs) {
		grown := make([]uint64, id+8)
		copy(grown, s.nodeRefs)
		s.nodeRefs = grown
	}
	return id
}

// retain increments the refcount for origin id (a new rec.dots[id] was created).
// Mutates under cvMu; callers already hold the relevant shard lock.
func (s *State) retain(id uint32) {
	s.cvMu.Lock()
	if int(id) >= len(s.nodeRefs) {
		grown := make([]uint64, id+8)
		copy(grown, s.nodeRefs)
		s.nodeRefs = grown
	}
	s.nodeRefs[id]++
	s.cvMu.Unlock()
}

// release decrements the refcount for origin id (a rec.dots[id] was removed).
// Mutates under cvMu; callers already hold the relevant shard lock.
func (s *State) release(id uint32) {
	s.cvMu.Lock()
	if int(id) < len(s.nodeRefs) && s.nodeRefs[id] > 0 {
		s.nodeRefs[id]--
	}
	s.cvMu.Unlock()
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

// StringIDs returns a copy of the intern table indexed by compact ID. A
// reclaimed slot is "". Used to observe intern-table size for reclamation tests.
func (s *State) StringIDs() []string {
	s.cvMu.RLock()
	defer s.cvMu.RUnlock()
	out := make([]string, len(s.stringIDs))
	copy(out, s.stringIDs)
	return out
}

// shardFor returns the shard index for a name.
func ShardFor(name string) int {
	return int(xxhash.Sum64String(name) % ShardCount)
}

// Lookup returns the live PID for a name, if registered (and not tombstoned).
func (s *State) Lookup(name string) (pid.PID, bool) {
	sh := &s.shards[ShardFor(name)]
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	rec, ok := sh.entries[name]
	if !ok {
		return pid.PID{}, false
	}
	w := s.winnerOf(rec)
	if w == nil || w.Deleted {
		return pid.PID{}, false
	}
	return w.PID, true
}

// winnerOf derives the visible entry for a name record. The winner is the
// highest-ranked LIVE dot across origins; if no origin is live, it is the
// highest-ranked tombstone (so the name reports as absent but a tombstone
// still propagates and GCs). Ranking is winnerKey: (Priority, FNV64(name,
// origin)), which is fixed per (name, origin) and therefore a total,
// transitive, arrival-order-independent order. Returns nil for an empty record.
func (s *State) winnerOf(rec *nameRecord) *Entry {
	var live, tomb *Entry
	var liveKey, tombKey concurrentKey
	for _, e := range rec.dots {
		k := s.winnerKey(e)
		if e.Deleted {
			if tomb == nil || concurrentKeyCmp(k, tombKey) > 0 {
				tomb, tombKey = e, k
			}
			continue
		}
		if live == nil || concurrentKeyCmp(k, liveKey) > 0 {
			live, liveKey = e, k
		}
	}
	if live != nil {
		return live
	}
	return tomb
}

// LostBinding records a local registration that lost a name to a different
// origin's winner. The home node uses it to signal name_revoked to the local
// process identified by PID. The (Node, Counter, PID) triple is the lost dot,
// used for once-per-loss dedupe.
type LostBinding struct {
	Name    string
	PID     pid.PID
	Counter uint64 // counter of the lost local dot
	Node    uint32 // origin of the lost local dot (always localNode)
}

// RegisterResult is the outcome of a local Register attempt.
type RegisterResult struct {
	// Entry is the local dot to broadcast (always set). On a win it is the
	// authoritative winner; on a cross-origin loss it is still the freshly
	// minted local dot — it MUST be gossiped so peers learn this origin's
	// claim and converge on the same winner.
	Entry *Entry
	// Winner is the currently-authoritative entry after the local dot is
	// merged. Equals Entry when Won is true.
	Winner *Entry
	// Lost is set when this fresh registration did not become the winner
	// because a different-origin entry out-ranks it (the local process must be
	// signaled). Nil otherwise.
	Lost *LostBinding
	// Won is true when the local registration is now the visible winner.
	Won bool
}

// Register creates a local registration with a fresh dot and merges it.
//
// If the name is already held by a different PID on THIS node (same origin,
// live), the registration is rejected with no dot consumed — same-node
// conflicts stay a hard local error (Won=false, Lost=nil, Entry=existing).
//
// Otherwise a fresh local dot is minted and merged. If it becomes the visible
// winner, Won=true. If a different-origin entry out-ranks it, Won=false and
// Lost carries the local PID so the home node signals name_revoked; the local
// dot is still installed and returned in Entry for gossip so the cluster
// converges deterministically.
func (s *State) Register(name string, p pid.PID, wallMs int64, priority uint32) RegisterResult {
	sh := &s.shards[ShardFor(name)]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	rec, existed := sh.entries[name]
	if existed {
		if cur, ok := rec.dots[s.localNode]; ok && !cur.Deleted && cur.PID != p {
			return RegisterResult{Entry: cur, Winner: s.winnerOf(rec), Won: false}
		}
	} else {
		rec = &nameRecord{dots: make(map[uint32]*Entry, 1)}
		sh.entries[name] = rec
	}

	prevWinner := s.winnerOf(rec)
	counter := s.localCounter.Add(1)
	e := &Entry{
		PID:      p,
		Name:     name,
		Node:     s.localNode,
		Counter:  counter,
		Wall:     wallMs,
		Priority: priority,
	}
	_, hadLocal := rec.dots[s.localNode]
	rec.dots[s.localNode] = e
	if !hadLocal {
		s.retain(s.localNode)
	}
	s.bumpCV(s.localNode, counter)
	newWinner := s.winnerOf(rec)
	s.adjustCounts(sh, name, prevWinner, newWinner)

	won := newWinner == e
	res := RegisterResult{Entry: e, Winner: newWinner, Won: won}
	if !won {
		res.Lost = &LostBinding{Name: name, PID: p, Node: s.localNode, Counter: counter}
	}
	return res
}

// Unregister tombstones a local registration. Returns the tombstone entry
// that callers should broadcast, or nil if the name wasn't held live locally.
func (s *State) Unregister(name string, wallMs int64) *Entry {
	sh := &s.shards[ShardFor(name)]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	rec, ok := sh.entries[name]
	if !ok {
		return nil
	}
	cur, ok := rec.dots[s.localNode]
	if !ok || cur.Deleted {
		return nil
	}
	prevWinner := s.winnerOf(rec)
	counter := s.localCounter.Add(1)
	e := &Entry{
		Name:     name,
		Node:     s.localNode,
		Counter:  counter,
		Wall:     wallMs,
		Priority: cur.Priority,
		Deleted:  true,
	}
	rec.dots[s.localNode] = e
	s.bumpCV(s.localNode, counter)
	s.adjustCounts(sh, name, prevWinner, s.winnerOf(rec))
	return e
}

// Apply merges a remote dot into the per-origin record. Returns the outcome,
// the authoritative winner after the merge, and a LostBinding when the merge
// changed the winner away from a LOCAL-origin live dot to a different origin
// (the home node signals name_revoked off this). lost is nil otherwise.
//
// The join is commutative, associative, idempotent:
//
//   - Per origin: the dot with the highest Counter is kept (causal). On equal
//     counter (same dot) a delete wins over a live entry. Wall is never used.
//   - The visible winner is then derived across origins by winnerOf: highest
//     winnerKey among LIVE origins (observed-remove — a tombstone from origin
//     X cannot suppress origin Y's live dot because Y's dot is kept
//     independently). winnerKey is fixed per (name, origin), so the order is
//     total and arrival-independent.
func (s *State) Apply(in *Entry) (MergeOutcome, *Entry, *LostBinding) {
	sh := &s.shards[ShardFor(in.Name)]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	rec, existed := sh.entries[in.Name]
	if !existed {
		rec = &nameRecord{dots: map[uint32]*Entry{in.Node: in}}
		sh.entries[in.Name] = rec
		s.retain(in.Node)
		s.bumpCV(in.Node, in.Counter)
		w := s.winnerOf(rec)
		s.adjustCounts(sh, in.Name, nil, w)
		return MergeApplied, w, nil
	}

	prevWinner := s.winnerOf(rec)
	cur, hasOrigin := rec.dots[in.Node]
	outcome := MergeNoop
	if !hasOrigin {
		rec.dots[in.Node] = in
		s.retain(in.Node)
		outcome = MergeApplied
	} else {
		switch {
		case in.Counter > cur.Counter:
			rec.dots[in.Node] = in
			outcome = MergeApplied
		case in.Counter < cur.Counter:
			// stale within origin
		default:
			// Same dot: delete-wins.
			if in.Deleted && !cur.Deleted {
				rec.dots[in.Node] = in
				outcome = MergeDeleteWins
			}
		}
	}
	if outcome == MergeNoop {
		return MergeNoop, prevWinner, nil
	}

	s.bumpCV(in.Node, in.Counter)
	newWinner := s.winnerOf(rec)
	s.adjustCounts(sh, in.Name, prevWinner, newWinner)

	// Detect a local-origin live loss: the winner changed from our local live
	// dot to a different origin's winner.
	var lost *LostBinding
	if prevWinner != nil && newWinner != nil &&
		prevWinner.Node == s.localNode && !prevWinner.Deleted &&
		newWinner.Node != s.localNode && newWinner != prevWinner {
		lost = &LostBinding{
			Name:    prevWinner.Name,
			PID:     prevWinner.PID,
			Node:    prevWinner.Node,
			Counter: prevWinner.Counter,
		}
		if outcome == MergeApplied {
			outcome = MergeConflictResolved
		}
	}
	return outcome, newWinner, lost
}

// ReapNode tombstones every LIVE dot whose resolved PID lives on `departed`,
// regardless of which origin minted the dot. A departed node B is the origin of
// its own names, so on a surviving replica B's bindings live at rec.dots[B] —
// not at rec.dots[localNode]. Unregister only touches the local origin's dot
// and cannot reap them; ReapNode tombstones the foreign-origin dot in place.
//
// The tombstone keeps the dot's (origin Node, Counter) and only sets
// Deleted=true. This converges deterministically: every surviving replica
// independently produces the IDENTICAL tombstone for the departed origin's dot
// (same origin, same counter, Deleted=true), and same-dot delete-wins (Apply's
// equal-counter branch) means whichever order replicas exchange these dots, the
// tombstone wins over any lingering live copy. The departed origin cannot mint
// a newer counter to resurrect it (it has left), so the tombstone is terminal.
//
// Returns the tombstone entries (copies) so the service can broadcast them and
// let the delete propagate by gossip.
func (s *State) ReapNode(departed string) []*Entry {
	var out []*Entry
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		for name, rec := range sh.entries {
			prevWinner := s.winnerOf(rec)
			changed := false
			for node, e := range rec.dots {
				if e.Deleted || e.PID.Node != departed {
					continue
				}
				tomb := &Entry{
					Name:     e.Name,
					Node:     e.Node,
					Counter:  e.Counter,
					Wall:     e.Wall,
					Priority: e.Priority,
					Deleted:  true,
				}
				rec.dots[node] = tomb
				changed = true
				cp := *tomb
				out = append(out, &cp)
			}
			if changed {
				s.adjustCounts(sh, name, prevWinner, s.winnerOf(rec))
			}
		}
		sh.mu.Unlock()
	}
	return out
}

// adjustCounts updates the live/tombstone gauges after a name's visible winner
// changes from prev to next. Counts track NAMES by their derived winner's
// liveness, so a name with concurrent dots is counted once.
func (s *State) adjustCounts(_ *shard, _ string, prev, next *Entry) {
	prevLive := prev != nil && !prev.Deleted
	prevTomb := prev != nil && prev.Deleted
	nextLive := next != nil && !next.Deleted
	nextTomb := next != nil && next.Deleted
	if prevLive && !nextLive {
		s.live.Add(-1)
	}
	if !prevLive && nextLive {
		s.live.Add(1)
	}
	if prevTomb && !nextTomb {
		s.tombstone.Add(-1)
	}
	if !prevTomb && nextTomb {
		s.tombstone.Add(1)
	}
}

// concurrentKey is the cross-origin tiebreak key. It is FIXED per
// (name, origin): it deliberately excludes Counter, PID and Wall. Including
// any per-dot field makes the order non-transitive (A1>B1, B1>A2, A2>A1) and
// the CRDT diverges by arrival order. Holding the key fixed per origin lets
// same-origin causality reduce each origin to its latest dot while the
// cross-origin rank stays stable as Counter advances.
type concurrentKey struct {
	priority uint32
	hash     uint64
}

// winnerKey computes the concurrent tiebreak key for an entry:
// (Priority, FNV64a(name || originNodeIDstring)). Compared Priority desc,
// then hash desc.
func (s *State) winnerKey(e *Entry) concurrentKey {
	h := fnv.New64a()
	_, _ = h.Write([]byte(e.Name))
	_, _ = h.Write([]byte(s.NodeString(e.Node)))
	return concurrentKey{priority: e.Priority, hash: h.Sum64()}
}

// concurrentKeyCmp returns >0 if a outranks b, <0 if b outranks a, 0 if equal.
func concurrentKeyCmp(a, b concurrentKey) int {
	switch {
	case a.priority > b.priority:
		return 1
	case a.priority < b.priority:
		return -1
	case a.hash > b.hash:
		return 1
	case a.hash < b.hash:
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

// ShardHash returns an xxhash64 of every per-origin dot in shard `i`, sorted
// by (name, origin string) so two replicas with identical state produce
// identical hashes — the basis of digest-based anti-entropy. Hashing all dots
// (not just the derived winner) is required: two replicas could agree on the
// winner while disagreeing on a loser's dot, and that disagreement must
// surface so anti-entropy heals it.
func (s *State) ShardHash(i int) uint64 {
	if i < 0 || i >= ShardCount {
		return 0
	}
	sh := &s.shards[i]
	sh.mu.RLock()
	defer sh.mu.RUnlock()

	type dotRef struct {
		e      *Entry
		name   string
		origin string
	}
	refs := make([]dotRef, 0, len(sh.entries))
	for name, rec := range sh.entries {
		for node, e := range rec.dots {
			refs = append(refs, dotRef{name: name, origin: s.NodeString(node), e: e})
		}
	}
	sort.Slice(refs, func(a, b int) bool {
		if refs[a].name != refs[b].name {
			return refs[a].name < refs[b].name
		}
		return refs[a].origin < refs[b].origin
	})

	h := xxhash.New()
	var meta [21]byte
	for _, r := range refs {
		e := r.e
		_, _ = h.WriteString(r.name)
		_, _ = h.Write([]byte{0}) // name terminator
		_, _ = h.WriteString(r.origin)
		_, _ = h.Write([]byte{0}) // origin terminator
		putUint64(meta[0:8], e.Counter)
		putUint64(meta[8:16], uint64(e.Wall))
		putUint32(meta[16:20], e.Priority)
		if e.Deleted {
			meta[20] = 1
		} else {
			meta[20] = 0
		}
		_, _ = h.Write(meta[:])
		// Mix the resolved PID so two replicas holding the same dot but
		// divergent PIDs produce different hashes — anti-entropy detects and
		// reconciles. Tombstones carry a zero PID; hashing it is harmless.
		_, _ = h.WriteString(e.PID.Node)
		_, _ = h.Write([]byte{0})
		_, _ = h.WriteString(e.PID.Host)
		_, _ = h.Write([]byte{0})
		_, _ = h.WriteString(e.PID.UniqID)
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

// ShardEntries returns a snapshot of every per-origin dot in shard `i`.
// The returned slice is owned by the caller. All dots are emitted (not just
// winners) so bulk anti-entropy transfers the full per-origin state.
func (s *State) ShardEntries(i int) []*Entry {
	if i < 0 || i >= ShardCount {
		return nil
	}
	sh := &s.shards[i]
	sh.mu.RLock()
	out := make([]*Entry, 0, len(sh.entries))
	for _, rec := range sh.entries {
		for _, e := range rec.dots {
			cp := *e
			out = append(out, &cp)
		}
	}
	sh.mu.RUnlock()
	return out
}

// LiveCount returns the number of names whose visible winner is live.
func (s *State) LiveCount() int64 { return s.live.Load() }

// TombstoneCount returns the number of names whose visible winner is a tombstone.
func (s *State) TombstoneCount() int64 { return s.tombstone.Load() }

// ReapTombstones drops per-origin tombstone dots whose origin counter is ≤ the
// safe counter for that origin, or older than the wall floor. `safeByNode` is
// keyed by compact node ID. Wall is a GC retention floor only here — never a
// conflict-resolution discriminator. A name whose record becomes empty is
// removed; if reaping changes the visible winner, the live/tombstone gauges
// are adjusted. Returns counts: (gcSafe, gcWallFloor) of dots dropped.
func (s *State) ReapTombstones(safeByNode []uint64, nowMs, wallFloorMs int64) (int, int) {
	var safe, floor int
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		for name, rec := range sh.entries {
			prevWinner := s.winnerOf(rec)
			for node, e := range rec.dots {
				if !e.Deleted {
					continue
				}
				if int(e.Node) < len(safeByNode) && e.Counter <= safeByNode[e.Node] {
					delete(rec.dots, node)
					s.release(node)
					safe++
					continue
				}
				if nowMs-e.Wall > wallFloorMs {
					delete(rec.dots, node)
					s.release(node)
					floor++
				}
			}
			if len(rec.dots) == 0 {
				delete(sh.entries, name)
				s.adjustCounts(sh, name, prevWinner, nil)
				continue
			}
			s.adjustCounts(sh, name, prevWinner, s.winnerOf(rec))
		}
		sh.mu.Unlock()
	}
	return safe, floor
}

// ReclaimUnreferencedNodes frees compact ids that no record references and no
// longer belong to a live cluster member, returning the count reclaimed. A slot
// is reclaimed iff its refcount is zero (no record holds a dot from that origin,
// live OR tombstone), it is not the local node, it is still interned, and its
// string id is absent from `alive`. The freed slot is pushed onto freeIDs for
// reuse by the next new origin.
//
// Reclamation is a purely local memory optimization with zero convergence or
// wire impact: the gossip wire encodes origin STRINGS (BroadcastQueue.originFor,
// EncodeShardPayload) and ShardHash/digest hash origin STRINGS via NodeString,
// never the compact uint32. Replicas may therefore reclaim independently and
// still converge. The refcount gate closes the reclaim TOCTOU: an id with any
// live or tombstone dot has refs > 0 and is skipped; a departed origin's id is
// reclaimed only after every one of its dots (including tombstones) has been
// dropped by ReapTombstones and the origin is not a live member.
//
// Called under cvMu (acquired here). It does not touch shard locks, so it can
// never invert the shard → cvMu order and cannot deadlock against retain,
// release, or bumpCV.
func (s *State) ReclaimUnreferencedNodes(alive map[string]struct{}) int {
	s.cvMu.Lock()
	defer s.cvMu.Unlock()

	reclaimed := 0
	for id := uint32(0); int(id) < len(s.stringIDs); id++ {
		if id == s.localNode {
			continue
		}
		name := s.stringIDs[id]
		if name == "" {
			continue
		}
		if int(id) < len(s.nodeRefs) && s.nodeRefs[id] > 0 {
			continue
		}
		if _, ok := alive[name]; ok {
			continue
		}
		delete(s.nodeIDs, name)
		s.stringIDs[id] = ""
		if int(id) < len(s.cv) {
			s.cv[id] = 0
		}
		s.freeIDs = append(s.freeIDs, id)
		reclaimed++
	}
	return reclaimed
}

// putUint32 is a little-endian writer used in ShardHash.
func putUint32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
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
