// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	hraft "github.com/hashicorp/raft"

	"github.com/wippyai/runtime/api/event"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

func msToDuration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }

// RaftFSM is the kv state machine replicated by the shared cluster raft. It is
// node-wide: every store.kv.raft entry is a key namespace into this one FSM.
// raft serializes Apply/Snapshot/Restore, so the mutable state needs no lock;
// concurrent reads are served from an atomically-published snapshot.
type RaftFSM struct {
	bus    event.Bus
	state  *state
	leases *leaseManager
	snap   atomic.Pointer[stateSnapshot]
	system event.System
	outbox []event.Event
	mu     sync.Mutex
}

// NewRaftFSM builds the kv FSM. bus may be nil (watch disabled).
func NewRaftFSM(bus event.Bus) *RaftFSM {
	f := &RaftFSM{
		state:  newState(),
		leases: newLeaseManager(),
		bus:    bus,
		system: "kv:raft",
	}
	f.snap.Store(f.state.snapshot())
	return f
}

// EventSystem returns the event.System watch events are published on, so the
// engine can build watchers bound to the same bus stream.
func (f *RaftFSM) EventSystem() event.System { return f.system }

// Apply executes one committed command. The multiplex router has already
// stripped the kv domain byte, so log.Data is a bare command.
func (f *RaftFSM) Apply(log *hraft.Log) any {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.state.applyIndex = log.Index

	if len(log.Data) > 0 && opcode(log.Data[0]) == opTxn {
		ops, err := decodeTxn(log.Data)
		if err != nil {
			return applyResult{Err: err}
		}
		res := f.applyTxn(ops)
		f.snap.Store(f.state.snapshot())
		f.flush()
		return res
	}

	c, err := decodeCommand(log.Data)
	if err != nil {
		return applyResult{Err: err}
	}

	res := f.applyCommand(c)
	f.snap.Store(f.state.snapshot())
	f.flush()
	return res
}

// flush delivers buffered watch events after the new snapshot is published, so a
// watcher that reacts by reading the kv observes the change that triggered it.
func (f *RaftFSM) flush() {
	if len(f.outbox) == 0 {
		return
	}
	for i := range f.outbox {
		f.bus.Send(context.Background(), f.outbox[i])
	}
	f.outbox = f.outbox[:0]
}

// applyTxn evaluates every precondition against current state, then applies all
// puts/deletes iff all hold (all-or-nothing under the FSM mutex).
func (f *RaftFSM) applyTxn(ops []kvapi.TxnOp) applyResult {
	for _, op := range ops {
		if !condHolds(op.Cond, op.Expect, f.state.get(op.Key)) {
			return applyResult{OK: false}
		}
	}
	for _, op := range ops {
		switch op.Kind {
		case kvapi.TxnPut:
			prev := f.state.get(op.Key)
			f.state.set(op.Key, op.Value, "")
			f.emitPut(op.Key, prev)
		case kvapi.TxnDelete:
			if prev := f.state.del(op.Key); prev != nil {
				f.emitEvent(kvapi.WatchDelete, nil, prev)
			}
		case kvapi.TxnCheck:
		}
	}
	return applyResult{OK: true}
}

func (f *RaftFSM) applyCommand(c command) applyResult {
	switch c.Op {
	case opSet:
		prev, ver := f.state.set(c.Key, c.Value, "")
		f.emitPut(c.Key, prev)
		return applyResult{Version: ver, OK: true}
	case opDelete:
		prev := f.state.del(c.Key)
		if prev == nil {
			return applyResult{Err: kvapi.ErrKeyNotFound}
		}
		f.emitEvent(kvapi.WatchDelete, nil, prev)
		return applyResult{OK: true}
	case opCAS:
		ver, ok := f.state.cas(c.Key, c.Expect, c.Value)
		if ok {
			f.emitPut(c.Key, nil)
		}
		return applyResult{Version: ver, OK: ok}
	case opSetIfAbsent:
		ver, ok := f.state.setIfAbsent(c.Key, c.Value, "")
		if ok {
			f.emitPut(c.Key, nil)
		}
		return applyResult{Version: ver, OK: ok}
	case opCompareAndDelete:
		prev := f.state.get(c.Key)
		deleted, _ := f.state.compareAndDelete(c.Key, c.Expect)
		if deleted {
			f.emitEvent(kvapi.WatchDelete, nil, prev)
		}
		return applyResult{OK: deleted}
	case opSetWithLease:
		if _, ok := f.leases.getHandle(c.LeaseID); !ok {
			return applyResult{Err: kvapi.ErrLeaseNotFound}
		}
		prev, ver := f.state.set(c.Key, c.Value, c.LeaseID)
		f.emitPut(c.Key, prev)
		return applyResult{Version: ver, OK: true}
	case opSetIfAbsentWithLease:
		if _, ok := f.leases.getHandle(c.LeaseID); !ok {
			return applyResult{Err: kvapi.ErrLeaseNotFound}
		}
		ver, ok := f.state.setIfAbsent(c.Key, c.Value, c.LeaseID)
		if ok {
			f.emitPut(c.Key, nil)
		}
		return applyResult{Version: ver, OK: ok}
	case opLeaseGrant:
		f.state.addLease(c.LeaseID, c.TTLms)
		f.leases.grant(c.LeaseID, msToDuration(c.TTLms), time.Now())
		return applyResult{OK: true}
	case opLeaseRenew:
		if !f.leases.renew(c.LeaseID, time.Now()) {
			return applyResult{Err: kvapi.ErrLeaseNotFound}
		}
		return applyResult{OK: true}
	case opLeaseRevoke:
		keys := f.state.removeLease(c.LeaseID)
		for _, key := range keys {
			if prev := f.state.del(key); prev != nil {
				f.emitEvent(kvapi.WatchExpired, nil, prev)
			}
		}
		f.leases.revoke(c.LeaseID)
		return applyResult{OK: true}
	default:
		return applyResult{Err: fmt.Errorf("kv fsm: unknown op %d", c.Op)}
	}
}

