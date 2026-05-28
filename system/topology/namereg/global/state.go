// SPDX-License-Identifier: MPL-2.0

package global

import (
	"hash/fnv"
	"sync"

	"github.com/wippyai/runtime/api/pid"
)

const shardCount = 16

// shard holds a partition of the global name space. Each shard has its own
// RWMutex so reads on different shards never contend.
type shard struct {
	names    map[string]*nameEntry
	pidNames map[string][]string
	mu       sync.RWMutex
}

// nameEntry records the owner of a global name. RequiredNodes/Epoch are
// populated only for a promoted Strong name: RequiredNodes is the exclusion
// holder set (so a terminal removal of the active name can deliver an exclusion
// release) and Epoch is the reservation epoch the exclusion was latched at (so
// the release is indexed to the held instance). Both are empty/zero for
// Consistent-scope entries.
type nameEntry struct {
	PID           pid.PID
	NodeID        pid.NodeID
	RequiredNodes []pid.NodeID
	AppliedAt     uint64
	Epoch         uint64
}

// shardedState is the in-memory state machine, split into shardCount shards.
// All write methods assume the caller holds the appropriate shard write-lock
// (or is being called from FSM.Apply which is single-threaded).
type shardedState struct {
	pending   map[string]*pendingEntry
	nodeIndex sync.Map
	expired   []ExpiredRecord
	shards    [shardCount]shard
	pendingMu sync.RWMutex
	expiredMu sync.Mutex
}

// expiredRingCap caps the in-memory history of expired Strong reservations.
const expiredRingCap = 64

// pendingEntry captures a Strong-scope reservation in flight.
type pendingEntry struct {
	AckSet           map[pid.NodeID]struct{}
	PID              pid.PID
	Name             string
	NodeID           pid.NodeID
	RequiredNodes    []pid.NodeID
	Epoch            uint64
	DeadlineUnixNano int64
	CreatedAt        int64
}

// ExpiredRecord is the public view of a released Strong reservation.
type ExpiredRecord struct {
	PID         pid.PID      `json:"pid"`
	Name        string       `json:"name"`
	Reason      string       `json:"reason"`
	RejectedBy  pid.NodeID   `json:"rejected_by,omitempty"`
	MissingAcks []pid.NodeID `json:"missing_acks,omitempty"`
	Epoch       uint64       `json:"epoch"`
	ExpiredAt   int64        `json:"expired_at_unix_nano"`
}

// PendingView is the public read-only view of a Strong reservation.
type PendingView struct {
	PID              pid.PID      `json:"pid"`
	Name             string       `json:"name"`
	RequiredNodes    []pid.NodeID `json:"required_nodes"`
	AckSet           []pid.NodeID `json:"ack_set"`
	MissingAcks      []pid.NodeID `json:"missing_acks"`
	Epoch            uint64       `json:"epoch"`
	DeadlineUnixNano int64        `json:"deadline_unix_nano"`
	CreatedAt        int64        `json:"created_at_unix_nano"`
}

// pidSet is a thread-safe set of PID string keys used in the node index.
type pidSet struct {
	pids map[string]struct{}
	mu   sync.Mutex
}

func newShardedState() *shardedState {
	s := &shardedState{
		pending: make(map[string]*pendingEntry),
	}
	for i := range s.shards {
		s.shards[i].names = make(map[string]*nameEntry)
		s.shards[i].pidNames = make(map[string][]string)
	}
	return s
}

// shardFor returns the shard index for a name.
func shardFor(name string) int {
	h := fnv.New32a()
	h.Write([]byte(name))
	return int(h.Sum32() % shardCount)
}

// --- Read operations (caller manages read-lock) ---

// Lookup returns the PID registered for a name, if any.
func (s *shardedState) Lookup(name string) (pid.PID, bool) {
	sh := &s.shards[shardFor(name)]
	sh.mu.RLock()
	e, ok := sh.names[name]
	sh.mu.RUnlock()
	if !ok {
		return pid.PID{}, false
	}
	return e.PID, true
}

