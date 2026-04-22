// SPDX-License-Identifier: MPL-2.0

package globalreg

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

// nameEntry records the owner of a global name.
type nameEntry struct {
	PID       pid.PID
	NodeID    pid.NodeID
	AppliedAt uint64 // Raft log index that created/updated this entry
}

// shardedState is the in-memory state machine, split into shardCount shards.
// All write methods assume the caller holds the appropriate shard write-lock
// (or is being called from FSM.Apply which is single-threaded).
type shardedState struct {
	// nodeIndex maps nodeID → set of pidKeys that belong to that node.
	// Used for efficient CmdRemoveNode.
	nodeIndex sync.Map // nodeID → *pidSet
	shards    [shardCount]shard
}

// pidSet is a thread-safe set of PID string keys used in the node index.
type pidSet struct {
	pids map[string]struct{}
	mu   sync.Mutex
}

func newShardedState() *shardedState {
	s := &shardedState{}
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

// LookupWithFence returns the PID and fencing token (Raft log index) for a name.
// The fencing token should be attached to messages so receivers can reject
// stale references after a name has been re-registered.
func (s *shardedState) LookupWithFence(name string) (pid.PID, uint64, bool) {
	sh := &s.shards[shardFor(name)]
	sh.mu.RLock()
	e, ok := sh.names[name]
	sh.mu.RUnlock()
	if !ok {
		return pid.PID{}, 0, false
	}
	return e.PID, e.AppliedAt, true
}

// ValidateFence checks whether a fencing token is still valid for a name.
// Returns true if the token matches or exceeds the current registration's
// AppliedAt index (i.e., the caller's view is not stale).
func (s *shardedState) ValidateFence(name string, token uint64) bool {
	sh := &s.shards[shardFor(name)]
	sh.mu.RLock()
	e, ok := sh.names[name]
	sh.mu.RUnlock()
	if !ok {
		// Name no longer exists — the caller's reference is stale.
		return false
	}
	return token >= e.AppliedAt
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

// register attempts to insert or verify a name → PID mapping.
// Returns the existing PID and false if the name is taken by another PID.
// Returns the supplied PID and true if the name was successfully registered
// (or was already registered by the same PID — idempotent).
func (s *shardedState) register(name string, p pid.PID, nodeID pid.NodeID, index uint64) (pid.PID, bool) {
	sh := &s.shards[shardFor(name)]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	if existing, ok := sh.names[name]; ok {
		if existing.PID == p {
			// Idempotent re-registration.
			return p, true
		}
		return existing.PID, false
	}

	sh.names[name] = &nameEntry{PID: p, NodeID: nodeID, AppliedAt: index}

	pidKey := p.String()
	sh.pidNames[pidKey] = append(sh.pidNames[pidKey], name)

	// Update node index.
	s.addToNodeIndex(nodeID, pidKey)

	return p, true
}

// unregister removes a single name. Returns true if the name existed.
func (s *shardedState) unregister(name string) bool {
	sh := &s.shards[shardFor(name)]
	sh.mu.Lock()
	defer sh.mu.Unlock()

	entry, ok := sh.names[name]
	if !ok {
		return false
	}

	delete(sh.names, name)

	pidKey := entry.PID.String()
	s.removePIDName(sh, pidKey, name)
	return true
}

// removePID removes all names for a given PID across all shards.
// This locks shards in index order to prevent deadlocks.
func (s *shardedState) removePID(p pid.PID) int {
	pidKey := p.String()
	removed := 0

	// Collect names to remove from each shard.
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		names, ok := sh.pidNames[pidKey]
		if ok {
			for _, name := range names {
				delete(sh.names, name)
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

	return removed
}

// removeNode removes all names for all PIDs on a node.
// Holds the pidSet lock while deleting from nodeIndex to prevent a concurrent
// register from creating a new pidSet for the same node between the delete
// and the shard cleanup (TOCTOU fix).
func (s *shardedState) removeNode(nodeID pid.NodeID) int {
	val, ok := s.nodeIndex.Load(nodeID)
	if !ok {
		return 0
	}
	ps, ok := val.(*pidSet)
	if !ok {
		return 0
	}

	// Hold pidSet lock while extracting keys and deleting from nodeIndex.
	// This prevents concurrent addToNodeIndex from creating a new pidSet
	// in the gap between Load and Delete.
	ps.mu.Lock()
	pidKeys := make([]string, 0, len(ps.pids))
	for k := range ps.pids {
		pidKeys = append(pidKeys, k)
	}
	s.nodeIndex.Delete(nodeID)
	ps.mu.Unlock()

	removed := 0
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		for _, pidKey := range pidKeys {
			names, ok := sh.pidNames[pidKey]
			if !ok {
				continue
			}
			for _, name := range names {
				delete(sh.names, name)
				removed++
			}
			delete(sh.pidNames, pidKey)
		}
		sh.mu.Unlock()
	}

	return removed
}

// --- Helpers ---

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
type snapshotEntry struct {
	Name      string     `codec:"n"`
	PID       pid.PID    `codec:"p"`
	NodeID    pid.NodeID `codec:"d"`
	AppliedAt uint64     `codec:"a"`
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
			entries = append(entries, snapshotEntry{
				Name:      name,
				PID:       e.PID,
				NodeID:    e.NodeID,
				AppliedAt: e.AppliedAt,
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
func (s *shardedState) restore(entries []snapshotEntry) {
	// Acquire all write-locks before clearing
	for i := range s.shards {
		s.shards[i].mu.Lock()
	}

	// Clear everything under full lock
	for i := range s.shards {
		s.shards[i].names = make(map[string]*nameEntry)
		s.shards[i].pidNames = make(map[string][]string)
	}
	s.nodeIndex = sync.Map{}

	// Re-populate while still holding all locks
	for _, e := range entries {
		sh := &s.shards[shardFor(e.Name)]
		sh.names[e.Name] = &nameEntry{PID: e.PID, NodeID: e.NodeID, AppliedAt: e.AppliedAt}
		pidKey := e.PID.String()
		sh.pidNames[pidKey] = append(sh.pidNames[pidKey], e.Name)
		s.addToNodeIndex(e.NodeID, pidKey)
	}

	// Release all write-locks
	for i := range s.shards {
		s.shards[i].mu.Unlock()
	}
}
