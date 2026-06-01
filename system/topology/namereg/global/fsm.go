// SPDX-License-Identifier: MPL-2.0

// Package globalreg implements the distributed global name registry backed
// by Raft consensus with a sharded state machine.
package global

import (
	"fmt"
	"io"
	"time"

	"github.com/hashicorp/go-msgpack/v2/codec"
	hraft "github.com/hashicorp/raft"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology/namereg/global"
)

// PendingEvent is delivered to FSM.onPending after applyRegisterPending
// commits a Strong reservation. Each replica receives the event independently
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

// ExpiredEvent is delivered when a Strong reservation reaches a terminal state
// without remaining active — either because the deadline elapsed, the leader /
// caller dropped the reservation explicitly, a required node rejected it, or an
// already-active name was unregistered. RejectedBy is set only on a reject
// (NACK) terminal outcome. RequiredNodes carries the exclusion-holder set so the
// leader can deliver a targeted release to the nodes that latched the exclusion.
type ExpiredEvent struct {
	ExpiredAt     time.Time
	PID           pid.PID
	Name          string
	Reason        string
	RejectedBy    pid.NodeID
	MissingAcks   []pid.NodeID
	RequiredNodes []pid.NodeID
	Epoch         uint64
}

// strongRejectConflict is the ExpiredEvent/ExpiredRecord reason carried when a
// required node rejects a Strong reservation (cross-scope conflict). Distinct
// from the timeout reason so callers can surface a conflict error.
const strongRejectConflict = "conflict"

// FSM implements the hashicorp/raft.FSM interface.
// It is the replicated state machine for the global name registry.
// All Apply calls are serialized by Raft, so no additional locking is
// needed for write operations beyond what the shardedState already provides.
type FSM struct {
	state     *shardedState
	resolve   global.ResolveFunc
	tel       *telemetry
	onRestore func()
	onPending func(PendingEvent)
	onActive  func(ActiveEvent)
	onExpired func(ExpiredEvent)
	onBinding func(BindingEvent)
	pgLabel   string
}

