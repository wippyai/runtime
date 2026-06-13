// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/event"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/crdt"
	"go.uber.org/zap"
)

// crdtReapInterval is how often expired lease keys are swept locally. Each node
// reaps independently; the resulting tombstones gossip so the cluster converges.
const crdtReapInterval = time.Second

// crdtTombstoneGCInterval is how often the local tombstone GC pass runs.
const crdtTombstoneGCInterval = time.Minute

// DefaultTombstoneRetention is the default age-only delete-tombstone retention
// for store.kv.crdt. A non-positive value disables age-only GC. Clustered
// deployments can opt into acknowledgement-gated GC by wiring a peer set; a
// positive wall floor is only an explicit max-partition tradeoff for operators
// that want a hard memory bound independent of peer acks.
const DefaultTombstoneRetention time.Duration = 0

type tombstoneDot struct {
	node    uint32
	counter uint64
}

// wallScale shifts the real millisecond clock left so a per-node tiebreak fits
// in the low digits. crdt.State.Apply resolves a cross-origin conflict purely by
// Wall and otherwise keeps the current entry, so two distinct concurrent writers
// at the same millisecond would diverge. Embedding a deterministic per-node
// tiebreak guarantees distinct, consistently-ordered walls — hence convergence —
// while real-time order still dominates. 1.7e12 ms * 1e6 stays within int64.
const wallScale = 1_000_000

// CRDTEngine implements kvapi.Engine over a gossip CRDT. It is node-wide;
// store.kv.crdt entries scope it by key namespace. Writes mutate local state and
// enqueue a delta the gossip delegate disseminates; reads are local. CAS and
// SetIfAbsent are best-effort under eventual consistency — use store.kv.raft for
// linearizable conditional writes.
type CRDTEngine struct {
	bus            event.Bus
	ctx            context.Context
	durableNS      map[string]struct{}
	queue          *crdt.BroadcastQueue
	logger         *zap.Logger
	state          *crdt.State
	cancel         context.CancelFunc
	peerTombAck    map[string]map[tombstoneDot]struct{}
	aliveFn        func() map[string]struct{}
	leaseKeys      map[kvapi.LeaseID]map[string]struct{}
	deadlines      map[kvapi.LeaseID]time.Time
	keyLease       map[string]kvapi.LeaseID
	localNode      string
	system         event.System
	dataDir        string
	wg             sync.WaitGroup
	snapInterval   time.Duration
	gcInterval     time.Duration
	tombstoneFloor time.Duration
	leaseSeq       atomic.Uint64
	tiebreak       int64
	mu             sync.Mutex
}

// NewCRDTEngine builds the node-wide crdt engine.
func NewCRDTEngine(localNode string, bus event.Bus, logger *zap.Logger) *CRDTEngine {
	if logger == nil {
		logger = zap.NewNop()
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(localNode))
	return &CRDTEngine{
		state:          crdt.NewState(localNode),
		queue:          crdt.NewBroadcastQueue(localNode, 0),
		bus:            bus,
		logger:         logger.Named("kv-crdt"),
		system:         "kv:crdt",
		localNode:      localNode,
		tiebreak:       int64(h.Sum32() % wallScale),
		peerTombAck:    make(map[string]map[tombstoneDot]struct{}),
		durableNS:      make(map[string]struct{}),
		snapInterval:   30 * time.Second,
		gcInterval:     crdtTombstoneGCInterval,
		tombstoneFloor: DefaultTombstoneRetention,
		leaseKeys:      make(map[kvapi.LeaseID]map[string]struct{}),
		deadlines:      make(map[kvapi.LeaseID]time.Time),
		keyLease:       make(map[string]kvapi.LeaseID),
	}
}

// wall returns a totally-ordered logical write timestamp: real milliseconds in
// the high digits, a deterministic per-node tiebreak in the low digits.
func (e *CRDTEngine) wall() int64 { return time.Now().UnixMilli()*wallScale + e.tiebreak }