// LookupWithIndex returns the PID and registration index (Raft log index at
// which the name was committed) for a name. The index orders registrations
// and feeds the Strong-scope activation epoch.
func (s *shardedState) LookupWithIndex(name string) (pid.PID, uint64, bool) {
	sh := &s.shards[shardFor(name)]
	sh.mu.RLock()
	e, ok := sh.names[name]
	sh.mu.RUnlock()
	if !ok {
		return pid.PID{}, 0, false
	}
	return e.PID, e.AppliedAt, true
}

// Len returns the total number of registered names across all shards.
// Acquires read-locks on every shard briefly so the result is a consistent
// point-in-time count. Intended for telemetry — do not call in tight loops.
func (s *shardedState) Len() int {
	total := 0
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.RLock()
		total += len(sh.names)
		sh.mu.RUnlock()
	}

	return total
}

// LookupByPID returns all names registered to a PID.
// Since pid → names is stored per-shard in pidNames, we must scan
// all shards (the PID may have names hashing to different shards).
func (s *shardedState) LookupByPID(p pid.PID) []string {
	key := p.String()
	var result []string
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.RLock()
		if names, ok := sh.pidNames[key]; ok {
			result = append(result, names...)
		}
		sh.mu.RUnlock()
	}
	return result
}

// --- Write operations (called from FSM.Apply, single-threaded) ---

// registerOutcome captures the disposition of a register attempt. It lets
// callers (FSM/Service) emit fine-grained telemetry — fresh registration vs
// idempotent dedupe vs name-collision — without re-deriving the case.
type registerOutcome uint8

const (
	// registerInserted means a brand-new name → PID mapping was committed.
	registerInserted registerOutcome = iota
	// registerDedupe means the name was already mapped to the same PID; no-op.
	registerDedupe
	// registerConflict means the name is already taken by a different PID.
	registerConflict
)

// register attempts to insert or verify a name → PID mapping.
// On success it returns the supplied PID and either registerInserted (fresh)
// or registerDedupe (already mapped to the same PID — idempotent no-op).
// On collision it returns the existing owner PID and registerConflict.
func (s *shardedState) register(name string, p pid.PID, nodeID pid.NodeID, index uint64) (pid.PID, registerOutcome) {
	s.pendingMu.RLock()
	if e, ok := s.pending[name]; ok {
		s.pendingMu.RUnlock()
		if e.PID == p {
			return p, registerDedupe
		}
		return e.PID, registerConflict
	}
	s.pendingMu.RUnlock()

	sh := &s.shards[shardFor(name)]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	if existing, ok := sh.names[name]; ok {
		if existing.PID == p {
			return p, registerDedupe
		}

		return existing.PID, registerConflict
	}

	sh.names[name] = &nameEntry{PID: p, NodeID: nodeID, AppliedAt: index}

	pidKey := p.String()
	sh.pidNames[pidKey] = append(sh.pidNames[pidKey], name)

	s.addToNodeIndex(nodeID, pidKey)

	return p, registerInserted
}

// pendingOutcome captures the disposition of a registerPending attempt.
type pendingOutcome uint8

const (
	pendingInserted pendingOutcome = iota
	pendingDedupe
	pendingConflictActive
	pendingConflictPending
)

// registerPending opens a Strong-scope reservation. The reservation is rejected
// if the name is already active (registerConflictActive) or already pending
// for a different PID (pendingConflictPending). Re-submitting the same name+PID
// while pending is idempotent (pendingDedupe).
//
// requiredNodes is stamped by the leader at the pending commit and embedded in
// the log entry so every replica sees the same set during replay. A node-leave
// prunes the departed node from this set deterministically via CmdDropRequired
// (state.dropRequired), never by re-reading membership inside Apply.
func (s *shardedState) registerPending(name string, p pid.PID, nodeID pid.NodeID, epoch uint64, required []pid.NodeID, deadline int64, createdAt int64) (pid.PID, pendingOutcome) {
	sh := &s.shards[shardFor(name)]
	sh.mu.RLock()
	existing, hasActive := sh.names[name]
	sh.mu.RUnlock()
	if hasActive {
		if existing.PID == p {
			return p, pendingDedupe
		}
		return existing.PID, pendingConflictActive
	}

	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if e, ok := s.pending[name]; ok {
		if e.PID == p && e.Epoch == epoch {
			return p, pendingDedupe
		}
		return e.PID, pendingConflictPending
	}
	reqCopy := make([]pid.NodeID, len(required))
	copy(reqCopy, required)
	s.pending[name] = &pendingEntry{
		Name:             name,
		PID:              p,
		NodeID:           nodeID,
		Epoch:            epoch,
		RequiredNodes:    reqCopy,
		AckSet:           make(map[pid.NodeID]struct{}, len(required)),
		DeadlineUnixNano: deadline,
		CreatedAt:        createdAt,
	}
	return p, pendingInserted
}

