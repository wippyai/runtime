// SPDX-License-Identifier: MPL-2.0

// Package globalreg implements the distributed global name registry backed
// by Raft consensus with a sharded state machine.
package globalreg

import (
	"fmt"
	"io"

	"github.com/hashicorp/go-msgpack/v2/codec"
	hraft "github.com/hashicorp/raft"

	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/pid"
)

// FSM implements the hashicorp/raft.FSM interface.
// It is the replicated state machine for the global name registry.
// All Apply calls are serialized by Raft, so no additional locking is
// needed for write operations beyond what the shardedState already provides.
type FSM struct {
	state     *shardedState
	resolve   globalreg.ResolveFunc
	tel       *telemetry
	onRestore func() // optional callback fired after Restore() completes
	pgLabel   string // value for the "pg" telemetry label on fence_token gauges.
}

// NewFSM creates a new FSM with an empty sharded state.
// An optional ResolveFunc can be provided for conflict resolution when a name
// is already registered to a different PID (e.g., after partition heal).
// If nil, DefaultResolve (first-write-wins) is used.
func NewFSM(resolve ...globalreg.ResolveFunc) *FSM {
	f := &FSM{
		state:   newShardedState(),
		resolve: globalreg.DefaultResolve,
		pgLabel: HostID,
	}
	if len(resolve) > 0 && resolve[0] != nil {
		f.resolve = resolve[0]
	}

	return f
}

// SetTelemetry installs the metrics recorder used to emit pg_fence_*/
// pg_globalreg_* series. Safe to call once during boot; the FSM is otherwise
// silent (nil-safe recorders).
func (f *FSM) SetTelemetry(tel *telemetry) {
	f.tel = tel
	// Emit an initial size sample so the gauge is non-empty before the
	// first mutation.
	f.tel.recordGlobalregSize(f.state.Len())
}

// SetOnRestore installs a callback invoked after Raft installs a snapshot.
// The Service uses this to reset the reestablishMonitors watermark, since
// AppliedAt indices in the new snapshot are not comparable to the prior FSM.
func (f *FSM) SetOnRestore(fn func()) { f.onRestore = fn }

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
	existing, outcome := f.state.register(cmd.Name, cmd.PID, nodeID, index)
	switch outcome {
	case registerInserted:
		f.tel.recordFenceToken(f.pgLabel, nodeID, index)
		f.tel.recordGlobalregSize(f.state.Len())
		return &RegisterResult{PID: existing}
	case registerDedupe:
		f.tel.recordGlobalregDedupe()
		return &RegisterResult{PID: existing}
	case registerConflict:
		// Name taken by a different PID. Invoke conflict resolution.
		winner := f.resolve(cmd.Name, existing, cmd.PID)
		if winner == cmd.PID {
			// Resolve chose the incoming PID — force re-register.
			f.state.unregister(cmd.Name)
			f.state.register(cmd.Name, cmd.PID, nodeID, index)
			f.tel.recordFenceToken(f.pgLabel, nodeID, index)
			f.tel.recordGlobalregSize(f.state.Len())
			return &RegisterResult{
				PID:         cmd.PID,
				ResolvedPID: existing, // the loser
			}
		}
		// Resolve kept the existing owner.
		return &RegisterResult{
			ExistingPID: existing,
			Err:         fmt.Errorf("global name %q already registered to %s", cmd.Name, existing.String()),
		}
	default:
		return &RegisterResult{
			ExistingPID: existing,
			Err:         fmt.Errorf("globalreg: unknown register outcome %d", outcome),
		}
	}
}

func (f *FSM) applyUnregister(cmd *Command) any {
	removed := f.state.unregister(cmd.Name)
	if removed {
		f.tel.recordGlobalregSize(f.state.Len())
	}

	return &UnregisterResult{Removed: removed}
}

func (f *FSM) applyRemovePID(cmd *Command) any {
	count := f.state.removePID(cmd.PID)
	if count > 0 {
		f.tel.recordGlobalregSize(f.state.Len())
	}

	return &RemoveResult{Count: count}
}

func (f *FSM) applyRemoveNode(cmd *Command) any {
	count, hasMore := f.state.removeNode(cmd.NodeID, cmd.Limit)
	if count > 0 {
		f.tel.recordGlobalregSize(f.state.Len())
	}

	return &RemoveResult{Count: count, HasMore: hasMore}
}

// RegisterResult is returned by Apply for CmdRegister.
type RegisterResult struct {
	Err         error   // Non-nil if registration failed.
	PID         pid.PID // The PID that owns the name.
	ExistingPID pid.PID // Set only when registration fails (name taken).
	ResolvedPID pid.PID // Set when conflict resolution replaced the previous owner.
}

// UnregisterResult is returned by Apply for CmdUnregister.
type UnregisterResult struct {
	Removed bool
}

// RemoveResult is returned by Apply for CmdRemovePID and CmdRemoveNode.
// HasMore is set by chunked CmdRemoveNode (Limit > 0) when entries remain;
// the Service loops until HasMore=false.
type RemoveResult struct {
	Count   int  // Number of names removed.
	HasMore bool // true if a chunked CmdRemoveNode left entries unprocessed.
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
	dec := codec.NewDecoder(rc, newMsgpackHandle())
	if err := dec.Decode(&entries); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}

	f.state.restore(entries)
	f.tel.recordGlobalregSize(f.state.Len())
	if f.onRestore != nil {
		f.onRestore()
	}
	return nil
}

// fsmSnapshot implements hraft.FSMSnapshot.
type fsmSnapshot struct {
	entries []snapshotEntry
}

// Persist writes the snapshot data to the sink.
func (s *fsmSnapshot) Persist(sink hraft.SnapshotSink) error {
	enc := codec.NewEncoder(sink, newMsgpackHandle())
	if err := enc.Encode(s.entries); err != nil {
		_ = sink.Cancel()
		return fmt.Errorf("encode snapshot: %w", err)
	}
	return sink.Close()
}

// Release is called after Persist completes. No-op.
func (s *fsmSnapshot) Release() {}