// SetDurability enables fs snapshots for durable namespaces under dataDir. With
// dataDir empty the engine stays purely in-memory (reconverges from peers).
func (e *CRDTEngine) SetDurability(dataDir string, interval time.Duration) {
	e.mu.Lock()
	e.dataDir = dataDir
	if interval > 0 {
		e.snapInterval = interval
	}
	e.mu.Unlock()
}

// SetTombstoneRetention sets the age-only delete tombstone retention. A
// non-positive duration disables age-only GC and keeps delete fences until
// acknowledgement-gated GC can prove the configured peer set has observed each
// delete.
func (e *CRDTEngine) SetTombstoneRetention(retention time.Duration) {
	e.mu.Lock()
	e.tombstoneFloor = retention
	e.mu.Unlock()
}

// SetAlivePeers wires the peer set whose tombstone acknowledgements are
// required by GC. The returned map must include the local node when it is alive.
// Omitting a peer is safe only when that peer's stale AP state has been retired
// or fenced from rejoining under the same node ID; counting an acknowledged peer
// is safe only when it cannot roll back to a pre-ack snapshot. A nil function
// disables acknowledgement-gated GC and preserves the default
// infinite-retention safety behavior.
func (e *CRDTEngine) SetAlivePeers(fn func() map[string]struct{}) {
	e.mu.Lock()
	e.aliveFn = fn
	e.mu.Unlock()
}

// MarkDurable records that a namespace's keys should be persisted. Called by the
// store manager for each durable store.kv.crdt entry.
func (e *CRDTEngine) MarkDurable(namespace string) {
	e.mu.Lock()
	e.durableNS[namespace] = struct{}{}
	e.mu.Unlock()
}

func (e *CRDTEngine) isDurableKey(key string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for ns := range e.durableNS {
		if strings.HasPrefix(key, ns+":") {
			return true
		}
	}
	return false
}

func (e *CRDTEngine) snapPath() string { return filepath.Join(e.dataDir, "kvcrdt.snap") }

// snapshotDurable atomically writes every durable-namespace entry to disk using
// the portable wire format (origin strings, not node-local interned ids).
func (e *CRDTEngine) snapshotDurable() error {
	if e.dataDir == "" {
		return nil
	}
	var out []byte
	for shard := 0; shard < crdt.ShardCount; shard++ {
		for _, en := range e.state.ShardEntries(shard) {
			if !e.isDurableKey(en.Key) {
				continue
			}
			if buf, err := crdt.EncodeDelta(nil, &en, e.state.NodeString(en.Node)); err == nil {
				out = append(out, buf...)
			}
		}
	}
	if err := os.MkdirAll(e.dataDir, 0o750); err != nil {
		return err
	}
	tmp := e.snapPath() + ".tmp"
	if err := os.WriteFile(tmp, out, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, e.snapPath())
}

// loadDurable replays a prior snapshot into local state on startup.
func (e *CRDTEngine) loadDurable() error {
	if e.dataDir == "" {
		return nil
	}
	data, err := os.ReadFile(e.snapPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	e.OnFrame(data)
	return nil
}

// Start replays any durable snapshot, then launches the lease reaper and (when
// durability is configured) the periodic snapshotter.
func (e *CRDTEngine) Start(ctx context.Context) error {
	e.ctx, e.cancel = context.WithCancel(ctx)
	if err := e.loadDurable(); err != nil {
		e.logger.Warn("crdt: load snapshot failed", zap.Error(err))
	}
	e.wg.Add(1)
	go e.reaper()
	e.wg.Add(1)
	go e.tombstoneReaper()
	if e.dataDir != "" {
		e.wg.Add(1)
		go e.snapshotter()
	}
	return nil
}

func (e *CRDTEngine) snapshotter() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.snapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			if err := e.snapshotDurable(); err != nil {
				e.logger.Warn("crdt: final snapshot failed", zap.Error(err))
			}
			return
		case <-ticker.C:
			if err := e.snapshotDurable(); err != nil {
				e.logger.Warn("crdt: snapshot failed", zap.Error(err))
			}
		}
	}
}

