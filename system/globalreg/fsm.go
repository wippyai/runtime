// SPDX-License-Identifier: MPL-2.0

// Package globalreg implements the distributed global name registry backed
// by Raft consensus with a sharded state machine.
package globalreg

import (
	"fmt"
	"io"
	"time"

	"github.com/hashicorp/go-msgpack/v2/codec"
	hraft "github.com/hashicorp/raft"

	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/pid"
)

// PendingEvent is delivered to FSM.onPending after applyRegisterPending
// commits a Root reservation. Each replica receives the event independently
// during Apply so the Service can decide whether the local node is in the
// required set and emit an ack to the leader.
type PendingEvent struct {
	PID              pid.PID
	Name             string
	RequiredNodes    []pid.NodeID
	Epoch            uint64
	DeadlineUnixNano int64
}

// ActiveEvent is delivered when applyRegisterAck completes the ack set and
// promotes a pending reservation to active. Carries the missing-set as
// empty (full) for consistency with ExpiredEvent.
type ActiveEvent struct {
	ActivationTime time.Time
	PID            pid.PID
	Name           string
	Epoch          uint64
	ActivationIdx  uint64
}

// ExpiredEvent is delivered when a pending reservation is released without
// reaching active state — either because the deadline elapsed or because
// the leader / caller dropped the reservation explicitly.
type ExpiredEvent struct {
	ExpiredAt   time.Time
	PID         pid.PID
	Name        string
	Reason      string
	MissingAcks []pid.NodeID
	Epoch       uint64
}

// FSM implements the hashicorp/raft.FSM interface.
// It is the replicated state machine for the global name registry.
// All Apply calls are serialized by Raft, so no additional locking is
// needed for write operations beyond what the shardedState already provides.
type FSM struct {
	state     *shardedState
	resolve   globalreg.ResolveFunc
	tel       *telemetry
	onRestore func()
	onPending func(PendingEvent)
	onActive  func(ActiveEvent)
	onExpired func(ExpiredEvent)
	pgLabel   string
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
	f.tel.recordGlobalregSize(f.state.Len())
}

// SetOnRestore installs a callback invoked after Raft installs a snapshot.
// The Service uses this to reset the reestablishMonitors watermark, since
// AppliedAt indices in the new snapshot are not comparable to the prior FSM.
func (f *FSM) SetOnRestore(fn func()) { f.onRestore = fn }

// SetOnPending wires the Service's pending-event hook. Invoked on every
// replica inside FSM.Apply for CmdRegisterPending — the replica decides if
// it should emit an ack to the leader.
func (f *FSM) SetOnPending(fn func(PendingEvent)) { f.onPending = fn }

// SetOnActive wires the Service's active-event hook. Invoked on every
// replica inside FSM.Apply for the CmdRegisterAck that completes the set.
func (f *FSM) SetOnActive(fn func(ActiveEvent)) { f.onActive = fn }

// SetOnExpired wires the Service's expired-event hook. Invoked on every
// replica inside FSM.Apply for CmdRegisterExpired.
func (f *FSM) SetOnExpired(fn func(ExpiredEvent)) { f.onExpired = fn }

// State returns the underlying sharded state for direct read access.
func (f *FSM) State() *shardedState {
	return f.state
}

// Apply is called by Raft once a log entry has been committed by a quorum.
// The returned value is available via the ApplyFuture.Response().
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
	case CmdRegisterPending:
		return f.applyRegisterPending(cmd, log.Index)
	case CmdRegisterAck:
		return f.applyRegisterAck(cmd, log.Index)
	case CmdRegisterExpired:
		return f.applyRegisterExpired(cmd, log.Index)
	case CmdRegisterUnreserve:
		return f.applyRegisterUnreserve(cmd)
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
		return &RegisterResult{PID: existing, FenceToken: index}
	case registerDedupe:
		f.tel.recordGlobalregDedupe()
		return &RegisterResult{PID: existing, FenceToken: index}
	case registerConflict:
		winner := f.resolve(cmd.Name, existing, cmd.PID)
		if winner == cmd.PID {
			f.state.unregister(cmd.Name)
			f.state.register(cmd.Name, cmd.PID, nodeID, index)
			f.tel.recordFenceToken(f.pgLabel, nodeID, index)
			f.tel.recordGlobalregSize(f.state.Len())
			return &RegisterResult{
				PID:         cmd.PID,
				ResolvedPID: existing,
				FenceToken:  index,
			}
		}
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
	pendingNames := f.state.lookupPendingByPID(cmd.PID)
	for _, n := range pendingNames {
		if e, ok := f.state.unreservePending(n, cmd.PID); ok && e != nil {
			f.tel.recordRootRelease("pid_exit")
		}
	}
	count := f.state.removePID(cmd.PID)
	if count > 0 || len(pendingNames) > 0 {
		f.tel.recordGlobalregSize(f.state.Len())
	}

	return &RemoveResult{Count: count + len(pendingNames)}
}

