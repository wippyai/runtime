// SPDX-License-Identifier: MPL-2.0

package kvraft

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/hashicorp/go-msgpack/v2/codec"
	hraft "github.com/hashicorp/raft"
	"github.com/wippyai/runtime/api/kv"
)

// FSM implements hraft.FSM for the kvraft replication group. All write
// operations land here via Raft-replicated Apply calls, ensuring identical
// state across followers.
type FSM struct {
	state *state

	// applyCallback fires on every successful FSM mutation so the Service
	// can publish Watch events. Receives (op, space, key, value, version).
	applyCallback atomic.Pointer[applyCallbackFn]
}

type applyCallbackFn func(op kv.Op, space, key string, value []byte, version uint64)

// NewFSM constructs an empty FSM.
func NewFSM() *FSM {
	return &FSM{state: newState()}
}

// SetApplyCallback installs the watch hook. Safe to call from any goroutine.
func (f *FSM) SetApplyCallback(fn applyCallbackFn) {
	f.applyCallback.Store(&fn)
}

// State exposes the underlying read API for direct local reads.
// Lookups are lock-free per shard.
func (f *FSM) State() *state { return f.state }

// Apply executes a Command. Called by Raft on every node, single-threaded
// per FSM. Returns a *Result struct that propagates back to the leader's
// Apply caller.
func (f *FSM) Apply(log *hraft.Log) any {
	cmd, err := DecodeCommand(log.Data)
	if err != nil {
		return &Result{Err: fmt.Errorf("kvraft: decode command: %w", err)}
	}
	switch cmd.Type {
	case CmdPut:
		return f.applyPut(cmd, log.Index)
	case CmdDelete:
		return f.applyDelete(cmd, log.Index)
	case CmdCAS:
		return f.applyCAS(cmd, log.Index)
	case CmdReapTTL:
		return f.applyReapTTL(cmd, log.Index)
	default:
		return &Result{Err: fmt.Errorf("kvraft: unknown command type: %d", cmd.Type)}
	}
}

func (f *FSM) applyPut(cmd *Command, index uint64) *Result {
	sk := shardKey{Space: cmd.Space, Key: cmd.Key}
	now := nowMs()

	if cmd.ExpectAbsent {
		if f.state.has(sk, now) {
			return &Result{Err: kv.ErrKeyExists}
		}
	}
	if cmd.ExpectVersion != 0 {
		_, version, _, ok := f.state.get(sk, now)
		if !ok || version != cmd.ExpectVersion {
			return &Result{Err: kv.ErrCASMismatch}
		}
	}
	f.state.put(sk, cmd.Value, cmd.TTL, index)
	f.fireCallback(kv.OpPut, cmd.Space, cmd.Key, cmd.Value, index)
	return &Result{Version: index}
}

func (f *FSM) applyDelete(cmd *Command, index uint64) *Result {
	sk := shardKey{Space: cmd.Space, Key: cmd.Key}
	if cmd.ExpectVersion != 0 {
		_, version, _, ok := f.state.get(sk, nowMs())
		if !ok || version != cmd.ExpectVersion {
			return &Result{Err: kv.ErrCASMismatch}
		}
	}
	if f.state.delete(sk) {
		f.fireCallback(kv.OpDelete, cmd.Space, cmd.Key, nil, index)
	}
	return &Result{Version: index}
}

func (f *FSM) applyCAS(cmd *Command, index uint64) *Result {
	sk := shardKey{Space: cmd.Space, Key: cmd.Key}
	now := nowMs()
	cur, _, _, ok := f.state.get(sk, now)
	if !ok {
		return &Result{Err: kv.ErrKeyNotFound}
	}
	if !bytes.Equal(cur, cmd.ExpectValue) {
		return &Result{Err: kv.ErrCASMismatch}
	}
	f.state.put(sk, cmd.Value, cmd.TTL, index)
	f.fireCallback(kv.OpPut, cmd.Space, cmd.Key, cmd.Value, index)
	return &Result{Version: index}
}

func (f *FSM) applyReapTTL(_ *Command, _ uint64) *Result {
	removed := f.state.reapExpired(nowMs())
	return &Result{Removed: removed}
}

func (f *FSM) fireCallback(op kv.Op, space, key string, value []byte, version uint64) {
	if cb := f.applyCallback.Load(); cb != nil {
		(*cb)(op, space, key, value, version)
	}
}

// --- Snapshot / Restore ---

// Snapshot returns a point-in-time snapshot of the FSM state.
func (f *FSM) Snapshot() (hraft.FSMSnapshot, error) {
	entries := f.state.snapshot()
	return &fsmSnapshot{entries: entries}, nil
}

// Restore replaces the FSM state from a snapshot.
func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	var entries []snapshotEntry
	dec := codec.NewDecoder(rc, newMsgpackHandle())
	if err := dec.Decode(&entries); err != nil {
		return fmt.Errorf("kvraft: decode snapshot: %w", err)
	}
	f.state.restore(entries)
	return nil
}

// fsmSnapshot is hraft.FSMSnapshot.
type fsmSnapshot struct {
	entries []snapshotEntry
}

func (s *fsmSnapshot) Persist(sink hraft.SnapshotSink) error {
	enc := codec.NewEncoder(sink, newMsgpackHandle())
	if err := enc.Encode(s.entries); err != nil {
		_ = sink.Cancel()
		return fmt.Errorf("kvraft: encode snapshot: %w", err)
	}
	return sink.Close()
}

func (s *fsmSnapshot) Release() {}

// nowMs is a function for testability — tests override it for deterministic
// TTL evaluation.
var nowMs = ourTimeNowUnixMilli

// ourTimeNowUnixMilli is split out so tests can override `nowMs` without
// pulling time into a dependency-injected interface across the package.
func ourTimeNowUnixMilli() int64 {
	return ourTimeNow().UnixMilli()
}

var _ = errors.New