// Stop halts the reaper.
func (e *CRDTEngine) Stop() error {
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
	return nil
}

// EventSystem returns the watch event.System.
func (e *CRDTEngine) EventSystem() event.System { return e.system }

// --- kvapi.Engine reads ---

func (e *CRDTEngine) Get(key string) (kvapi.Entry, error) {
	en, ok := e.state.LookupEntry(key)
	if !ok {
		return kvapi.Entry{}, kvapi.ErrKeyNotFound
	}
	return kvapi.Entry{Key: key, Value: en.Value, Version: en.Counter}, nil
}

func (e *CRDTEngine) Scan(prefix string, fn func(kvapi.Entry) bool) error {
	e.state.Range(prefix, "", func(en crdt.Entry) bool {
		if prefix != "" && !strings.HasPrefix(en.Key, prefix) {
			return true
		}
		return fn(kvapi.Entry{Key: en.Key, Value: en.Value, Version: en.Counter})
	})
	return nil
}

func (e *CRDTEngine) Watch(ctx context.Context, prefix string) (kvapi.Watcher, error) {
	if e.bus == nil {
		return nil, fmt.Errorf("kv: event bus not available")
	}
	return newWatcher(ctx, e.bus, e.system, prefix)
}

// --- kvapi.Engine writes ---

func (e *CRDTEngine) Set(key string, value []byte) (kvapi.Version, error) {
	en := e.state.Overwrite(key, value, e.wall())
	e.queue.Push(en)
	e.emit(kvapi.WatchPut, &kvapi.Entry{Key: key, Value: value, Version: en.Counter})
	return en.Counter, nil
}

func (e *CRDTEngine) Delete(key string) error {
	en, ok := e.state.Unregister(key, e.wall())
	if !ok {
		return kvapi.ErrKeyNotFound
	}
	e.queue.Push(en)
	e.detachKey(key)
	e.emit(kvapi.WatchDelete, &kvapi.Entry{Key: key})
	return nil
}

func (e *CRDTEngine) SetIfAbsent(key string, value []byte) (kvapi.Version, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.state.LookupEntry(key); ok {
		cur, _ := e.state.LookupEntry(key)
		return cur.Counter, false, nil
	}
	en := e.state.Overwrite(key, value, e.wall())
	e.queue.Push(en)
	e.emit(kvapi.WatchPut, &kvapi.Entry{Key: key, Value: value, Version: en.Counter})
	return en.Counter, true, nil
}

func (e *CRDTEngine) CompareAndSwap(key string, expect kvapi.Version, value []byte) (kvapi.Version, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cur, ok := e.state.LookupEntry(key)
	var curVer kvapi.Version
	if ok {
		curVer = cur.Counter
	}
	if curVer != expect {
		return curVer, false, nil
	}
	en := e.state.Overwrite(key, value, e.wall())
	e.queue.Push(en)
	e.emit(kvapi.WatchPut, &kvapi.Entry{Key: key, Value: value, Version: en.Counter})
	return en.Counter, true, nil
}

// CompareAndDelete is best-effort on the CRDT backend (local compare, not
// linearizable); concurrent writers on other nodes converge by wall-clock LWW.
func (e *CRDTEngine) CompareAndDelete(key string, expect kvapi.Version) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cur, ok := e.state.LookupEntry(key)
	if !ok || cur.Counter != expect {
		return false, nil
	}
	en, removed := e.state.Unregister(key, e.wall())
	if !removed {
		return false, nil
	}
	e.queue.Push(en)
	e.detachKey(key)
	e.emit(kvapi.WatchDelete, &kvapi.Entry{Key: key})
	return true, nil
}

