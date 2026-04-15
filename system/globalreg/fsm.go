// SPDX-License-Identifier: MPL-2.0

// Package globalreg implements the distributed global name registry backed
// by Raft consensus with a sharded state machine.
package globalreg

import (
	"fmt"
	"io"

	"github.com/hashicorp/go-msgpack/v2/codec"
	hraft "github.com/hashicorp/raft"

	"github.com/wippyai/runtime/api/pid"
)

// FSM implements the hashicorp/raft.FSM interface.
// It is the replicated state machine for the global name registry.
// All Apply calls are serialized by Raft, so no additional locking is
// needed for write operations beyond what the shardedState already provides.
type FSM struct {
	state *shardedState
}

// NewFSM creates a new FSM with an empty sharded state.
func NewFSM() *FSM {
	return &FSM{state: newShardedState()}
}

// State returns the underlying sharded state for direct read access.
func (f *FSM) State() *shardedState {
	return f.state
}

// Apply is called by Raft once a log entry has been committed by a quorum.
// The returned value is available via the ApplyFuture.Response().
// For CmdRegister, it returns either nil (success) or an error (name taken).
func (f *FSM) Apply(log *hraft.Log) any {
	cmd, err := DecodeCommand(log.Data)
	if err != nil {
		return fmt.Errorf("decode global registry command: %w", err)
	}

	switch cmd.Type {
	case CmdRegister:
		return f.applyRegister(cmd, log.Index)
	case CmdUnregister:
		return f.applyUnregister(cmd)
	case CmdRemovePID:
		return f.applyRemovePID(cmd)
	case CmdRemoveNode:
		return f.applyRemoveNode(cmd)
	default:
		return fmt.Errorf("unknown command type: %d", cmd.Type)
	}
}

func (f *FSM) applyRegister(cmd *Command, index uint64) any {
	nodeID := cmd.NodeID
	if nodeID == "" {
		nodeID = cmd.PID.Node
	}
	existing, ok := f.state.register(cmd.Name, cmd.PID, nodeID, index)
	if !ok {
		// Name taken by a different PID.
		return &RegisterResult{
			ExistingPID: existing,
			Err:         fmt.Errorf("global name %q already registered to %s", cmd.Name, existing.String()),
		}
	}
	return &RegisterResult{PID: existing}
}

func (f *FSM) applyUnregister(cmd *Command) any {
	removed := f.state.unregister(cmd.Name)
	return &UnregisterResult{Removed: removed}
}

func (f *FSM) applyRemovePID(cmd *Command) any {
	count := f.state.removePID(cmd.PID)
	return &RemoveResult{Count: count}
}

func (f *FSM) applyRemoveNode(cmd *Command) any {
	count := f.state.removeNode(cmd.NodeID)
	return &RemoveResult{Count: count}
}

// RegisterResult is returned by Apply for CmdRegister.
type RegisterResult struct {
	Err         error   // Non-nil if registration failed.
	PID         pid.PID // The PID that owns the name.
	ExistingPID pid.PID // Set only when registration fails (name taken).
}

// UnregisterResult is returned by Apply for CmdUnregister.
type UnregisterResult struct {
	Removed bool
}

// RemoveResult is returned by Apply for CmdRemovePID and CmdRemoveNode.
type RemoveResult struct {
	Count int // Number of names removed.
}

// --- Snapshot / Restore ---

// Snapshot returns a point-in-time snapshot of the FSM state.
// Called by Raft periodically for log compaction.
func (f *FSM) Snapshot() (hraft.FSMSnapshot, error) {
	entries := f.state.snapshot()
	return &fsmSnapshot{entries: entries}, nil
}

// Restore replaces the entire FSM state from a snapshot.
// Called on a follower or recovering node.
func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	var entries []snapshotEntry
	dec := codec.NewDecoder(rc, handle)
	if err := dec.Decode(&entries); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}

	f.state.restore(entries)
	return nil
}

// fsmSnapshot implements hraft.FSMSnapshot.
type fsmSnapshot struct {
	entries []snapshotEntry
}

// Persist writes the snapshot data to the sink.
func (s *fsmSnapshot) Persist(sink hraft.SnapshotSink) error {
	enc := codec.NewEncoder(sink, handle)
	if err := enc.Encode(s.entries); err != nil {
		_ = sink.Cancel()
		return fmt.Errorf("encode snapshot: %w", err)
	}
	return sink.Close()
}

// Release is called after Persist completes. No-op.
func (s *fsmSnapshot) Release() {}
