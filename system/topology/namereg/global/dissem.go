// SPDX-License-Identifier: MPL-2.0

package global

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/pid"
	"go.uber.org/zap"
)

// DissemKind is the multiplex byte that membership routes globalreg
// active-binding dissemination frames on. Distinct from eventualreg's
// 0xE1; 0xC1 marks the CONSISTENT/STRONG active-binding read plane.
const DissemKind byte = 0xC1

const (
	// dissemQueueCap bounds the leader-side broadcast queue. The leader injects
	// one delta per active-transition; under steady state the queue drains
	// every gossip cycle.
	dissemQueueCap = 4096

	// dissemMaxFrameBytes mirrors eventualreg's safe UDP packet ceiling.
	dissemMaxFrameBytes = 1400

	// tombstoneGCInterval is the period the cache checks whether tombstone
	// expiry is explicitly enabled. Cheap walk; intentionally infrequent.
	tombstoneGCInterval = 5 * time.Minute

	// tombstoneWallFloor is the default deleted-binding fence retention for the
	// AP cache. Zero disables age-only GC by default: without per-peer delete
	// acknowledgements or a durable delete vector, a finite default TTL is only
	// an operator assumption about max partition length.
	tombstoneWallFloor = 0

	// antiEntropyInterval drives the periodic digest exchange with a derived
	// member. Bounded; one peer per tick.
	antiEntropyInterval = 30 * time.Second
)

// BindingEvent describes an active-binding transition the FSM committed. Emitted
// from FSM.Apply on every replica; only the leader translates it to a gossip
// broadcast (raft is the source of truth, gossip is the AP dissemination).
type BindingEvent struct {
	Name      string
	PID       pid.PID
	RaftIndex uint64
	Deleted   bool
}

// bindingEntry is the in-memory cache value: name -> binding. RaftIndex is the
// LWW dot — strictly monotonic per-name because raft log indices are globally
// monotonic. Wall is the receipt wall-time (used for tombstone GC).
type bindingEntry struct {
	PID       pid.PID
	RaftIndex uint64
	Wall      int64
	Deleted   bool
}

// Dissem holds the active-binding cache and drives gossip dissemination.
// The cache fills via:
//   - LeaderBroadcast (leader-driven, called by the Service's FSM hooks).
//   - NotifyMsg       (incoming gossip from any other node's leader-broadcast).
//   - SeedFromSnapshot (the join-epoch snapshot path).
//   - LocalApply      (member's own FSM.Apply seeding the cache so members can
//     use the cache as a fast path alongside their FSM).
type Dissem struct {
	cache              map[string]bindingEntry
	logger             *zap.Logger
	stopCh             chan struct{}
	localNode          pid.NodeID
	queue              []bindingDelta
	tombstoneRetention time.Duration
	qDropped           uint64
	mu                 sync.RWMutex
	qMu                sync.Mutex
}

// bindingDelta is the wire form of one cache mutation. Encoded compactly so
// many deltas fit into one ~1400-byte UDP frame.
//
// Wire format (little-endian unless noted):
//
//	flags:1            (bit 0 = deleted)
//	raft_index:8
//	wall_unix_nano:8
//	name_len:2 | name
//	pid_node_len:1 | pid_node
//	pid_host_len:1 | pid_host
//	pid_uniq_len:2 | pid_uniq
type bindingDelta struct {
	PID       pid.PID
	Name      string
	RaftIndex uint64
	Wall      int64
	Deleted   bool
}

// DissemOption tunes the active-binding dissemination cache.
type DissemOption func(*Dissem)

// WithTombstoneRetention tunes how long deleted bindings remain in the AP cache
// to fence stale lower-index live gossip. A non-positive value keeps the
// default.
func WithTombstoneRetention(retention time.Duration) DissemOption {
	return func(d *Dissem) {
		if retention > 0 {
			d.tombstoneRetention = retention
		}
	}
}