// Txn is unsupported on the CRDT backend: atomic multi-key transactions require
// linearizable consensus. The kv-backed registry runs on the raft backend only.
func (e *CRDTEngine) Txn(_ []kvapi.TxnOp) (bool, error) {
	return false, kvapi.ErrUnsupported
}

// --- leases (local wall-clock; tombstones gossip on expiry) ---

func (e *CRDTEngine) GrantLease(_ context.Context, ttl time.Duration) (kvapi.Lease, error) {
	id := kvapi.LeaseID(fmt.Sprintf("%s-clease-%d", e.state.NodeString(e.state.LocalNode()), e.leaseSeq.Add(1)))
	e.mu.Lock()
	e.leaseKeys[id] = make(map[string]struct{})
	e.deadlines[id] = time.Now().Add(ttl)
	e.mu.Unlock()

	h := newLease(id, ttl)
	h.keepAlive = func(_ context.Context) error {
		e.mu.Lock()
		if _, ok := e.deadlines[id]; !ok {
			e.mu.Unlock()
			return kvapi.ErrLeaseNotFound
		}
		e.deadlines[id] = time.Now().Add(ttl)
		e.mu.Unlock()
		return nil
	}
	h.revoke = func(_ context.Context) error {
		e.expireLease(id)
		return nil
	}
	return h, nil
}

func (e *CRDTEngine) SetWithLease(key string, value []byte, lease kvapi.LeaseID) (kvapi.Version, error) {
	e.mu.Lock()
	keys, ok := e.leaseKeys[lease]
	if !ok {
		e.mu.Unlock()
		return 0, kvapi.ErrLeaseNotFound
	}
	keys[key] = struct{}{}
	e.keyLease[key] = lease
	e.mu.Unlock()

	en := e.state.Overwrite(key, value, e.wall())
	e.queue.Push(en)
	e.emit(kvapi.WatchPut, &kvapi.Entry{Key: key, Value: value, Version: en.Counter})
	return en.Counter, nil
}

func (e *CRDTEngine) SetIfAbsentWithLease(key string, value []byte, lease kvapi.LeaseID) (kvapi.Version, bool, error) {
	e.mu.Lock()
	keys, ok := e.leaseKeys[lease]
	if !ok {
		e.mu.Unlock()
		return 0, false, kvapi.ErrLeaseNotFound
	}
	if _, exists := e.state.LookupEntry(key); exists {
		cur, _ := e.state.LookupEntry(key)
		e.mu.Unlock()
		return cur.Counter, false, nil
	}
	keys[key] = struct{}{}
	e.keyLease[key] = lease
	e.mu.Unlock()

	en := e.state.Overwrite(key, value, e.wall())
	e.queue.Push(en)
	e.emit(kvapi.WatchPut, &kvapi.Entry{Key: key, Value: value, Version: en.Counter})
	return en.Counter, true, nil
}

func (e *CRDTEngine) detachKey(key string) {
	e.mu.Lock()
	if id, ok := e.keyLease[key]; ok {
		if keys, ok := e.leaseKeys[id]; ok {
			delete(keys, key)
		}
		delete(e.keyLease, key)
	}
	e.mu.Unlock()
}

func (e *CRDTEngine) expireLease(id kvapi.LeaseID) {
	e.mu.Lock()
	keys := e.leaseKeys[id]
	delete(e.leaseKeys, id)
	delete(e.deadlines, id)
	var toDelete []string
	for k := range keys {
		toDelete = append(toDelete, k)
		delete(e.keyLease, k)
	}
	e.mu.Unlock()

	for _, k := range toDelete {
		if en, ok := e.state.Unregister(k, e.wall()); ok {
			e.queue.Push(en)
			e.emit(kvapi.WatchExpired, &kvapi.Entry{Key: k})
		}
	}
}