// NewFSM creates a new FSM with an empty sharded state.
// An optional ResolveFunc can be provided for conflict resolution when a name
// is already registered to a different PID (e.g., after partition heal).
// If nil, DefaultResolve (first-write-wins) is used.
func NewFSM(resolve ...global.ResolveFunc) *FSM {
	f := &FSM{
		state:   newShardedState(),
		resolve: global.DefaultResolve,
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

// SetOnBinding wires the Service's active-binding hook. Invoked on every
// replica inside FSM.Apply whenever an ACTIVE binding mutates:
//
//   - CONSISTENT register inserted / conflict-resolved → Deleted=false
//   - STRONG promote-to-active (recordAck completion / DropRequired completion)
//     → Deleted=false
//   - CONSISTENT unregister of an active name → Deleted=true
//   - STRONG unreserve of an active name → Deleted=true
//   - applyRemovePID / applyRemoveNode terminal of any active name → Deleted=true
//
// PENDING reservations are NOT delivered — the cache holds only ACTIVE
// bindings. The leader translates Deleted=true frames into tombstones.
func (f *FSM) SetOnBinding(fn func(BindingEvent)) { f.onBinding = fn }

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
		return f.applyUnregister(cmd, log.Index)
	case CmdRemovePID:
		return f.applyRemovePID(cmd, log.Index)
	case CmdRemoveNode:
		return f.applyRemoveNode(cmd, log.Index)
	case CmdRegisterPending:
		return f.applyRegisterPending(cmd, log.Index)
	case CmdRegisterAck:
		return f.applyRegisterAck(cmd, log.Index)
	case CmdRegisterExpired:
		return f.applyRegisterExpired(cmd, log.Index)
	case CmdRegisterUnreserve:
		return f.applyRegisterUnreserve(cmd, log.Index)
	case CmdDropRequired:
		return f.applyDropRequired(cmd, log.Index)
	case CmdRegisterReject:
		return f.applyRegisterReject(cmd, log.Index)
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
		f.emitBinding(BindingEvent{Name: cmd.Name, PID: cmd.PID, RaftIndex: index})
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
			f.emitBinding(BindingEvent{Name: cmd.Name, PID: cmd.PID, RaftIndex: index})
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

// emitBinding fans out an active-binding event to the dissem hook (if wired).
// Called from every Apply path that mutates the ACTIVE binding set.
func (f *FSM) emitBinding(ev BindingEvent) {
	if f.onBinding != nil {
		f.onBinding(ev)
	}
}

func (f *FSM) applyUnregister(cmd *Command, index uint64) any {
	removed, existed := f.state.unregisterEntry(cmd.Name)
	if existed {
		f.tel.recordGlobalregSize(f.state.Len())
		// A promoted Strong name removed via a Consistent unregister is still a
		// terminal for any held exclusion: deliver a release to its holders.
		if f.onExpired != nil && len(removed.RequiredNodes) > 0 {
			f.tel.recordStrongRelease("unregister_active")
			f.onExpired(ExpiredEvent{
				Name:          cmd.Name,
				PID:           removed.PID,
				Epoch:         removed.Epoch,
				Reason:        "unregister",
				RequiredNodes: removed.RequiredNodes,
				ExpiredAt:     time.Now(),
			})
		}
		f.emitBinding(BindingEvent{
			Name: cmd.Name, PID: removed.PID, RaftIndex: index, Deleted: true,
		})
	}

	return &UnregisterResult{Removed: existed}
}

func (f *FSM) applyRemovePID(cmd *Command, index uint64) any {
	pendingNames := f.state.lookupPendingByPID(cmd.PID)
	for _, n := range pendingNames {
		if e, ok := f.state.unreservePending(n, cmd.PID); ok && e != nil {
			f.tel.recordStrongRelease("pid_exit")
			if f.onExpired != nil {
				f.onExpired(ExpiredEvent{
					Name:          n,
					PID:           e.PID,
					Epoch:         e.Epoch,
					Reason:        "pid_exit",
					RequiredNodes: e.RequiredNodes,
					ExpiredAt:     time.Now(),
				})
			}
		}
	}
	removedNames, count, strongs := f.state.removePIDWithNames(cmd.PID)
	for _, st := range strongs {
		// A promoted Strong name removed on process exit is a terminal: release
		// its exclusion on the holders.
		f.tel.recordStrongRelease("pid_exit_active")
		if f.onExpired != nil {
			f.onExpired(ExpiredEvent{
				Name:          st.Name,
				PID:           st.PID,
				Epoch:         st.Epoch,
				Reason:        "pid_exit",
				RequiredNodes: st.RequiredNodes,
				ExpiredAt:     time.Now(),
			})
		}
	}
	for _, n := range removedNames {
		f.emitBinding(BindingEvent{
			Name: n, PID: cmd.PID, RaftIndex: index, Deleted: true,
		})
	}
	if count > 0 || len(pendingNames) > 0 {
		f.tel.recordGlobalregSize(f.state.Len())
	}

	return &RemoveResult{Count: count + len(pendingNames)}
}

func (f *FSM) applyRemoveNode(cmd *Command, index uint64) any {
	removedNames, count, hasMore, strongs := f.state.removeNodeWithNames(cmd.NodeID, cmd.Limit)
	for _, st := range strongs {
		// A promoted Strong name on the departed node is a terminal: release its
		// exclusion on the surviving holders.
		f.tel.recordStrongRelease("node_removed_active")
		if f.onExpired != nil {
			f.onExpired(ExpiredEvent{
				Name:          st.Name,
				PID:           st.PID,
				Epoch:         st.Epoch,
				Reason:        "node_removed",
				RequiredNodes: st.RequiredNodes,
				ExpiredAt:     time.Now(),
			})
		}
	}
	for _, rn := range removedNames {
		f.emitBinding(BindingEvent{
			Name: rn.Name, PID: rn.PID, RaftIndex: index, Deleted: true,
		})
	}
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
		f.tel.recordStrongPending("inserted")
		f.tel.setStrongPendingInFlight(f.state.PendingLen())
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
			State:      global.RegisterStateUnknown,
		}
	case pendingDedupe:
		f.tel.recordStrongPending("dedupe")
		return &RegisterResult{PID: existing, FenceToken: epoch}
	case pendingConflictActive:
		f.tel.recordStrongPending("conflict_active")
		return &RegisterResult{
			ExistingPID: existing,
			Err:         global.ErrNameAlreadyRegistered,
		}
	case pendingConflictPending:
		f.tel.recordStrongPending("conflict_pending")
		return &RegisterResult{
			ExistingPID: existing,
			Err:         global.ErrPendingConflict,
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
		f.tel.recordStrongAck("fresh")
	} else {
		f.tel.recordStrongAck("duplicate")
	}
	if !complete {
		return &AckResult{Recognized: true, Complete: false}
	}
	promoted, ok := f.state.promotePending(cmd.Name, cmd.Epoch, index)
	if !ok || promoted == nil {
		return &AckResult{Recognized: true, Complete: true}
	}
	f.tel.recordFenceToken(f.pgLabel, promoted.NodeID, index)
	f.tel.recordStrongActive(ackBucket(len(promoted.AckSet)))
	f.tel.setStrongPendingInFlight(f.state.PendingLen())
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
	f.emitBinding(BindingEvent{Name: cmd.Name, PID: promoted.PID, RaftIndex: index})
	return &AckResult{Recognized: true, Complete: true, Activated: true}
}

func (f *FSM) applyRegisterExpired(cmd *Command, index uint64) any {
	expiredAt := time.Now().UnixNano()
	e, missing, ok := f.state.expirePending(cmd.Name, cmd.Epoch, cmd.Reason, expiredAt)
	if !ok || e == nil {
		return &ExpireResult{Removed: false}
	}
	f.tel.recordStrongExpired(cmd.Reason)
	f.tel.setStrongPendingInFlight(f.state.PendingLen())
	if f.onExpired != nil {
		f.onExpired(ExpiredEvent{
			Name:          cmd.Name,
			PID:           e.PID,
			Epoch:         cmd.Epoch,
			Reason:        cmd.Reason,
			MissingAcks:   missing,
			RequiredNodes: e.RequiredNodes,
			ExpiredAt:     time.Unix(0, expiredAt),
		})
	}
	_ = index
	return &ExpireResult{Removed: true, MissingAcks: missing}
}

func (f *FSM) applyRegisterUnreserve(cmd *Command, index uint64) any {
	e, ok := f.state.unreservePending(cmd.Name, cmd.PID)
	if !ok {
		// Not pending — the name may already be promoted to active. Removing the
		// active entry is a terminal for any held exclusion, so deliver a release
		// to its holders (RequiredNodes) keyed to the promotion epoch.
		removed, existed := f.state.unregisterEntry(cmd.Name)
		if existed {
			f.tel.recordGlobalregSize(f.state.Len())
			if f.onExpired != nil && len(removed.RequiredNodes) > 0 {
				f.tel.recordStrongRelease("unreserve_active")
				f.onExpired(ExpiredEvent{
					Name:          cmd.Name,
					PID:           removed.PID,
					Epoch:         removed.Epoch,
					Reason:        "unreserve",
					RequiredNodes: removed.RequiredNodes,
					ExpiredAt:     time.Now(),
				})
			}
			f.emitBinding(BindingEvent{
				Name: cmd.Name, PID: removed.PID, RaftIndex: index, Deleted: true,
			})
		}
		return &UnregisterResult{Removed: existed}
	}
	f.tel.recordStrongRelease("unreserve")
	f.tel.setStrongPendingInFlight(f.state.PendingLen())
	if f.onExpired != nil {
		f.onExpired(ExpiredEvent{
			Name:          cmd.Name,
			PID:           e.PID,
			Epoch:         e.Epoch,
			Reason:        "unreserve",
			RequiredNodes: e.RequiredNodes,
			ExpiredAt:     time.Now(),
		})
	}
	return &UnregisterResult{Removed: true}
}

// applyDropRequired removes a departed node from a pending reservation's
// RequiredNodes set. If the remaining ack set then covers the reduced set the
// entry is promoted to active in the same Apply, mirroring recordAck's
// completion path. Idempotent: a drop for a node no longer required, an
// already-promoted/expired entry, or a stale epoch is a no-op.
func (f *FSM) applyDropRequired(cmd *Command, index uint64) any {
	e, dropped, complete := f.state.dropRequired(cmd.Name, cmd.Epoch, cmd.NodeID)
	if e == nil {
		return &DropRequiredResult{Dropped: false}
	}
	if dropped {
		f.tel.recordStrongDropRequired()
	}
	if !complete {
		return &DropRequiredResult{Dropped: dropped}
	}
	promoted, ok := f.state.promotePending(cmd.Name, cmd.Epoch, index)
	if !ok || promoted == nil {
		return &DropRequiredResult{Dropped: dropped}
	}
	f.tel.recordFenceToken(f.pgLabel, promoted.NodeID, index)
	f.tel.recordStrongActive(ackBucket(len(promoted.AckSet)))
	f.tel.setStrongPendingInFlight(f.state.PendingLen())
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
	f.emitBinding(BindingEvent{Name: cmd.Name, PID: promoted.PID, RaftIndex: index})
	return &DropRequiredResult{Dropped: dropped, Activated: true}
}

// applyRegisterReject terminally fails a pending reservation rejected by a
// required node. NACK dominates: once rejected the entry is removed and never
// resurrects, so any later ack/drop is a no-op. The rejecter and reason are
// carried into the ExpiredEvent so the caller surfaces a conflict.
func (f *FSM) applyRegisterReject(cmd *Command, index uint64) any {
	reason := cmd.Reason
	if reason == "" {
		reason = strongRejectConflict
	}
	expiredAt := time.Now().UnixNano()
	e, ok := f.state.rejectPending(cmd.Name, cmd.Epoch, cmd.AckerNode, reason, expiredAt)
	if !ok || e == nil {
		return &RejectResult{Rejected: false}
	}
	f.tel.recordStrongExpired(reason)
	f.tel.setStrongPendingInFlight(f.state.PendingLen())
	missing := make([]pid.NodeID, 0, len(e.RequiredNodes))
	for _, n := range e.RequiredNodes {
		if _, acked := e.AckSet[n]; !acked {
			missing = append(missing, n)
		}
	}
	if f.onExpired != nil {
		f.onExpired(ExpiredEvent{
			Name:          cmd.Name,
			PID:           e.PID,
			Epoch:         cmd.Epoch,
			Reason:        reason,
			RejectedBy:    cmd.AckerNode,
			MissingAcks:   missing,
			RequiredNodes: e.RequiredNodes,
			ExpiredAt:     time.Unix(0, expiredAt),
		})
	}
	_ = index
	return &RejectResult{Rejected: true, RejectedBy: cmd.AckerNode}
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
	State       global.RegisterState
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

// DropRequiredResult is returned by Apply for CmdDropRequired. Dropped is true
// when the node was present in RequiredNodes and removed; Activated is true
// when the drop completed the (reduced) ack set and promoted the entry.
type DropRequiredResult struct {
	Dropped   bool
	Activated bool
}

// RejectResult is returned by Apply for CmdRegisterReject.
type RejectResult struct {
	RejectedBy pid.NodeID
	Rejected   bool
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
	f.tel.setStrongPendingInFlight(f.state.PendingLen())
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