// NewDissem builds an empty dissemination plane bound to the local node ID.
func NewDissem(localNode pid.NodeID, logger *zap.Logger, opts ...DissemOption) *Dissem {
	if logger == nil {
		logger = zap.NewNop()
	}
	d := &Dissem{
		localNode:          localNode,
		logger:             logger.Named("global.dissem"),
		cache:              make(map[string]bindingEntry, 64),
		stopCh:             make(chan struct{}),
		tombstoneRetention: tombstoneWallFloor,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Kind reports the multiplex byte for globalreg dissemination frames.
func (d *Dissem) Kind() byte { return DissemKind }

// Lookup reports the cached PID for name, or (zero, false) on miss / tombstone.
// Tombstones are surfaced to the caller as a not-found result so the read
// plane sees a removed binding identically to a never-seen one.
func (d *Dissem) Lookup(name string) (pid.PID, bool) {
	d.mu.RLock()
	e, ok := d.cache[name]
	d.mu.RUnlock()
	if !ok || e.Deleted {
		return pid.PID{}, false
	}
	return e.PID, true
}

// CachedIndex reports the raft index of the held cache entry for name (zero
// when none). Used by tests and anti-entropy digest construction.
func (d *Dissem) CachedIndex(name string) uint64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if e, ok := d.cache[name]; ok {
		return e.RaftIndex
	}
	return 0
}

// CacheSize reports the number of cache entries (including tombstones).
func (d *Dissem) CacheSize() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.cache)
}

// merge applies one inbound delta with LWW-by-RaftIndex. Returns true when the
// local cache moved (newer index installed). A same-or-lower index is dropped.
func (d *Dissem) merge(in bindingDelta) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	existing, ok := d.cache[in.Name]
	if ok {
		if in.RaftIndex < existing.RaftIndex {
			return false
		}
		if in.RaftIndex == existing.RaftIndex {
			// Same-index tie-break: a live binding deterministically wins over a
			// tombstone regardless of gossip arrival order; otherwise keep. (Distinct
			// ops carry distinct raft indices, so this only guards a degenerate case.)
			if !existing.Deleted || in.Deleted {
				return false
			}
		}
	}
	d.cache[in.Name] = bindingEntry{
		PID:       in.PID,
		RaftIndex: in.RaftIndex,
		Wall:      in.Wall,
		Deleted:   in.Deleted,
	}
	return true
}

// LeaderBroadcast queues a delta for outbound gossip. The Service calls this
// only when raftSvc.IsLeader() so a follower's FSM.Apply does NOT inject —
// raft replicates the same committed fact to every member, and the leader is
// the single source of truth for the AP broadcast.
//
// The delta is unconditionally enqueued (the cache is only a local fast path;
// the broadcast is the AP distribution of a CP-decided fact). Cache update
// follows the same LWW rule applied to all deltas — strictly monotonic by
// raftIndex.
func (d *Dissem) LeaderBroadcast(ev BindingEvent) {
	wall := time.Now().UnixNano()
	delta := bindingDelta{
		Name:      ev.Name,
		PID:       ev.PID,
		RaftIndex: ev.RaftIndex,
		Wall:      wall,
		Deleted:   ev.Deleted,
	}
	d.merge(delta)
	d.enqueue(delta)
}

// LocalApply seeds the local cache from a follower's FSM.Apply without
// queuing a broadcast. Members keep their FSM authoritative; the cache is a
// fast path on the same node.
func (d *Dissem) LocalApply(ev BindingEvent) {
	d.merge(bindingDelta{
		Name:      ev.Name,
		PID:       ev.PID,
		RaftIndex: ev.RaftIndex,
		Wall:      time.Now().UnixNano(),
		Deleted:   ev.Deleted,
	})
}

// SeedFromSnapshot installs cache entries from the join-epoch snapshot. The
// snapshot is the leader's authoritative PENDING∪ACTIVE view at the time of
// fetch; deleted=false because the snapshot lists only live bindings. Pending
// reservations are skipped — the cache holds only ACTIVE bindings (a Lookup
// for a pending name resolves only after promotion).
func (d *Dissem) SeedFromSnapshot(name string, p pid.PID, raftIndex uint64) {
	d.merge(bindingDelta{
		Name:      name,
		PID:       p,
		RaftIndex: raftIndex,
		Wall:      time.Now().UnixNano(),
		Deleted:   false,
	})
}

// enqueue appends a delta to the outbound broadcast queue, dropping the
// oldest entries on overflow (telemetry surfaces qDropped). The newest delta
// is always retained because it carries the latest committed index.
func (d *Dissem) enqueue(delta bindingDelta) {
	d.qMu.Lock()
	defer d.qMu.Unlock()
	if len(d.queue) >= dissemQueueCap {
		// Drop oldest. The newest entry's higher RaftIndex wins on merge so a
		// receiver that missed the drop heals from the next anti-entropy.
		d.queue = d.queue[1:]
		d.qDropped++
	}
	d.queue = append(d.queue, delta)
}