func (f *FSM) applyRemoveNode(cmd *Command) any {
	count, hasMore := f.state.removeNode(cmd.NodeID, cmd.Limit)
	if count > 0 {
		f.tel.recordGlobalregSize(f.state.Len())
	}

	return &RemoveResult{Count: count, HasMore: hasMore}
}

func (f *FSM) applyRegisterPending(cmd *Command, index uint64) any {
	nodeID := cmd.NodeID
	if nodeID == "" {
		nodeID = cmd.PID.Node
	}
	epoch := cmd.Epoch
	if epoch == 0 {
		epoch = index
	}
	createdAt := index
	existing, outcome := f.state.registerPending(cmd.Name, cmd.PID, nodeID,
		epoch, cmd.RequiredNodes, cmd.DeadlineUnixNano, int64(createdAt))
	switch outcome {
	case pendingInserted:
		f.tel.recordRootPending("inserted")
		f.tel.setRootPendingInFlight(f.state.PendingLen())
		if f.onPending != nil {
			req := make([]pid.NodeID, len(cmd.RequiredNodes))
			copy(req, cmd.RequiredNodes)
			f.onPending(PendingEvent{
				Name:             cmd.Name,
				PID:              cmd.PID,
				Epoch:            epoch,
				RequiredNodes:    req,
				DeadlineUnixNano: cmd.DeadlineUnixNano,
			})
		}
		return &RegisterResult{
			PID:        cmd.PID,
			FenceToken: epoch,
			State:      globalreg.RegisterStateUnknown,
		}
	case pendingDedupe:
		f.tel.recordRootPending("dedupe")
		return &RegisterResult{PID: existing, FenceToken: epoch}
	case pendingConflictActive:
		f.tel.recordRootPending("conflict_active")
		return &RegisterResult{
			ExistingPID: existing,
			Err:         globalreg.ErrNameAlreadyRegistered,
		}
	case pendingConflictPending:
		f.tel.recordRootPending("conflict_pending")
		return &RegisterResult{
			ExistingPID: existing,
			Err:         globalreg.ErrPendingConflict,
		}
	default:
		return &RegisterResult{
			Err: fmt.Errorf("globalreg: unknown pending outcome %d", outcome),
		}
	}
}

func (f *FSM) applyRegisterAck(cmd *Command, index uint64) any {
	e, fresh, complete := f.state.recordAck(cmd.Name, cmd.Epoch, cmd.AckerNode)
	if e == nil {
		return &AckResult{Recognized: false}
	}
	if fresh {
		f.tel.recordRootAck("fresh")
	} else {
		f.tel.recordRootAck("duplicate")
	}
	if !complete {
		return &AckResult{Recognized: true, Complete: false}
	}
	promoted, ok := f.state.promotePending(cmd.Name, cmd.Epoch, index)
	if !ok || promoted == nil {
		return &AckResult{Recognized: true, Complete: true}
	}
	f.tel.recordFenceToken(f.pgLabel, promoted.NodeID, index)
	f.tel.recordRootActive(ackBucket(len(promoted.AckSet)))
	f.tel.setRootPendingInFlight(f.state.PendingLen())
	f.tel.recordGlobalregSize(f.state.Len())
	if f.onActive != nil {
		f.onActive(ActiveEvent{
			Name:           cmd.Name,
			PID:            promoted.PID,
			Epoch:          cmd.Epoch,
			ActivationIdx:  index,
			ActivationTime: time.Now(),
		})
	}
	return &AckResult{Recognized: true, Complete: true, Activated: true}
}