// recordAck records a node's ack for a pending reservation. Returns the
// pending entry (post-update), and whether the ack set now covers
// RequiredNodes.
func (s *shardedState) recordAck(name string, epoch uint64, acker pid.NodeID) (*pendingEntry, bool, bool) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	e, ok := s.pending[name]
	if !ok || e.Epoch != epoch {
		return nil, false, false
	}
	if _, dup := e.AckSet[acker]; dup {
		return e, false, ackComplete(e)
	}
	e.AckSet[acker] = struct{}{}
	return e, true, ackComplete(e)
}

func ackComplete(e *pendingEntry) bool {
	for _, n := range e.RequiredNodes {
		if _, ok := e.AckSet[n]; !ok {
			return false
		}
	}
	return true
}

// dropRequired removes a departed node from an in-flight pending entry's
// RequiredNodes set. Returns the entry (post-update), whether a node was
// actually removed, and whether the ack set now covers the reduced required
// set (so the caller can promote in the same Apply). Idempotent: a drop for a
// node no longer required, an unknown name, or a mismatched epoch returns
// dropped=false.
func (s *shardedState) dropRequired(name string, epoch uint64, node pid.NodeID) (*pendingEntry, bool, bool) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	e, ok := s.pending[name]
	if !ok || e.Epoch != epoch {
		return nil, false, false
	}
	idx := -1
	for i, n := range e.RequiredNodes {
		if n == node {
			idx = i
			break
		}
	}
	if idx < 0 {
		return e, false, ackComplete(e)
	}
	e.RequiredNodes = append(e.RequiredNodes[:idx], e.RequiredNodes[idx+1:]...)
	return e, true, ackComplete(e)
}

// rejectPending terminally fails a pending reservation rejected by a required
// node. The entry is removed and recorded in the expired ring with the
// rejecter and reason. Returns the entry and true on success; false if the
// name is unknown or the epoch mismatches (stale reject).
func (s *shardedState) rejectPending(name string, epoch uint64, rejecter pid.NodeID, reason string, expiredAt int64) (*pendingEntry, bool) {
	s.pendingMu.Lock()
	e, ok := s.pending[name]
	if !ok || e.Epoch != epoch {
		s.pendingMu.Unlock()
		return nil, false
	}
	delete(s.pending, name)
	s.pendingMu.Unlock()

	missing := make([]pid.NodeID, 0, len(e.RequiredNodes))
	for _, n := range e.RequiredNodes {
		if _, acked := e.AckSet[n]; !acked {
			missing = append(missing, n)
		}
	}

	s.expiredMu.Lock()
	rec := ExpiredRecord{
		Name:        name,
		PID:         e.PID,
		Epoch:       epoch,
		Reason:      reason,
		RejectedBy:  rejecter,
		MissingAcks: missing,
		ExpiredAt:   expiredAt,
	}
	if len(s.expired) < expiredRingCap {
		s.expired = append(s.expired, rec)
	} else {
		copy(s.expired, s.expired[1:])
		s.expired[len(s.expired)-1] = rec
	}
	s.expiredMu.Unlock()

	return e, true
}

// promotePending moves a pending entry into the active names map. Callers
// must have already verified that recordAck returned complete=true.
// activationIndex is the Raft log index of the CmdRegisterAck that completed
// the set — it becomes the fence token of the active entry.
func (s *shardedState) promotePending(name string, epoch uint64, activationIndex uint64) (*pendingEntry, bool) {
	s.pendingMu.Lock()
	e, ok := s.pending[name]
	if !ok || e.Epoch != epoch {
		s.pendingMu.Unlock()
		return nil, false
	}
	delete(s.pending, name)
	s.pendingMu.Unlock()

	req := make([]pid.NodeID, len(e.RequiredNodes))
	copy(req, e.RequiredNodes)
	sh := &s.shards[shardFor(name)]
	sh.mu.Lock()
	sh.names[name] = &nameEntry{
		PID:           e.PID,
		NodeID:        e.NodeID,
		AppliedAt:     activationIndex,
		RequiredNodes: req,
		Epoch:         e.Epoch,
	}
	pidKey := e.PID.String()
	sh.pidNames[pidKey] = append(sh.pidNames[pidKey], name)
	sh.mu.Unlock()
	s.addToNodeIndex(e.NodeID, pidKey)
	return e, true
}