// GetBroadcasts pulls up to `byteBudget` worth of queued deltas, packed into
// frames bounded by dissemMaxFrameBytes. `headerOverhead` is the per-frame
// budget memberlist reserves; we account for it identically to eventual.
func (d *Dissem) GetBroadcasts(headerOverhead, byteBudget int) [][]byte {
	d.qMu.Lock()
	defer d.qMu.Unlock()
	if len(d.queue) == 0 {
		return nil
	}

	perFrameLimit := dissemMaxFrameBytes - headerOverhead
	if perFrameLimit < 64 {
		perFrameLimit = 64
	}
	if byteBudget <= 0 {
		byteBudget = dissemMaxFrameBytes
	}

	var (
		out      [][]byte
		emitted  int
		frame    []byte
		commit   []int
		skipped  []int
		consumed []int
	)
	frame = make([]byte, 0, perFrameLimit)

	flush := func() bool {
		if len(frame) == 0 {
			return true
		}
		cost := len(frame) + headerOverhead
		if emitted+cost > byteBudget {
			return false
		}
		out = append(out, frame)
		emitted += cost
		commit = append(commit, consumed...)
		consumed = consumed[:0]
		frame = make([]byte, 0, perFrameLimit)
		return true
	}

	for i, delta := range d.queue {
		buf, err := encodeBindingDelta(nil, delta)
		if err != nil {
			skipped = append(skipped, i)
			continue
		}
		if len(buf) > perFrameLimit {
			skipped = append(skipped, i)
			continue
		}
		room := byteBudget - emitted - headerOverhead
		if room <= 0 {
			break
		}
		if room > perFrameLimit {
			room = perFrameLimit
		}
		if len(frame)+len(buf) > room {
			if !flush() {
				break
			}
			room = byteBudget - emitted - headerOverhead
			if room <= 0 || len(buf) > room {
				break
			}
		}
		frame = append(frame, buf...)
		consumed = append(consumed, i)
	}
	flush()

	if len(commit) == 0 && len(skipped) == 0 {
		return out
	}
	remove := make(map[int]struct{}, len(commit)+len(skipped))
	for _, i := range commit {
		remove[i] = struct{}{}
	}
	for _, i := range skipped {
		remove[i] = struct{}{}
	}
	keep := d.queue[:0]
	for i, e := range d.queue {
		if _, drop := remove[i]; drop {
			continue
		}
		keep = append(keep, e)
	}
	d.queue = keep
	return out
}

// NotifyMsg merges one inbound gossip frame. Every delta is folded LWW-by-index.
func (d *Dissem) NotifyMsg(payload []byte) {
	deltas, err := decodeBindingFrame(payload)
	if err != nil {
		d.logger.Debug("malformed dissem frame", zap.Error(err))
	}
	for _, delta := range deltas {
		d.merge(delta)
	}
}

// LocalState returns nil: dissem does not use memberlist's bulk push/pull
// transfer. Name bindings propagate over the gossip delta channel, and a
// fresh peer's full snapshot rides the JoinNameEpoch RPC. The method exists
// only to satisfy the memberlist delegate interface.
func (d *Dissem) LocalState(_ bool) []byte { return nil }

// MergeRemoteState is a no-op for dissem — the snapshot path is the
// JoinNameEpoch RPC, not memberlist's bulk-transfer. Kept to satisfy the
// UserDelegate contract.
func (d *Dissem) MergeRemoteState(_ []byte, _ bool) {}

// Stop closes the GC ticker (if running).
func (d *Dissem) Stop() {
	select {
	case <-d.stopCh:
		return
	default:
		close(d.stopCh)
	}
}

// RunGC sweeps tombstones older than tombstoneRetention. Bounded retention is
// safe for this AP cache because Raft/FSM is the source of truth and memberlist
// retransmission windows are finite; the floor exists only to fence stale
// lower-index live gossip.
func (d *Dissem) RunGC() {
	t := time.NewTicker(tombstoneGCInterval)
	defer t.Stop()
	for {
		select {
		case <-d.stopCh:
			return
		case <-t.C:
			d.sweepTombstones(time.Now().UnixNano())
		}
	}
}

// sweepTombstones drops every tombstone whose wall-time predates the floor. Held
// only briefly under the cache write-lock.
func (d *Dissem) sweepTombstones(nowNano int64) int {
	retention := d.tombstoneRetention
	if retention <= 0 {
		return 0
	}
	floor := nowNano - retention.Nanoseconds()
	d.mu.Lock()
	defer d.mu.Unlock()
	removed := 0
	for name, e := range d.cache {
		if !e.Deleted {
			continue
		}
		if e.Wall < floor {
			delete(d.cache, name)
			removed++
		}
	}
	return removed
}