func (f *FSM) applyRegisterExpired(cmd *Command, index uint64) any {
	expiredAt := time.Now().UnixNano()
	e, missing, ok := f.state.expirePending(cmd.Name, cmd.Epoch, cmd.Reason, expiredAt)
	if !ok || e == nil {
		return &ExpireResult{Removed: false}
	}
	f.tel.recordRootExpired(cmd.Reason)
	f.tel.setRootPendingInFlight(f.state.PendingLen())
	if f.onExpired != nil {
		f.onExpired(ExpiredEvent{
			Name:        cmd.Name,
			PID:         e.PID,
			Epoch:       cmd.Epoch,
			Reason:      cmd.Reason,
			MissingAcks: missing,
			ExpiredAt:   time.Unix(0, expiredAt),
		})
	}
	_ = index
	return &ExpireResult{Removed: true, MissingAcks: missing}
}

func (f *FSM) applyRegisterUnreserve(cmd *Command) any {
	e, ok := f.state.unreservePending(cmd.Name, cmd.PID)
	if !ok {
		removed := f.state.unregister(cmd.Name)
		if removed {
			f.tel.recordGlobalregSize(f.state.Len())
		}
		return &UnregisterResult{Removed: removed}
	}
	f.tel.recordRootRelease("unreserve")
	f.tel.setRootPendingInFlight(f.state.PendingLen())
	if f.onExpired != nil {
		f.onExpired(ExpiredEvent{
			Name:      cmd.Name,
			PID:       e.PID,
			Epoch:     e.Epoch,
			Reason:    "unreserve",
			ExpiredAt: time.Now(),
		})
	}
	return &UnregisterResult{Removed: true}
}

// PendingLen returns the count of in-flight pending reservations.
func (s *shardedState) PendingLen() int {
	s.pendingMu.RLock()
	defer s.pendingMu.RUnlock()
	return len(s.pending)
}

// ackBucket converts an ack-set size into a low-cardinality label bucket
// used by globalreg_root_active_total{ack_count_bucket}.
func ackBucket(n int) string {
	switch {
	case n <= 1:
		return "1"
	case n <= 3:
		return "2-3"
	case n <= 5:
		return "4-5"
	case n <= 9:
		return "6-9"
	default:
		return "10+"
	}
}

// RegisterResult is returned by Apply for CmdRegister and CmdRegisterPending.
type RegisterResult struct {
	Err         error
	PID         pid.PID
	ExistingPID pid.PID
	ResolvedPID pid.PID
	FenceToken  uint64
	State       globalreg.RegisterState
}

// UnregisterResult is returned by Apply for CmdUnregister.
type UnregisterResult struct {
	Removed bool
}

// RemoveResult is returned by Apply for CmdRemovePID and CmdRemoveNode.
// HasMore is set by chunked CmdRemoveNode (Limit > 0) when entries remain;
// the Service loops until HasMore=false.
type RemoveResult struct {
	Count   int
	HasMore bool
}

// AckResult is returned by Apply for CmdRegisterAck.
type AckResult struct {
	Recognized bool
	Complete   bool
	Activated  bool
}

// ExpireResult is returned by Apply for CmdRegisterExpired.
type ExpireResult struct {
	MissingAcks []pid.NodeID
	Removed     bool
}

// --- Snapshot / Restore ---

// Snapshot returns a point-in-time snapshot of the FSM state.
// Called by Raft periodically for log compaction.
func (f *FSM) Snapshot() (hraft.FSMSnapshot, error) {
	entries := f.state.snapshot()
	pending := f.state.pendingSnapshot()
	return &fsmSnapshot{payload: fsmSnapshotPayload{Entries: entries, Pending: pending}}, nil
}

// Restore replaces the entire FSM state from a snapshot.
// Called on a follower or recovering node.
func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	var payload fsmSnapshotPayload
	dec := codec.NewDecoder(rc, newMsgpackHandle())
	if err := dec.Decode(&payload); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}

	f.state.restore(payload.Entries, payload.Pending)
	f.tel.recordGlobalregSize(f.state.Len())
	f.tel.setRootPendingInFlight(f.state.PendingLen())
	if f.onRestore != nil {
		f.onRestore()
	}
	return nil
}

// fsmSnapshot implements hraft.FSMSnapshot.
type fsmSnapshot struct {
	payload fsmSnapshotPayload
}

// Persist writes the snapshot data to the sink.
func (s *fsmSnapshot) Persist(sink hraft.SnapshotSink) error {
	enc := codec.NewEncoder(sink, newMsgpackHandle())
	if err := enc.Encode(s.payload); err != nil {
		_ = sink.Cancel()
		return fmt.Errorf("encode snapshot: %w", err)
	}
	return sink.Close()
}

// Release is called after Persist completes. No-op.
func (s *fsmSnapshot) Release() {}