// expirePending releases a pending reservation and records it in the
// expired ring buffer.
func (s *shardedState) expirePending(name string, epoch uint64, reason string, expiredAt int64) (*pendingEntry, []pid.NodeID, bool) {
	s.pendingMu.Lock()
	e, ok := s.pending[name]
	if !ok || e.Epoch != epoch {
		s.pendingMu.Unlock()
		return nil, nil, false
	}
	delete(s.pending, name)
	s.pendingMu.Unlock()

	missing := make([]pid.NodeID, 0, len(e.RequiredNodes))
	for _, n := range e.RequiredNodes {
		if _, acked := e.AckSet[n]; !acked {
			missing = append(missing, n)
		}
	}

	s.expiredMu.Lock()
	rec := ExpiredRecord{
		Name:        name,
		PID:         e.PID,
		Epoch:       epoch,
		Reason:      reason,
		MissingAcks: missing,
		ExpiredAt:   expiredAt,
	}
	if len(s.expired) < expiredRingCap {
		s.expired = append(s.expired, rec)
	} else {
		copy(s.expired, s.expired[1:])
		s.expired[len(s.expired)-1] = rec
	}
	s.expiredMu.Unlock()

	return e, missing, true
}

// unreservePending drops a pending entry without recording it as expired.
// Used by explicit UnregisterScope(Strong) before activation completes.
func (s *shardedState) unreservePending(name string, p pid.PID) (*pendingEntry, bool) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	e, ok := s.pending[name]
	if !ok {
		return nil, false
	}
	if p != (pid.PID{}) && e.PID != p {
		return e, false
	}
	delete(s.pending, name)
	return e, true
}

// pendingByName returns a snapshot of a single pending entry, or nil if
// none. The returned PendingView is a copy.
func (s *shardedState) pendingByName(name string) *PendingView {
	s.pendingMu.RLock()
	defer s.pendingMu.RUnlock()
	e, ok := s.pending[name]
	if !ok {
		return nil
	}
	return e.view()
}

// strongActiveView is a read-only view of a promoted Strong name. A promoted
// Strong entry is distinguished from a plain Consistent register by carrying a
// non-empty RequiredNodes set and a non-zero Epoch (set at promotePending);
// Consistent entries leave both empty/zero. The join-epoch snapshot enumerates
// these so a joining node installs an Active exclusion for each.
type strongActiveView struct {
	PID   pid.PID
	Name  string
	Epoch uint64
}

// activeBinding is a read-only view of an ACTIVE binding (any scope), keyed by
// the Raft index at which the binding was committed. The join-snapshot
// enumerates these so a joining node seeds the dissem cache for CONSISTENT
// names alongside the existing STRONG seeding.
type activeBinding struct {
	PID       pid.PID
	Name      string
	RaftIndex uint64
}

// listActiveConsistent returns every active CONSISTENT name across all shards.
// CONSISTENT and STRONG entries are distinguished by RequiredNodes: STRONG
// carries the exclusion-holder set, CONSISTENT leaves it empty.
func (s *shardedState) listActiveConsistent() []activeBinding {
	for i := range s.shards {
		s.shards[i].mu.RLock()
	}
	var out []activeBinding
	for i := range s.shards {
		sh := &s.shards[i]
		for name, e := range sh.names {
			if len(e.RequiredNodes) > 0 {
				continue
			}
			out = append(out, activeBinding{Name: name, PID: e.PID, RaftIndex: e.AppliedAt})
		}
	}
	for i := range s.shards {
		s.shards[i].mu.RUnlock()
	}
	return out
}