// Digest is a compact projection of the cache used by anti-entropy:
// name -> (raftIndex, deleted). Sorted in name order so comparison is
// deterministic.
type Digest struct {
	Entries []DigestEntry
}

// DigestEntry is one name + index in a digest. Deleted entries are included so
// anti-entropy can repair peers that missed a tombstone broadcast.
type DigestEntry struct {
	Name      string
	RaftIndex uint64
	Deleted   bool
}

// DigestSnapshot returns a sorted snapshot of (name, raftIndex, deleted) for
// the cache. Used by anti-entropy mismatch detection.
func (d *Dissem) DigestSnapshot() Digest {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]DigestEntry, 0, len(d.cache))
	for name, e := range d.cache {
		out = append(out, DigestEntry{Name: name, RaftIndex: e.RaftIndex, Deleted: e.Deleted})
	}
	return Digest{Entries: out}
}

// --- wire encoding ---

// encodeBindingDelta appends one delta to buf and returns the new slice.
func encodeBindingDelta(buf []byte, d bindingDelta) ([]byte, error) {
	if len(d.Name) > 0xFFFF {
		return buf, fmt.Errorf("globalreg dissem: name too long: %d", len(d.Name))
	}
	if len(d.PID.Node) > 0xFF || len(d.PID.Host) > 0xFF || len(d.PID.UniqID) > 0xFFFF {
		return buf, fmt.Errorf("globalreg dissem: pid fields too long")
	}
	var flags byte
	if d.Deleted {
		flags |= 0x01
	}
	buf = append(buf, flags)
	buf = binary.LittleEndian.AppendUint64(buf, d.RaftIndex)
	buf = binary.LittleEndian.AppendUint64(buf, uint64(d.Wall))
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(d.Name)))
	buf = append(buf, d.Name...)
	buf = append(buf, byte(len(d.PID.Node)))
	buf = append(buf, d.PID.Node...)
	buf = append(buf, byte(len(d.PID.Host)))
	buf = append(buf, d.PID.Host...)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(d.PID.UniqID)))
	buf = append(buf, d.PID.UniqID...)
	return buf, nil
}

// decodeBindingFrame reads zero or more deltas from a frame body.
func decodeBindingFrame(data []byte) ([]bindingDelta, error) {
	var out []bindingDelta
	for len(data) > 0 {
		d, n, err := decodeBindingDelta(data)
		if err != nil {
			return out, err
		}
		out = append(out, d)
		data = data[n:]
	}
	return out, nil
}

func decodeBindingDelta(data []byte) (bindingDelta, int, error) {
	const minHeader = 1 + 8 + 8 + 2
	if len(data) < minHeader {
		return bindingDelta{}, 0, errors.New("globalreg dissem: short delta header")
	}
	pos := 0
	flags := data[pos]
	pos++
	raftIndex := binary.LittleEndian.Uint64(data[pos:])
	pos += 8
	wall := int64(binary.LittleEndian.Uint64(data[pos:]))
	pos += 8
	nameLen := int(binary.LittleEndian.Uint16(data[pos:]))
	pos += 2
	if len(data) < pos+nameLen+1 {
		return bindingDelta{}, 0, errors.New("globalreg dissem: short name")
	}
	name := string(data[pos : pos+nameLen])
	pos += nameLen

	nodeLen := int(data[pos])
	pos++
	if len(data) < pos+nodeLen+1 {
		return bindingDelta{}, 0, errors.New("globalreg dissem: short pid.node")
	}
	node := string(data[pos : pos+nodeLen])
	pos += nodeLen

	hostLen := int(data[pos])
	pos++
	if len(data) < pos+hostLen+2 {
		return bindingDelta{}, 0, errors.New("globalreg dissem: short pid.host")
	}
	host := string(data[pos : pos+hostLen])
	pos += hostLen

	uniqLen := int(binary.LittleEndian.Uint16(data[pos:]))
	pos += 2
	if len(data) < pos+uniqLen {
		return bindingDelta{}, 0, errors.New("globalreg dissem: short pid.uniq")
	}
	uniq := string(data[pos : pos+uniqLen])
	pos += uniqLen

	return bindingDelta{
		Name:      name,
		PID:       pid.PID{Node: node, Host: host, UniqID: uniq},
		RaftIndex: raftIndex,
		Wall:      wall,
		Deleted:   flags&0x01 != 0,
	}, pos, nil
}
