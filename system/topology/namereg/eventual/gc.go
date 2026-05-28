// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"sync"
	"time"
)

// DefaultWallFloor is the wall-clock cutoff at which a tombstone is dropped
// regardless of safe-counter ack. Bounds memory if a node never acks back
// (e.g. permanent partition).
const DefaultWallFloor = 15 * time.Minute

// TombstoneTracker records, per origin node, the highest counter that EVERY
// alive cluster member has acknowledged. Acknowledgment comes from digest
// exchanges: when peer P replies with a CV showing `cv[origin] >= X`, we
// note that P has seen X. The "safe" counter for an origin is the min over
// all alive peers of the highest counter we've seen them ack. Tombstones
// with `(origin, counter ≤ safe[origin])` are guaranteed to have been seen
// by every alive node and can be dropped.
type TombstoneTracker struct {
	// peerCV[peerNodeStr][originNodeID] = highest counter peer acked for origin.
	peerCV map[string]map[uint32]uint64
	mu     sync.Mutex
}

// NewTombstoneTracker returns an empty tracker.
func NewTombstoneTracker() *TombstoneTracker {
	return &TombstoneTracker{peerCV: make(map[string]map[uint32]uint64)}
}

// RecordAck records that `peer` has acknowledged a CV (per-origin counters
// indexed by compact node ID, as returned by State.CVSnapshot).
func (t *TombstoneTracker) RecordAck(peer string, cv []uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	m, ok := t.peerCV[peer]
	if !ok {
		m = make(map[uint32]uint64, len(cv))
		t.peerCV[peer] = m
	}
	for origin, counter := range cv {
		if counter == 0 {
			continue
		}
		if cur := m[uint32(origin)]; counter > cur {
			m[uint32(origin)] = counter
		}
	}
}

// ForgetPeer drops a peer's records (e.g. when it leaves the cluster).
// Without this, a dead peer's missing acks would block tombstone GC forever.
func (t *TombstoneTracker) ForgetPeer(peer string) {
	t.mu.Lock()
	delete(t.peerCV, peer)
	t.mu.Unlock()
}

// SafeCounters returns, per origin (compact ID), the min counter that every
// CURRENTLY ALIVE peer has acked. A peer not in `alive` is excluded — that's
// what unblocks GC after a node leaves.
//
// `numOrigins` should be ≥ the highest origin ID we know about; the returned
// slice is indexed by compact node ID and has length `numOrigins`.
func (t *TombstoneTracker) SafeCounters(alive map[string]struct{}, numOrigins int) []uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	if numOrigins == 0 {
		return nil
	}
	safe := make([]uint64, numOrigins)
	for i := range safe {
		safe[i] = 1<<63 - 1 // start at max; min-reduce per peer
	}
	contributors := 0
	for peer, m := range t.peerCV {
		if _, ok := alive[peer]; !ok {
			continue
		}
		contributors++
		// For each origin we track for this peer, take the min into safe[origin].
		for origin := uint32(0); int(origin) < numOrigins; origin++ {
			seen := m[origin]
			if seen < safe[origin] {
				safe[origin] = seen
			}
		}
	}
	if contributors == 0 {
		// No alive peers (single-node cluster?). Return zero counters so
		// nothing is GC'd by safe-counter rule. Wall-floor still applies.
		for i := range safe {
			safe[i] = 0
		}
	}
	return safe
}

// GCRunner runs periodic tombstone reaping against a State.
type GCRunner struct {
	state       *State
	tracker     *TombstoneTracker
	aliveFn     func() map[string]struct{}
	now         func() time.Time
	done        chan struct{}
	onSafe      func(int)
	onWallFloor func(int)
	onReclaim   func(int)
	period      time.Duration
	floor       time.Duration
	stopOnce    sync.Once
}

// GCConfig configures the runner.
type GCConfig struct {
	State       *State
	Tracker     *TombstoneTracker
	AliveFn     func() map[string]struct{} // current alive peer node strings
	Now         func() time.Time           // override for tests
	OnSafe      func(int)                  // counter callback for safe-GC drops
	OnWallFloor func(int)                  // counter callback for wall-floor drops
	OnReclaim   func(int)                  // counter callback for reclaimed compact ids
	Period      time.Duration              // ticker cadence (default 20 s)
	WallFloor   time.Duration              // age at which tombstones drop (default 15 min)
}

// NewGCRunner constructs a runner. Must call Start to begin.
func NewGCRunner(cfg GCConfig) *GCRunner {
	if cfg.Period <= 0 {
		cfg.Period = 20 * time.Second
	}
	if cfg.WallFloor <= 0 {
		cfg.WallFloor = DefaultWallFloor
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &GCRunner{
		state:       cfg.State,
		tracker:     cfg.Tracker,
		aliveFn:     cfg.AliveFn,
		period:      cfg.Period,
		floor:       cfg.WallFloor,
		now:         cfg.Now,
		done:        make(chan struct{}),
		onSafe:      cfg.OnSafe,
		onWallFloor: cfg.OnWallFloor,
		onReclaim:   cfg.OnReclaim,
	}
}

// Start begins the background reaper. Returns immediately.
func (g *GCRunner) Start() {
	go g.loop()
}

// Stop signals the reaper to exit. A pass already in progress runs to
// completion; Stop does not block on it. Idempotent.
func (g *GCRunner) Stop() {
	g.stopOnce.Do(func() { close(g.done) })
}

func (g *GCRunner) loop() {
	t := time.NewTicker(g.period)
	defer t.Stop()
	for {
		select {
		case <-g.done:
			return
		case <-t.C:
			g.runOnce()
		}
	}
}

// runOnce executes a single reap pass — exposed for tests.
func (g *GCRunner) runOnce() {
	cv := g.state.CVSnapshot()
	alive := g.aliveFn()
	safe := g.tracker.SafeCounters(alive, len(cv))
	nowMs := g.now().UnixMilli()
	floorMs := g.floor.Milliseconds()
	gcSafe, gcFloor := g.state.ReapTombstones(safe, nowMs, floorMs)
	if g.onSafe != nil && gcSafe > 0 {
		g.onSafe(gcSafe)
	}
	if g.onWallFloor != nil && gcFloor > 0 {
		g.onWallFloor(gcFloor)
	}
	// Reclaim interned compact ids whose dots were all reaped above and whose
	// origin is no longer a live member. Runs after ReapTombstones so a just-
	// dropped tombstone's refcount has already fallen to zero this pass.
	if reclaimed := g.state.ReclaimUnreferencedNodes(alive); reclaimed > 0 && g.onReclaim != nil {
		g.onReclaim(reclaimed)
	}
}