// allActiveNames returns every active name across all shards (CONSISTENT and
// STRONG). Used by anti-entropy digest construction on members where the FSM
// is authoritative.
func (s *shardedState) allActiveNames() []string {
	for i := range s.shards {
		s.shards[i].mu.RLock()
	}
	var out []string
	for i := range s.shards {
		sh := &s.shards[i]
		for name := range sh.names {
			out = append(out, name)
		}
	}
	for i := range s.shards {
		s.shards[i].mu.RUnlock()
	}
	return out
}

// listActiveStrong returns every promoted Strong name across all shards. It
// holds all shard read-locks for a point-in-time consistent view, matching
// snapshot(). The discriminator is len(RequiredNodes) > 0 — only a promoted
// Strong entry carries the exclusion-holder set.
func (s *shardedState) listActiveStrong() []strongActiveView {
	for i := range s.shards {
		s.shards[i].mu.RLock()
	}
	var out []strongActiveView
	for i := range s.shards {
		sh := &s.shards[i]
		for name, e := range sh.names {
			if len(e.RequiredNodes) == 0 {
				continue
			}
			out = append(out, strongActiveView{Name: name, PID: e.PID, Epoch: e.Epoch})
		}
	}
	for i := range s.shards {
		s.shards[i].mu.RUnlock()
	}
	return out
}

// listPending returns a snapshot of every current pending entry.
func (s *shardedState) listPending() []PendingView {
	s.pendingMu.RLock()
	defer s.pendingMu.RUnlock()
	out := make([]PendingView, 0, len(s.pending))
	for _, e := range s.pending {
		out = append(out, *e.view())
	}
	return out
}

// expiredSnapshot returns a copy of the expired ring buffer.
func (s *shardedState) expiredSnapshot() []ExpiredRecord {
	s.expiredMu.Lock()
	defer s.expiredMu.Unlock()
	out := make([]ExpiredRecord, len(s.expired))
	copy(out, s.expired)
	return out
}

func (e *pendingEntry) view() *PendingView {
	acked := make([]pid.NodeID, 0, len(e.AckSet))
	for n := range e.AckSet {
		acked = append(acked, n)
	}
	missing := make([]pid.NodeID, 0, len(e.RequiredNodes))
	for _, n := range e.RequiredNodes {
		if _, ok := e.AckSet[n]; !ok {
			missing = append(missing, n)
		}
	}
	req := make([]pid.NodeID, len(e.RequiredNodes))
	copy(req, e.RequiredNodes)
	return &PendingView{
		Name:             e.Name,
		PID:              e.PID,
		Epoch:            e.Epoch,
		RequiredNodes:    req,
		AckSet:           acked,
		MissingAcks:      missing,
		DeadlineUnixNano: e.DeadlineUnixNano,
		CreatedAt:        e.CreatedAt,
	}
}

// unregister removes a single name. Returns true if the name existed.
func (s *shardedState) unregister(name string) bool {
	_, ok := s.unregisterEntry(name)
	return ok
}

// unregisterEntry removes a single name and returns a copy of the removed entry.
// The copy lets callers deliver an exclusion release to a promoted Strong name's
// holders (RequiredNodes/Epoch) on terminal removal.
func (s *shardedState) unregisterEntry(name string) (nameEntry, bool) {
	sh := &s.shards[shardFor(name)]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	entry, ok := sh.names[name]
	if !ok {
		return nameEntry{}, false
	}
	removed := *entry

	delete(sh.names, name)

	pidKey := entry.PID.String()
	s.removePIDName(sh, pidKey, name)
	return removed, true
}

// lookupPendingByPID returns the names this PID currently holds a pending
// Strong reservation for. Used by removePID to also drop reservations the
// owning process opened.
func (s *shardedState) lookupPendingByPID(p pid.PID) []string {
	s.pendingMu.RLock()
	defer s.pendingMu.RUnlock()
	out := make([]string, 0, 4)
	for name, e := range s.pending {
		if e.PID == p {
			out = append(out, name)
		}
	}
	return out
}

// strongTerminal identifies a promoted Strong name removed by a terminal so the
// caller can deliver an exclusion release to its holders.
type strongTerminal struct {
	Name          string
	PID           pid.PID
	RequiredNodes []pid.NodeID
	Epoch         uint64
}