func (e *CRDTEngine) reaper() {
	defer e.wg.Done()
	ticker := time.NewTicker(crdtReapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			e.mu.Lock()
			var expired []kvapi.LeaseID
			for id, dl := range e.deadlines {
				if !dl.After(now) {
					expired = append(expired, id)
				}
			}
			e.mu.Unlock()
			for _, id := range expired {
				e.expireLease(id)
			}
		}
	}
}

// tombstoneReaper periodically drops delete tombstones older than the configured
// wall floor. With the default non-positive floor it is correctness-first and
// does not age out tombstones; operators can configure a positive retention to
// trade max-partition tolerance for bounded delete-churn memory.
func (e *CRDTEngine) tombstoneReaper() {
	defer e.wg.Done()
	ticker := time.NewTicker(e.gcInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			if safe, floor := e.gcTombstones(); safe+floor > 0 {
				e.logger.Debug("crdt: reaped tombstones", zap.Int("safe", safe), zap.Int("wall_floor", floor))
			}
		}
	}
}

// gcTombstones runs one tombstone GC pass. A tombstone is safe to drop only
// after every configured peer has sent a full-state snapshot containing that
// exact delete dot. Otherwise the default correctness-first behavior retains
// tombstones indefinitely. now and the floor are in the engine's scaled wall
// units (real ms * wallScale) to match Entry.Wall.
func (e *CRDTEngine) gcTombstones() (int, int) {
	e.mu.Lock()
	retention := e.tombstoneFloor
	e.mu.Unlock()
	nowScaled := time.Now().UnixMilli() * wallScale
	floorScaled := retention.Milliseconds() * wallScale
	safe, floor, reaped := e.state.ReapTombstonesWhere(e.tombstoneAckedByPeers(), nowScaled, floorScaled)
	e.pruneTombstoneAcks(reaped)
	return safe, floor
}

func (e *CRDTEngine) tombstoneAckedByPeers() func(crdt.Entry) bool {
	e.mu.Lock()
	aliveFn := e.aliveFn
	localNode := e.localNode
	e.mu.Unlock()
	if aliveFn == nil {
		return nil
	}
	alive := aliveFn()
	if len(alive) == 0 {
		return nil
	}

	e.mu.Lock()
	peerAck := make([]map[tombstoneDot]struct{}, 0, len(alive))
	for peer := range alive {
		if peer == localNode {
			continue
		}
		src := e.peerTombAck[peer]
		cp := make(map[tombstoneDot]struct{}, len(src))
		for dot := range src {
			cp[dot] = struct{}{}
		}
		peerAck = append(peerAck, cp)
	}
	e.mu.Unlock()

	return func(ent crdt.Entry) bool {
		dot := tombstoneDot{node: ent.Node, counter: ent.Counter}
		for _, ack := range peerAck {
			if _, ok := ack[dot]; !ok {
				return false
			}
		}
		return true
	}
}

func (e *CRDTEngine) pruneTombstoneAcks(reaped []crdt.Entry) {
	if len(reaped) == 0 {
		return
	}
	dots := make([]tombstoneDot, 0, len(reaped))
	for i := range reaped {
		dots = append(dots, tombstoneDot{node: reaped[i].Node, counter: reaped[i].Counter})
	}
	e.mu.Lock()
	for peer, ack := range e.peerTombAck {
		for _, dot := range dots {
			delete(ack, dot)
		}
		if len(ack) == 0 {
			delete(e.peerTombAck, peer)
		}
	}
	e.mu.Unlock()
}

// --- gossip hooks (driven by the delegate) ---

// DrainBroadcasts returns outgoing delta frames for this gossip cycle.
func (e *CRDTEngine) DrainBroadcasts(overhead, budget int) [][]byte {
	return e.queue.Drain(overhead, budget)
}

// OnFrame applies an inbound delta frame and epidemically re-broadcasts entries
// that advanced local state, so deltas reach the whole cluster.
func (e *CRDTEngine) OnFrame(data []byte) {
	if e.mergeFullState(data) {
		return
	}
	e.onFrame(data, "")
}

