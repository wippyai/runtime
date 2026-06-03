// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
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
	bus          event.Bus
	ctx          context.Context
	durableNS    map[string]struct{}
	queue        *crdt.BroadcastQueue
	logger       *zap.Logger
	state        *crdt.State
	cancel       context.CancelFunc
	leaseKeys    map[kvapi.LeaseID]map[string]struct{}
	deadlines    map[kvapi.LeaseID]time.Time
	keyLease     map[string]kvapi.LeaseID
	system       event.System
	dataDir      string
	wg           sync.WaitGroup
	snapInterval time.Duration
	leaseSeq     atomic.Uint64
	tiebreak     int64
	mu           sync.Mutex
}

// NewCRDTEngine builds the node-wide crdt engine.
func NewCRDTEngine(localNode string, bus event.Bus, logger *zap.Logger) *CRDTEngine {
	if logger == nil {
		logger = zap.NewNop()
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(localNode))
	return &CRDTEngine{
		state:        crdt.NewState(localNode),
		queue:        crdt.NewBroadcastQueue(localNode, 0),
		bus:          bus,
		logger:       logger.Named("kv-crdt"),
		system:       "kv:crdt",
		tiebreak:     int64(h.Sum32() % wallScale),
		durableNS:    make(map[string]struct{}),
		snapInterval: 30 * time.Second,
		leaseKeys:    make(map[kvapi.LeaseID]map[string]struct{}),
		deadlines:    make(map[kvapi.LeaseID]time.Time),
		keyLease:     make(map[string]kvapi.LeaseID),
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

// --- gossip hooks (driven by the delegate) ---

// DrainBroadcasts returns outgoing delta frames for this gossip cycle.
func (e *CRDTEngine) DrainBroadcasts(overhead, budget int) [][]byte {
	return e.queue.Drain(overhead, budget)
}

// OnFrame applies an inbound delta frame and epidemically re-broadcasts entries
// that advanced local state, so deltas reach the whole cluster.
func (e *CRDTEngine) OnFrame(data []byte) {
	entries, origins, err := crdt.DecodeFrame(data)
	if err != nil {
		e.logger.Debug("crdt: malformed frame", zap.Error(err))
	}
	for i := range entries {
		en := entries[i]
		en.Node = e.state.InternNode(origins[i])
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
// push/pull bulk sync. The receiver feeds it back through OnFrame.
func (e *CRDTEngine) FullState() []byte {
	var out []byte
	for shard := 0; shard < crdt.ShardCount; shard++ {
		entries := e.state.ShardEntries(shard)
		for i := range entries {
			buf, err := crdt.EncodeDelta(nil, &entries[i], e.state.NodeString(entries[i].Node))
			if err == nil {
				out = append(out, buf...)
			}
		}
	}
	return out
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