// removePIDWithNames removes all names for a given PID across all shards. Locks
// shards in index order to prevent deadlocks. Returns the full list of names
// removed (active CONSISTENT or STRONG). Dissem uses the list to emit one
// tombstone broadcast per name.
func (s *shardedState) removePIDWithNames(p pid.PID) ([]string, int, []strongTerminal) {
	pidKey := p.String()
	removed := 0
	var (
		strongs    []strongTerminal
		removedAll []string
	)

	// Collect names to remove from each shard.
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		names, ok := sh.pidNames[pidKey]
		if ok {
			for _, name := range names {
				e, st := dropNameLocked(sh, name)
				if e == nil {
					continue
				}
				if st != nil {
					strongs = append(strongs, *st)
				}
				removedAll = append(removedAll, name)
				removed++
			}
			delete(sh.pidNames, pidKey)
		}
		sh.mu.Unlock()
	}

	// Also clean the node index (scan all nodes for this pidKey).
	s.nodeIndex.Range(func(key, val any) bool {
		if ps, ok := val.(*pidSet); ok {
			ps.mu.Lock()
			delete(ps.pids, pidKey)
			empty := len(ps.pids) == 0
			ps.mu.Unlock()
			if empty {
				s.nodeIndex.Delete(key)
			}
		}
		return true
	})

	return removedAll, removed, strongs
}

// removedNameEntry records one (name, pid) tuple a removeNodeWithNames pass
// dropped, so the dissem broadcaster can emit a per-name tombstone with the
// correct pid.
type removedNameEntry struct {
	Name string
	PID  pid.PID
}

// removeNodeWithNames removes up to limit names for all PIDs on a node and
// returns the per-name list of removed (name, pid) tuples, the count, whether
// more remain (limit hit), and any Strong terminals for tombstoning. Dissem
// emits one tombstone per removed name.
func (s *shardedState) removeNodeWithNames(nodeID pid.NodeID, limit int) ([]removedNameEntry, int, bool, []strongTerminal) {
	val, ok := s.nodeIndex.Load(nodeID)
	if !ok {
		return nil, 0, false, nil
	}
	ps, ok := val.(*pidSet)
	if !ok {
		return nil, 0, false, nil
	}

	// Snapshot pidKeys without releasing pidSet lock, so a concurrent
	// addToNodeIndex can't create a parallel pidSet between Load and our
	// later Delete (TOCTOU fix preserved).
	ps.mu.Lock()
	pidKeys := make([]string, 0, len(ps.pids))
	for k := range ps.pids {
		pidKeys = append(pidKeys, k)
	}
	ps.mu.Unlock()

	cap := limit
	if cap <= 0 {
		cap = -1 // sentinel for "unlimited"
	}

	removed := 0
	hitLimit := false
	var (
		strongs    []strongTerminal
		removedAll []removedNameEntry
	)
	for i := range s.shards {
		if hitLimit {
			break
		}
		sh := &s.shards[i]
		sh.mu.Lock()
		for _, pidKey := range pidKeys {
			names, ok := sh.pidNames[pidKey]
			if !ok {
				continue
			}
			j := 0
			for ; j < len(names); j++ {
				if cap >= 0 && removed >= cap {
					break
				}
				e, st := dropNameLocked(sh, names[j])
				if e == nil {
					continue
				}
				if st != nil {
					strongs = append(strongs, *st)
				}
				removedAll = append(removedAll, removedNameEntry{Name: names[j], PID: e.PID})
				removed++
			}
			if j == len(names) {
				delete(sh.pidNames, pidKey)
			} else {
				// Some names left for this pidKey on this shard — preserve them.
				sh.pidNames[pidKey] = names[j:]
				hitLimit = true
				break
			}
		}
		sh.mu.Unlock()
	}

	if !hitLimit {
		// Final cleanup: drop the nodeIndex entry now that no entries remain.
		ps.mu.Lock()
		for _, k := range pidKeys {
			delete(ps.pids, k)
		}
		empty := len(ps.pids) == 0
		ps.mu.Unlock()
		if empty {
			s.nodeIndex.Delete(nodeID)
		}
	}

	return removedAll, removed, hitLimit, strongs
}

// --- Helpers ---