func (e *CRDTEngine) onFrame(data []byte, ackPeer string) {
	entries, origins, err := crdt.DecodeFrame(data)
	if err != nil {
		e.logger.Debug("crdt: malformed frame", zap.Error(err))
	}
	for i := range entries {
		en := entries[i]
		en.Node = e.state.InternNode(origins[i])
		if ackPeer != "" && en.Deleted {
			e.recordPeerTombstoneAck(ackPeer, en)
		}
		outcome, merged := e.state.Apply(en)
		if outcome == crdt.MergeApplied || outcome == crdt.MergeDeleteWins {
			e.queue.Push(merged)
			if merged.Deleted {
				e.emit(kvapi.WatchDelete, &kvapi.Entry{Key: merged.Key})
			} else {
				e.emit(kvapi.WatchPut, &kvapi.Entry{Key: merged.Key, Value: merged.Value, Version: merged.Counter})
			}
		}
	}
}

// FullState encodes every entry as a single delta frame for a memberlist
// push/pull bulk sync. The envelope carries the sender identity so receivers
// can treat tombstones present in the snapshot as per-peer delete
// acknowledgements.
func (e *CRDTEngine) FullState() []byte {
	var deltas []byte
	for shard := 0; shard < crdt.ShardCount; shard++ {
		entries := e.state.ShardEntries(shard)
		for i := range entries {
			buf, err := crdt.EncodeDelta(nil, &entries[i], e.state.NodeString(entries[i].Node))
			if err == nil {
				deltas = append(deltas, buf...)
			}
		}
	}
	return e.encodeFullState(deltas)
}

var kvCRDTFullStateMagic = [...]byte{'w', 'k', 'v', 'c', 'r', 'd', 't', 1}

func (e *CRDTEngine) encodeFullState(deltas []byte) []byte {
	node := e.localNode
	if len(node) > 0xFFFF {
		return deltas
	}
	size := len(kvCRDTFullStateMagic) + 2 + len(node) + len(deltas)
	out := make([]byte, 0, size)
	out = append(out, kvCRDTFullStateMagic[:]...)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(node)))
	out = append(out, node...)
	out = append(out, deltas...)
	return out
}

func (e *CRDTEngine) mergeFullState(buf []byte) bool {
	if len(buf) < len(kvCRDTFullStateMagic) || !bytes.Equal(buf[:len(kvCRDTFullStateMagic)], kvCRDTFullStateMagic[:]) {
		return false
	}
	pos := len(kvCRDTFullStateMagic)
	if len(buf)-pos < 2 {
		return true
	}
	nodeLen := int(binary.LittleEndian.Uint16(buf[pos:]))
	pos += 2
	if nodeLen == 0 || len(buf)-pos < nodeLen {
		return true
	}
	peer := string(buf[pos : pos+nodeLen])
	pos += nodeLen
	if pos < len(buf) {
		e.onFrame(buf[pos:], peer)
	}
	return true
}

func (e *CRDTEngine) recordPeerTombstoneAck(peer string, ent crdt.Entry) {
	if peer == "" {
		return
	}
	dot := tombstoneDot{node: ent.Node, counter: ent.Counter}
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.peerTombAck[peer]
	if m == nil {
		m = make(map[tombstoneDot]struct{})
		e.peerTombAck[peer] = m
	}
	m[dot] = struct{}{}
}

func (e *CRDTEngine) emit(typ kvapi.WatchEventType, cur *kvapi.Entry) {
	if e.bus == nil {
		return
	}
	key := ""
	if cur != nil {
		key = cur.Key
	}
	evt := kvapi.WatchEvent{Type: typ}
	if typ != kvapi.WatchDelete && typ != kvapi.WatchExpired {
		evt.Current = cur
	}
	e.bus.Send(context.Background(), event.Event{System: e.system, Kind: key, Data: evt})
}

var _ kvapi.Engine = (*CRDTEngine)(nil)