// get reads an entry from the published snapshot (lock-free).
func (f *RaftFSM) get(key string) (kvapi.Entry, bool) {
	snap := f.snap.Load()
	e := snap.get(key)
	if e == nil {
		return kvapi.Entry{}, false
	}
	return *e, true
}

// scan iterates the published snapshot under a prefix.
func (f *RaftFSM) scan(prefix string, fn func(kvapi.Entry) bool) {
	f.snap.Load().scan(prefix, fn)
}

// leaseSnapshot returns the (id, ttlMs) of every live lease, used by a node
// that just became leader to re-arm expiry timers.
func (f *RaftFSM) leaseSnapshot() map[kvapi.LeaseID]int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[kvapi.LeaseID]int64, len(f.state.leases))
	for id, ls := range f.state.leases {
		out[id] = ls.ttl
	}
	return out
}

// --- snapshot / restore ---

type snapEntry struct {
	Key     string
	LeaseID string
	Value   []byte
	Version uint64
	Epoch   uint64
}

type snapLease struct {
	ID    string
	Keys  []string
	TTLms int64
}

type fsmState struct {
	Entries []snapEntry
	Leases  []snapLease
	Version uint64
}

// Snapshot captures a consistent copy of the kv state. raft serializes this
// with Apply, so reading the mutable state directly is safe.
func (f *RaftFSM) Snapshot() (hraft.FSMSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	st := fsmState{Version: f.state.version}
	for _, e := range f.state.entries {
		st.Entries = append(st.Entries, snapEntry{
			Key: e.key, Value: e.value, Version: e.version, LeaseID: string(e.leaseID), Epoch: e.epoch,
		})
	}
	for id, ls := range f.state.leases {
		keys := make([]string, 0, len(ls.keys))
		for k := range ls.keys {
			keys = append(keys, k)
		}
		st.Leases = append(st.Leases, snapLease{ID: string(id), TTLms: ls.ttl, Keys: keys})
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(st); err != nil {
		return nil, fmt.Errorf("kv fsm snapshot: %w", err)
	}
	return &raftSnapshot{data: buf.Bytes()}, nil
}

// Restore rebuilds the state from a snapshot stream.
func (f *RaftFSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	var st fsmState
	if err := gob.NewDecoder(rc).Decode(&st); err != nil {
		return fmt.Errorf("kv fsm restore: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	fresh := newState()
	fresh.version = st.Version
	freshLeases := newLeaseManager()
	for _, l := range st.Leases {
		fresh.addLease(kvapi.LeaseID(l.ID), l.TTLms)
		freshLeases.grant(kvapi.LeaseID(l.ID), msToDuration(l.TTLms), time.Now())
	}
	for _, e := range st.Entries {
		fresh.entries[e.Key] = &entry{
			key: e.Key, value: e.Value, version: e.Version, leaseID: kvapi.LeaseID(e.LeaseID), epoch: e.Epoch,
		}
		if e.LeaseID != "" {
			if ls, ok := fresh.leases[kvapi.LeaseID(e.LeaseID)]; ok {
				ls.keys[e.Key] = struct{}{}
			}
		}
	}
	f.state = fresh
	f.leases = freshLeases
	f.snap.Store(fresh.snapshot())
	return nil
}

// emitPut publishes a WatchPut for a key just written.
func (f *RaftFSM) emitPut(key string, prev *entry) {
	current := f.state.get(key)
	if current == nil {
		return
	}
	f.emitEvent(kvapi.WatchPut, entryToAPI(current), prev)
}

// emitEvent publishes a watch event on the FSM's event system.
func (f *RaftFSM) emitEvent(typ kvapi.WatchEventType, current *kvapi.Entry, prev *entry) {
	if f.bus == nil {
		return
	}
	evt := kvapi.WatchEvent{Type: typ, Current: current, Index: f.state.applyIndex}
	key := ""
	if current != nil {
		key = current.Key
	}
	if prev != nil {
		evt.Previous = entryToAPI(prev)
		if key == "" {
			key = prev.key
		}
	}
	f.outbox = append(f.outbox, event.Event{System: f.system, Kind: key, Data: evt})
}

func entryToAPI(e *entry) *kvapi.Entry {
	if e == nil {
		return nil
	}
	return &kvapi.Entry{Key: e.key, Value: e.value, Version: e.version, LeaseID: e.leaseID, Epoch: e.epoch}
}

// raftSnapshot is the persisted form of a kv FSM snapshot.
type raftSnapshot struct {
	data []byte
}

func (s *raftSnapshot) Persist(sink hraft.SnapshotSink) error {
	if _, err := sink.Write(s.data); err != nil {
		_ = sink.Cancel()
		return err
	}
	return sink.Close()
}

func (s *raftSnapshot) Release() {}

var (
	_ hraft.FSM         = (*RaftFSM)(nil)
	_ hraft.FSMSnapshot = (*raftSnapshot)(nil)
)