// dropNameLocked removes name from the shard's name table and, if it was a
// Strong registration, returns its terminal record for tombstoning. Returns
// the removed entry (nil if the name was absent) so callers can read its PID.
// Caller must hold sh.mu write-lock. Shared by removePIDWithNames and
// removeNodeWithNames, which differ only in their outer iteration (single
// pid vs node-indexed, unlimited vs chunked).
func dropNameLocked(sh *shard, name string) (*nameEntry, *strongTerminal) {
	e, present := sh.names[name]
	if !present {
		return nil, nil
	}
	var st *strongTerminal
	if len(e.RequiredNodes) > 0 {
		req := make([]pid.NodeID, len(e.RequiredNodes))
		copy(req, e.RequiredNodes)
		st = &strongTerminal{Name: name, PID: e.PID, RequiredNodes: req, Epoch: e.Epoch}
	}
	delete(sh.names, name)
	return e, st
}

// removePIDName removes a single name from the pidNames reverse index within a shard.
// Caller must hold sh.mu write-lock.
func (s *shardedState) removePIDName(sh *shard, pidKey, name string) {
	names := sh.pidNames[pidKey]
	for i, n := range names {
		if n == name {
			sh.pidNames[pidKey] = append(names[:i], names[i+1:]...)
			break
		}
	}
	if len(sh.pidNames[pidKey]) == 0 {
		delete(sh.pidNames, pidKey)
	}
}

// addToNodeIndex records that a PID (by its string key) belongs to a node.
func (s *shardedState) addToNodeIndex(nodeID pid.NodeID, pidKey string) {
	val, _ := s.nodeIndex.LoadOrStore(nodeID, &pidSet{pids: make(map[string]struct{})})
	ps := val.(*pidSet)
	ps.mu.Lock()
	ps.pids[pidKey] = struct{}{}
	ps.mu.Unlock()
}

// --- Snapshot support ---

// snapshotEntry is the serialisable form of a single name registration.
// RequiredNodes/Epoch are present only for a promoted Strong name so a terminal
// removal after a snapshot restore can still deliver an exclusion release.
type snapshotEntry struct {
	PID           pid.PID      `codec:"p"`
	Name          string       `codec:"n"`
	NodeID        pid.NodeID   `codec:"d"`
	RequiredNodes []pid.NodeID `codec:"r,omitempty"`
	Epoch         uint64       `codec:"ep,omitempty"`
	AppliedAt     uint64       `codec:"a"`
}

// pendingSnapshotEntry is the serialisable form of a Strong pending reservation.
// Carried alongside `entries` in the FSM snapshot so a recovering node
// resumes mid-flight reservations after a Raft replay.
type pendingSnapshotEntry struct {
	PID              pid.PID      `codec:"p"`
	Name             string       `codec:"n"`
	NodeID           pid.NodeID   `codec:"d"`
	RequiredNodes    []pid.NodeID `codec:"r"`
	AckSet           []pid.NodeID `codec:"as"`
	Epoch            uint64       `codec:"e"`
	DeadlineUnixNano int64        `codec:"dl"`
	CreatedAt        int64        `codec:"c"`
}

// fsmSnapshotPayload is the top-level encoded snapshot: active entries plus the
// pending Strong reservations. Pending is omitempty, so a snapshot with no
// pending reservations decodes with a nil Pending slice.
type fsmSnapshotPayload struct {
	Entries []snapshotEntry        `codec:"e"`
	Pending []pendingSnapshotEntry `codec:"p,omitempty"`
}

// snapshotAbove returns entries with AppliedAt strictly greater than threshold.
// Used by reestablishMonitors to walk only entries that have been added since
// the last reestablish pass — bounds the leader-failover monitor-rebuild cost
// when most state was already monitored. Returns the highest AppliedAt seen
// (0 if no entries).
func (s *shardedState) snapshotAbove(threshold uint64) ([]snapshotEntry, uint64) {
	for i := range s.shards {
		s.shards[i].mu.RLock()
	}

	var entries []snapshotEntry
	var highest uint64
	for i := range s.shards {
		sh := &s.shards[i]
		for name, e := range sh.names {
			if e.AppliedAt <= threshold {
				continue
			}
			entries = append(entries, snapshotEntry{
				Name:      name,
				PID:       e.PID,
				NodeID:    e.NodeID,
				AppliedAt: e.AppliedAt,
			})
			if e.AppliedAt > highest {
				highest = e.AppliedAt
			}
		}
	}

	for i := range s.shards {
		s.shards[i].mu.RUnlock()
	}

	return entries, highest
}

// snapshot returns all entries across all shards as a point-in-time consistent view.
// Acquires all shard read-locks before reading, releases all after, to prevent
// concurrent mutations from producing an inconsistent snapshot.
func (s *shardedState) snapshot() []snapshotEntry {
	// Phase 1: acquire all read-locks in index order
	for i := range s.shards {
		s.shards[i].mu.RLock()
	}

	// Phase 2: read all shards while holding all locks
	var entries []snapshotEntry
	for i := range s.shards {
		sh := &s.shards[i]
		for name, e := range sh.names {
			var req []pid.NodeID
			if len(e.RequiredNodes) > 0 {
				req = make([]pid.NodeID, len(e.RequiredNodes))
				copy(req, e.RequiredNodes)
			}
			entries = append(entries, snapshotEntry{
				Name:          name,
				PID:           e.PID,
				NodeID:        e.NodeID,
				AppliedAt:     e.AppliedAt,
				RequiredNodes: req,
				Epoch:         e.Epoch,
			})
		}
	}

	// Phase 3: release all read-locks
	for i := range s.shards {
		s.shards[i].mu.RUnlock()
	}

	return entries
}

// restore replaces all state from a snapshot. Write-locks all shards
// simultaneously to prevent concurrent reads from seeing partial state.
func (s *shardedState) restore(entries []snapshotEntry, pending []pendingSnapshotEntry) {
	for i := range s.shards {
		s.shards[i].mu.Lock()
	}

	for i := range s.shards {
		s.shards[i].names = make(map[string]*nameEntry)
		s.shards[i].pidNames = make(map[string][]string)
	}
	s.nodeIndex = sync.Map{}

	for _, e := range entries {
		sh := &s.shards[shardFor(e.Name)]
		var req []pid.NodeID
		if len(e.RequiredNodes) > 0 {
			req = make([]pid.NodeID, len(e.RequiredNodes))
			copy(req, e.RequiredNodes)
		}
		sh.names[e.Name] = &nameEntry{
			PID:           e.PID,
			NodeID:        e.NodeID,
			AppliedAt:     e.AppliedAt,
			RequiredNodes: req,
			Epoch:         e.Epoch,
		}
		pidKey := e.PID.String()
		sh.pidNames[pidKey] = append(sh.pidNames[pidKey], e.Name)
		s.addToNodeIndex(e.NodeID, pidKey)
	}

	for i := range s.shards {
		s.shards[i].mu.Unlock()
	}

	s.pendingMu.Lock()
	s.pending = make(map[string]*pendingEntry, len(pending))
	for _, p := range pending {
		ackSet := make(map[pid.NodeID]struct{}, len(p.AckSet))
		for _, n := range p.AckSet {
			ackSet[n] = struct{}{}
		}
		req := make([]pid.NodeID, len(p.RequiredNodes))
		copy(req, p.RequiredNodes)
		s.pending[p.Name] = &pendingEntry{
			Name:             p.Name,
			PID:              p.PID,
			NodeID:           p.NodeID,
			Epoch:            p.Epoch,
			RequiredNodes:    req,
			AckSet:           ackSet,
			DeadlineUnixNano: p.DeadlineUnixNano,
			CreatedAt:        p.CreatedAt,
		}
	}
	s.pendingMu.Unlock()
}

// pendingSnapshot returns a copy of every pending entry as a serialisable slice.
func (s *shardedState) pendingSnapshot() []pendingSnapshotEntry {
	s.pendingMu.RLock()
	defer s.pendingMu.RUnlock()
	out := make([]pendingSnapshotEntry, 0, len(s.pending))
	for _, e := range s.pending {
		acks := make([]pid.NodeID, 0, len(e.AckSet))
		for n := range e.AckSet {
			acks = append(acks, n)
		}
		req := make([]pid.NodeID, len(e.RequiredNodes))
		copy(req, e.RequiredNodes)
		out = append(out, pendingSnapshotEntry{
			Name:             e.Name,
			PID:              e.PID,
			NodeID:           e.NodeID,
			Epoch:            e.Epoch,
			RequiredNodes:    req,
			AckSet:           acks,
			DeadlineUnixNano: e.DeadlineUnixNano,
			CreatedAt:        e.CreatedAt,
		})
	}
	return out
}
