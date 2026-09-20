// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"go.uber.org/zap"
)

// Strong-scope on kv. A reservation is a pending header key; each required node
// attests by writing its own ack key (replicated by raft — no ack relay); the
// leader promotes when every required ack is present, or expires on deadline or
// reject. Per-node exclusions block LOCAL/EVENTUAL shadowing during the window.
// reconcile(name) is the single state-machine step run on every kv change and on
// the leader deadline timer; it is idempotent and safe to call redundantly.

const (
	pendingPrefix = registryPrefix + "pending:"
	ackPrefix     = registryPrefix + "ack:"
	rejectPrefix  = registryPrefix + "reject:"
)

// strongRejectConflict mirrors the global service reason for a cross-scope NACK.
const strongRejectConflict = "cross_scope_conflict"

func pendingKey(name string) string { return pendingPrefix + name }

// Vote keys are scoped to the opaque registration attempt. Entry.Epoch is zero
// for local engines and can repeat across replacement observations, while the
// AttemptID remains stable when the pending header is rewritten for membership.
// The name and node components escape '%' and ':' so a colon-bearing component
// cannot absorb the fixed attempt segment and alias another vote key. Simple
// components retain the requested ack:<name>:<attemptID>:<node> form.
var voteComponentEscaper = strings.NewReplacer("%", "%25", ":", "%3A")

func voteComponent(s string) string {
	return voteComponentEscaper.Replace(s)
}

func ackBase(name, attemptID string) string {
	return ackPrefix + voteComponent(name) + ":" + attemptID + ":"
}

func ackKey(name, attemptID string, node pid.NodeID) string {
	return ackBase(name, attemptID) + voteComponent(string(node))
}

func rejectBase(name, attemptID string) string {
	return rejectPrefix + voteComponent(name) + ":" + attemptID + ":"
}

func rejectKey(name, attemptID string, node pid.NodeID) string {
	return rejectBase(name, attemptID) + voteComponent(node)
}

// pendingHeader is the stored payload of a Strong reservation. Epoch is not
// stored here: the authoritative epoch is the kv entry's raft index (Entry.Epoch)
// of the pending key, derived on read so every node agrees on the instance id.
type pendingHeader struct {
	PID  string `codec:"p"`
	Name string `codec:"n"`
	// AttemptID is stable across pending-header rewrites and promotion. It is
	// intentionally independent of the pending and active entry epochs.
	AttemptID        string       `codec:"a"`
	NodeID           pid.NodeID   `codec:"d"`
	RequiredNodes    []pid.NodeID `codec:"r"`
	DeadlineUnixNano int64        `codec:"dl"`
	CreatedAt        int64        `codec:"c"`
}

func decodePending(data []byte) (pendingHeader, error) {
	var v pendingHeader
	err := decodeInto(data, &v)
	if err == nil && v.AttemptID == "" {
		err = fmt.Errorf("missing Strong attempt identity")
	}
	return v, err
}

type exclusionState uint8

const (
	exclusionPending exclusionState = iota
	exclusionActive
)

type strongExclusion struct {
	pid       pid.PID
	attemptID string
	epoch     uint64
	state     exclusionState
}

type strongWaiter struct {
	ch        chan globalapi.RegisterOutcome
	attemptID string
	pid       pid.PID
}

// strongTimer is owned by exactly one reservation. Timer lifecycle operations
// must carry that identity: a delayed completion of an older transaction may
// run after the name has already been reserved again.
type strongTimer struct {
	timer     *time.Timer
	attemptID string
	epoch     uint64
	version   uint64
}

// StrongDeps are the cluster hooks the Strong plane needs. membership returns the
// current live node set (the required-ack quorum, including self); isLeader gates
// leader-only promotion/expiry; localConflict reports a conflicting LOCAL/EVENTUAL
// binding so this node NACKs instead of acking.
type StrongDeps struct {
	Membership    func() []pid.NodeID
	IsLeader      func() bool
	LocalConflict func(name string, p pid.PID) (pid.PID, bool)
	Clock         func() time.Time
	Deadline      time.Duration
}

type strongState struct {
	svc             *Service
	membership      func() []pid.NodeID
	isLeader        func() bool
	localConflict   func(name string, p pid.PID) (pid.PID, bool)
	clock           func() time.Time
	logger          *zap.Logger
	exclusions      map[string]strongExclusion
	timers          map[string]strongTimer
	waiters         map[string][]*strongWaiter
	terminalReason  map[string]string
	terminalMissing map[string][]pid.NodeID
	terminalEpoch   map[string]uint64
	deadline        time.Duration
	mu              sync.Mutex
}

const strongAttemptBytes = 16

// newStrongAttempt creates the opaque identity for one Strong registration.
// It is generated before the pending transaction so all later record versions
// carry the same identity.
func newStrongAttempt() (string, error) {
	var raw [strongAttemptBytes]byte
	if _, err := cryptorand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate Strong attempt identity: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// ConfigureStrong enables the Strong-scope plane with cluster hooks. Until it is
// called, Strong registers are rejected with ErrNotAvailable.
func (s *Service) ConfigureStrong(deps StrongDeps) {
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	deadline := deps.Deadline
	if deadline <= 0 {
		deadline = globalapi.StrongDeadline
	}
	localConflict := deps.LocalConflict
	if localConflict == nil {
		localConflict = func(string, pid.PID) (pid.PID, bool) { return pid.PID{}, false }
	}
	isLeader := deps.IsLeader
	if isLeader == nil {
		isLeader = func() bool { return true }
	}
	s.SetLeaderFunc(isLeader)
	s.strong = &strongState{
		svc:             s,
		membership:      deps.Membership,
		isLeader:        isLeader,
		localConflict:   localConflict,
		clock:           clock,
		deadline:        deadline,
		logger:          s.logger.Named("strong"),
		exclusions:      make(map[string]strongExclusion),
		timers:          make(map[string]strongTimer),
		waiters:         make(map[string][]*strongWaiter),
		terminalReason:  make(map[string]string),
		terminalMissing: make(map[string][]pid.NodeID),
		terminalEpoch:   make(map[string]uint64),
	}
}

func (s *Service) registerStrong(ctx context.Context, name string, p pid.PID) (globalapi.RegisterOutcome, error) {
	if s.strong == nil || s.strong.membership == nil {
		return globalapi.RegisterOutcome{}, globalapi.ErrNotAvailable
	}
	if s.nonMember != nil && s.nonMember() {
		return globalapi.RegisterOutcome{}, fmt.Errorf("naming participant without a local replica requires an authority feed")
	}
	if _, ok := s.engine.(kvapi.LocalSnapshotReader); !ok {
		return globalapi.RegisterOutcome{}, fmt.Errorf("registry reconciliation requires atomic local snapshot reads")
	}
	if run := s.reconciler.Load(); run != nil && !s.nameReady() {
		return globalapi.RegisterOutcome{}, globalapi.ErrNotReady
	}
	return s.strong.register(ctx, name, p)
}

func (s *Service) unregisterStrong(_ context.Context, name string) (bool, error) {
	if s.strong == nil {
		return false, nil
	}
	return s.strong.unreserve(name)
}

func (s *Service) strongReserved(name string) (pid.PID, bool) {
	if s.strong == nil {
		return pid.PID{}, false
	}
	return s.strong.reserved(name)
}

func (s *Service) nameReady() bool {
	// No Strong plane -> no join barrier needed. Otherwise the node is ready
	// only once the reconciler has seeded (learned and latched the cluster's
	// in-flight/active Strong reservations), so it cannot shadow one.
	if s.strong == nil {
		return true
	}
	run := s.reconciler.Load()
	if run == nil {
		return s.ready.Load()
	}
	if !s.ready.Load() || run.ctx.Err() != nil {
		return false
	}
	watch := run.watch.Load()
	if watch == nil {
		return false
	}
	select {
	case <-watch.Done():
		return false
	default:
		return true
	}
}

func (st *strongState) register(ctx context.Context, name string, p pid.PID) (globalapi.RegisterOutcome, error) {
	nodeID := p.Node
	if nodeID == "" {
		nodeID = st.svc.selfNode
	}
	deadline := st.clock().Add(st.deadline)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) && time.Until(dl) >= 50*time.Millisecond {
		deadline = dl
	}
	// Bound result waiting even when the caller supplies a distant deadline.
	// The owner context is joined so a synchronization failure cancels this
	// waiter without deleting or reaping the claim it did not create.
	ownerCtx := st.svc.reconcileContext()
	ctx, cancel := context.WithCancel(ctx)
	stopOwner := context.AfterFunc(ownerCtx, cancel)
	defer func() {
		stopOwner()
		cancel()
	}()
	ctx, deadlineCancel := context.WithDeadline(ctx, deadline.Add(2*time.Second))
	defer deadlineCancel()
	if err := ctx.Err(); err != nil {
		return globalapi.RegisterOutcome{}, err
	}
	attemptID, err := newStrongAttempt()
	if err != nil {
		return globalapi.RegisterOutcome{}, err
	}

	hdr, err := encode(pendingHeader{
		PID:              p.String(),
		Name:             name,
		AttemptID:        attemptID,
		NodeID:           nodeID,
		RequiredNodes:    st.requiredNodes(),
		DeadlineUnixNano: deadline.UnixNano(),
		CreatedAt:        st.clock().UnixNano(),
	})
	if err != nil {
		return globalapi.RegisterOutcome{}, err
	}

	committed, err := st.svc.engine.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondAbsent, Key: activeKey(name)},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: pendingKey(name), Value: hdr},
	})
	if err != nil {
		return globalapi.RegisterOutcome{}, err
	}
	if !committed {
		return st.conflictOutcome(name, p)
	}
	if err := ctx.Err(); err != nil {
		return globalapi.RegisterOutcome{}, err
	}

	pe, err := st.svc.get(pendingKey(name))
	if err != nil {
		return globalapi.RegisterOutcome{}, err
	}
	if err := ctx.Err(); err != nil {
		return globalapi.RegisterOutcome{}, err
	}
	epoch := pe.Epoch

	// Decode before installing the waiter: an undecodable pending (should never
	// happen — we just wrote it) must fail fast, not hang the waiter forever.
	// Best-effort remove the bad entry so it does not linger.
	phdr, derr := decodePending(pe.Value)
	if derr != nil {
		_, _ = st.svc.engine.CompareAndDelete(pendingKey(name), pe.Version)
		return globalapi.RegisterOutcome{}, derr
	}
	if phdr.AttemptID == "" {
		_, _ = st.svc.engine.CompareAndDelete(pendingKey(name), pe.Version)
		return globalapi.RegisterOutcome{}, fmt.Errorf("registry record %q: missing Strong attempt identity", pendingKey(name))
	}
	if phdr.AttemptID != attemptID {
		return globalapi.RegisterOutcome{}, fmt.Errorf("registry record %q: Strong attempt identity changed", pendingKey(name))
	}
	pendingPID, _ := pid.ParsePID(phdr.PID)

	waiter := &strongWaiter{ch: make(chan globalapi.RegisterOutcome, 1), attemptID: attemptID, pid: p}
	st.addWaiter(name, waiter)
	defer st.removeWaiter(name, waiter)
	if err := ctx.Err(); err != nil {
		return globalapi.RegisterOutcome{Epoch: epoch}, err
	}

	// Drive the just-opened pending directly: the caller goroutine knows it
	// exists (read via the leader), so it must not go through reconcile, whose
	// local read may not see the freshly-forwarded write yet and would
	// mis-fire onTerminal. The watch reconciler advances it from here on.
	st.attest(name, epoch, pe.Version, attemptID, pendingPID, phdr.RequiredNodes)
	if err := ctx.Err(); err != nil {
		return globalapi.RegisterOutcome{Epoch: epoch}, err
	}
	if st.isLeader() {
		st.leaderDrive(name, epoch, pe.Version, phdr)
	}

	select {
	case <-ctx.Done():
		return globalapi.RegisterOutcome{Epoch: epoch}, ctx.Err()
	case out := <-waiter.ch:
		return st.finalize(name, p, attemptID, out)
	}
}

func (st *strongState) finalize(name string, p pid.PID, attemptID string, out globalapi.RegisterOutcome) (globalapi.RegisterOutcome, error) {
	switch out.State {
	case globalapi.RegisterStateActive:
		if out.PID.String() != p.String() {
			return globalapi.RegisterOutcome{ExistingPID: out.PID}, globalapi.ErrNameAlreadyRegistered
		}
		return globalapi.RegisterOutcome{PID: p, Epoch: out.Epoch, State: globalapi.RegisterStateActive}, nil
	case globalapi.RegisterStateExpired:
		reason, missing, epoch := st.takeTerminal(attemptID)
		if reason == strongRejectConflict {
			return out, &globalapi.StrongConflictError{Name: name, Epoch: epoch, Reason: strongRejectConflict}
		}
		return out, &globalapi.StrongRegistrationTimeoutError{Name: name, Epoch: epoch, MissingAcks: missing}
	default:
		return out, globalapi.ErrNotAvailable
	}
}

func (st *strongState) setTerminal(name, attemptID, reason string, missing []pid.NodeID, epoch uint64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	// Only a waiting caller consumes terminal details. Remote attempts and
	// callers that already canceled must not accumulate retained outcomes.
	waiting := false
	for _, waiter := range st.waiters[name] {
		if waiter.attemptID == attemptID {
			waiting = true
			break
		}
	}
	if !waiting {
		return
	}
	st.terminalReason[attemptID] = reason
	st.terminalEpoch[attemptID] = epoch
	if len(missing) > 0 {
		st.terminalMissing[attemptID] = missing
	}
}

// takeTerminal returns and clears the terminal reason, missing-ack node set,
// and epoch for an attempt. The attempt key prevents an old terminal event
// from supplying details to a later registration of the same name and PID.
func (st *strongState) takeTerminal(attemptID string) (string, []string, uint64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	r := st.terminalReason[attemptID]
	delete(st.terminalReason, attemptID)
	ep := st.terminalEpoch[attemptID]
	delete(st.terminalEpoch, attemptID)
	m := st.terminalMissing[attemptID]
	delete(st.terminalMissing, attemptID)
	out := make([]string, len(m))
	copy(out, m)
	return r, out, ep
}

func (st *strongState) conflictOutcome(name string, p pid.PID) (globalapi.RegisterOutcome, error) {
	if e, err := st.svc.get(activeKey(name)); err == nil {
		if av, derr := decodeActive(e.Value); derr == nil {
			existing, _ := pid.ParsePID(av.PID)
			if existing.String() == p.String() {
				return globalapi.RegisterOutcome{PID: p, Epoch: e.Epoch, State: globalapi.RegisterStateActive}, nil
			}
			return globalapi.RegisterOutcome{ExistingPID: existing}, globalapi.ErrNameAlreadyRegistered
		}
	}
	if e, err := st.svc.get(pendingKey(name)); err == nil {
		if hdr, derr := decodePending(e.Value); derr == nil {
			existing, _ := pid.ParsePID(hdr.PID)
			if existing.String() == p.String() {
				return globalapi.RegisterOutcome{PID: p, Epoch: e.Epoch}, nil
			}
			return globalapi.RegisterOutcome{ExistingPID: existing}, globalapi.ErrPendingConflict
		}
	}
	return globalapi.RegisterOutcome{}, globalapi.ErrNameAlreadyRegistered
}

func (st *strongState) requiredNodes() []pid.NodeID {
	nodes := st.membership()
	if len(nodes) == 0 {
		return []pid.NodeID{st.svc.selfNode}
	}
	seen := make(map[pid.NodeID]struct{}, len(nodes)+1)
	out := make([]pid.NodeID, 0, len(nodes)+1)
	for _, n := range append(nodes, st.svc.selfNode) {
		if _, dup := seen[n]; dup || n == "" {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

// reconcile advances the Strong state machine for name. Safe to call on any node
// on any observed change and on the leader deadline tick; idempotent.
func (st *strongState) reconcile(name string) error {
	return st.reconcileForRun(name, st.svc.reconciler.Load())
}

// reconcileDeleted uses a watch deletion's previous record to identify the
// affected waiter. Direct empty-snapshot reconciliation still releases its
// current local exclusion; ordering/freshness of that observation is a
// separate admission concern.
func (st *strongState) reconcileDeleted(name, attemptID string, pendingDeleted bool) error {
	if err := st.reconcile(name); err != nil {
		return err
	}
	if attemptID != "" {
		// Promotion deletes pending and publishes active in the same transaction.
		// The pending delete is not a terminal event for that attempt.
		if pendingDeleted {
			st.mu.Lock()
			ex, exists := st.exclusions[name]
			promoted := exists && ex.attemptID == attemptID && ex.state == exclusionActive
			st.mu.Unlock()
			if promoted {
				return nil
			}
		}
		st.onTerminal(name, attemptID)
	}
	return nil
}

func (st *strongState) reconcileForRun(name string, run *reconcilerLifecycle) (err error) {
	defer func() {
		if err != nil {
			st.svc.failReconciler(run, err)
		}
	}()
	if run != nil && (st.svc.reconciler.Load() != run || run.ctx.Err() != nil) {
		return globalapi.ErrNotReady
	}
	// Reads are local (no forwarding): reconcile runs on the watch goroutine and
	// must not block on a leader round-trip. Read active and pending together so
	// a promotion cannot be observed as two different states. The write path
	// still uses versions and transactions; this snapshot only makes the local
	// observation coherent.
	reader, ok := st.svc.engine.(kvapi.LocalSnapshotReader)
	if !ok {
		return fmt.Errorf("registry reconciliation requires atomic local snapshot reads")
	}
	entries, _, err := reader.ReadLocalSnapshot([]string{activeKey(name), pendingKey(name)})
	if err != nil {
		return fmt.Errorf("read registry snapshot for %q: %w", name, err)
	}
	if run != nil && (st.svc.reconciler.Load() != run || run.ctx.Err() != nil) {
		return globalapi.ErrNotReady
	}

	// Active Strong wins: deliver success, convert the exclusion to Active, stop
	// timing. A malformed active record is an error, not absence: clearing a
	// held exclusion on an invalid record would open a cross-scope admission hole.
	if e, found := entries[activeKey(name)]; found {
		av, derr := decodeActive(e.Value)
		if derr != nil {
			return fmt.Errorf("registry record %q: %w", e.Key, derr)
		}
		if err := validateNamingRecord(e.Key, activePrefix, av.Name, av.PID); err != nil {
			return err
		}
		if av.Strong {
			ap, perr := pid.ParsePID(av.PID)
			if perr != nil {
				return fmt.Errorf("registry record %q: invalid owner: %w", e.Key, perr)
			}
			st.onActive(name, e.Epoch, av.AttemptID, ap)
			return nil
		}
	}

	pe, found := entries[pendingKey(name)]
	if !found {
		// Preserve direct reconciliation of absent reservations. Watch deletes
		// additionally carry their prior attempt identity for waiter delivery
		// when this node rejected before latching a local exclusion.
		st.onTerminal(name, "")
		return nil
	}
	hdr, derr := decodePending(pe.Value)
	if derr != nil {
		return fmt.Errorf("registry record %q: %w", pe.Key, derr)
	}
	if hdr.AttemptID == "" {
		return fmt.Errorf("registry record %q: missing Strong attempt identity", pe.Key)
	}
	if err := validateNamingRecord(pe.Key, pendingPrefix, hdr.Name, hdr.PID); err != nil {
		return err
	}
	epoch := pe.Epoch
	pendingPID, perr := pid.ParsePID(hdr.PID)
	if perr != nil {
		return fmt.Errorf("registry record %q: invalid owner: %w", pe.Key, perr)
	}

	st.attest(name, epoch, pe.Version, hdr.AttemptID, pendingPID, hdr.RequiredNodes)

	if st.isLeader() {
		st.leaderDrive(name, epoch, pe.Version, hdr)
	}
	return nil
}

// attest makes this node ack (and latch an exclusion) or reject the pending,
// once, based on a cross-scope local conflict check.
func (st *strongState) attest(name string, epoch, pendingVersion uint64, attemptID string, pendingPID pid.PID, required []pid.NodeID) {
	if !contains(required, st.svc.selfNode) {
		return
	}
	ack := ackKey(name, attemptID, st.svc.selfNode)
	reject := rejectKey(name, attemptID, st.svc.selfNode)
	if _, err := st.svc.engine.Get(ack); err == nil {
		return // already acked for this attempt
	}
	if _, err := st.svc.engine.Get(reject); err == nil {
		st.setTerminal(name, attemptID, strongRejectConflict, nil, epoch)
		return // already rejected for this attempt
	}
	if cp, conflict := st.localConflict(name, pendingPID); conflict && cp.String() != pendingPID.String() {
		committed, err := st.svc.engine.Txn([]kvapi.TxnOp{
			{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pendingKey(name), Expect: pendingVersion},
			{Kind: kvapi.TxnCheck, Cond: kvapi.CondAbsent, Key: ack},
			{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: reject, Value: []byte(strongRejectConflict)},
		})
		if err != nil {
			st.logger.Debug("strong reject write failed", zap.String("name", name), zap.Error(err))
		} else if !committed {
			return
		}
		st.setTerminal(name, attemptID, strongRejectConflict, nil, epoch)
		return
	}
	// Keep the local exclusion latched across an uncertain or failed vote
	// submission. A durable vote with a lost response must not leave a window
	// for a competing LOCAL/EVENTUAL registration. A later authoritative
	// pending/active/terminal observation replaces or releases this latch.
	st.latch(name, pendingPID, attemptID, epoch)
	committed, err := st.svc.engine.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pendingKey(name), Expect: pendingVersion},
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondAbsent, Key: reject},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: ack, Value: []byte(st.svc.selfNode)},
	})
	if err != nil {
		st.logger.Debug("strong ack write failed", zap.String("name", name), zap.Error(err))
		return
	}
	if !committed {
		// Keep the latch until a later pending/active/terminal observation
		// explains the failed conditional write.
		return
	}
}

// leaderDrive promotes when the ack set is complete, or expires on reject or
// deadline. Runs only on the leader.
func (st *strongState) leaderDrive(name string, epoch, headerVer uint64, hdr pendingHeader) {
	// Watch-driven path needs no barrier: raft applies in order, so the ack/
	// reject event that triggered this drive implies all prior acks/rejects are
	// already applied locally. A reject wins; a complete ack set promotes.
	for _, n := range hdr.RequiredNodes {
		if _, err := st.svc.engine.Get(rejectKey(name, hdr.AttemptID, n)); err == nil {
			st.leaderExpire(name, epoch, headerVer, hdr, strongRejectConflict)
			return
		}
	}
	if st.complete(name, hdr.AttemptID, hdr.RequiredNodes) {
		st.leaderPromote(name, epoch, headerVer, hdr)
		return
	}
	if st.clock().UnixNano() <= hdr.DeadlineUnixNano {
		st.armTimer(name, hdr.AttemptID, epoch, headerVer, hdr.DeadlineUnixNano)
		return
	}
	// Deadline reached. Barrier so a committed-but-unapplied ack set is not
	// falsely expired, then re-check completion before giving up. The barrier
	// runs on deadline attempts; failures retry with a delay. It is not needed
	// on the normal acknowledgement path.
	if st.svc.barrier != nil {
		if err := st.svc.barrier(); err != nil {
			// A past deadline would schedule an immediate callback loop.
			st.armTimer(name, hdr.AttemptID, epoch, headerVer, time.Now().Add(time.Second).UnixNano())
			return
		}
	}
	if st.complete(name, hdr.AttemptID, hdr.RequiredNodes) {
		st.leaderPromote(name, epoch, headerVer, hdr)
		return
	}
	st.leaderExpire(name, epoch, headerVer, hdr, "deadline")
}

func (st *strongState) complete(name, attemptID string, required []pid.NodeID) bool {
	for _, n := range required {
		if _, err := st.svc.engine.Get(ackKey(name, attemptID, n)); err != nil {
			return false
		}
	}
	return true
}

func (st *strongState) leaderPromote(name string, epoch, headerVer uint64, hdr pendingHeader) {
	av, err := encode(activeValue{PID: hdr.PID, Name: name, AttemptID: hdr.AttemptID, RequiredNodes: hdr.RequiredNodes, Strong: true})
	if err != nil {
		return
	}
	p, _ := pid.ParsePID(hdr.PID)
	idx, err := encode(indexValue{PID: hdr.PID, Name: name})
	if err != nil {
		return
	}
	ops := []kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pendingKey(name), Expect: headerVer},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pendingKey(name)},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: activeKey(name), Value: av},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: pidIndexKey(p, name), Value: idx},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: nodeIndexKey(p, name), Value: idx},
	}
	for _, n := range hdr.RequiredNodes {
		// The leader's preceding scan is only a hint. Validate the entire
		// admission decision in the committed transaction so a rejection that
		// precedes promotion in Raft order cannot be ignored.
		ops = append(ops,
			kvapi.TxnOp{Kind: kvapi.TxnCheck, Cond: kvapi.CondExists, Key: ackKey(name, hdr.AttemptID, n)},
			kvapi.TxnOp{Kind: kvapi.TxnCheck, Cond: kvapi.CondAbsent, Key: rejectKey(name, hdr.AttemptID, n)},
			kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: ackKey(name, hdr.AttemptID, n)},
		)
	}
	committed, terr := st.svc.engine.Txn(ops)
	if terr != nil {
		return
	}
	if !committed {
		// Promotion failed. If a conflicting binding now holds the name (active
		// not absent), the reservation lost -> terminal conflict (otherwise it is
		// a benign version race the next reconcile/sweep retries). Without this a
		// complete-but-unpromotable pending would retry forever and hang the
		// waiter until its deadline.
		if _, gerr := st.svc.engine.Get(activeKey(name)); gerr == nil {
			st.leaderExpire(name, epoch, headerVer, hdr, strongRejectConflict)
		}
		return
	}
	// The promotion belongs to this attempt. A replacement may already have
	// armed a timer after the transaction committed, so cleanup is conditional
	// on the old identity.
	st.takeTerminal(hdr.AttemptID)
	st.stopTimer(name, hdr.AttemptID)
	_ = st.reconcile(name)
}

func (st *strongState) leaderExpire(name string, epoch, headerVer uint64, hdr pendingHeader, reason string) {
	// Compute the missing-ack set before the txn deletes the ack keys, so the
	// timeout error can report which nodes failed to ack.
	var missing []pid.NodeID
	ops := []kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pendingKey(name), Expect: headerVer},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pendingKey(name)},
	}
	for _, n := range hdr.RequiredNodes {
		ack := ackKey(name, hdr.AttemptID, n)
		_, err := st.svc.engine.Get(ack)
		if err != nil && !errors.Is(err, kvapi.ErrKeyNotFound) {
			return
		}
		absent := errors.Is(err, kvapi.ErrKeyNotFound)
		if absent {
			missing = append(missing, n)
		}
		if reason == "deadline" {
			// Keep the reported missing set and rejection precedence true at
			// commit, not merely at the leader's earlier read.
			condition := kvapi.CondExists
			if absent {
				condition = kvapi.CondAbsent
			}
			ops = append(ops,
				kvapi.TxnOp{Kind: kvapi.TxnCheck, Cond: condition, Key: ack},
				kvapi.TxnOp{Kind: kvapi.TxnCheck, Cond: kvapi.CondAbsent, Key: rejectKey(name, hdr.AttemptID, n)},
			)
		}
		ops = append(ops,
			kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: ack},
			kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: rejectKey(name, hdr.AttemptID, n)},
		)
	}
	if reason == "deadline" && len(missing) == 0 {
		st.leaderPromote(name, epoch, headerVer, hdr)
		return
	}
	if committed, terr := st.svc.engine.Txn(ops); terr != nil || !committed {
		return
	}
	st.setTerminal(name, hdr.AttemptID, reason, missing, epoch)
	st.stopTimer(name, hdr.AttemptID)
	// The delete is this attempt's terminal event. Deliver it with the stable
	// identity immediately; a later watch/sweep for the same name must not be
	// able to complete a replacement attempt.
	st.onTerminal(name, hdr.AttemptID)
}

func (st *strongState) unreserve(name string) (bool, error) {
	pe, err := st.svc.get(pendingKey(name))
	if err == nil {
		hdr, derr := decodePending(pe.Value)
		if derr == nil {
			st.leaderExpire(name, pe.Epoch, pe.Version, hdr, "unreserve")
		}
	}
	return st.svc.UnregisterScope(context.Background(), name, globalapi.Consistent)
}

// --- exclusions + waiters + timers ---

func (st *strongState) latch(name string, p pid.PID, attemptID string, epoch uint64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if ex, ok := st.exclusions[name]; ok && ex.epoch >= epoch {
		return
	}
	st.exclusions[name] = strongExclusion{pid: p, attemptID: attemptID, epoch: epoch, state: exclusionPending}
}

func (st *strongState) onActive(name string, epoch uint64, attemptID string, ap pid.PID) {
	st.mu.Lock()
	if ex, ok := st.exclusions[name]; ok && ex.epoch > epoch {
		st.mu.Unlock()
		return
	}
	st.exclusions[name] = strongExclusion{pid: ap, attemptID: attemptID, epoch: epoch, state: exclusionActive}
	st.mu.Unlock()
	st.stopTimer(name, attemptID)
	st.svc.monitor(ap)
	st.deliver(name, attemptID, ap, globalapi.RegisterOutcome{PID: ap, Epoch: epoch, State: globalapi.RegisterStateActive})
}

func (st *strongState) onTerminal(name, attemptID string) {
	st.mu.Lock()
	ex, ok := st.exclusions[name]
	if attemptID == "" && ok {
		attemptID = ex.attemptID
	}
	if attemptID == "" {
		st.mu.Unlock()
		return
	}
	if ok && ex.attemptID != attemptID {
		st.mu.Unlock()
		// A replacement holds the name now; finish only the older caller.
		st.deliverAttempt(name, attemptID, globalapi.RegisterOutcome{State: globalapi.RegisterStateExpired})
		return
	}
	if ok {
		delete(st.exclusions, name)
	}
	st.mu.Unlock()
	if ok {
		st.stopTimer(name, ex.attemptID)
	}
	// finalize reads terminal detail by attempt, so a delayed terminal event for
	// an older registration cannot complete a replacement waiter.
	if ok {
		st.deliver(name, attemptID, ex.pid, globalapi.RegisterOutcome{State: globalapi.RegisterStateExpired})
		return
	}
	// A rejection can delete pending before this node ever latched an
	// exclusion. The watch's previous record still identifies the right waiter.
	st.deliverAttempt(name, attemptID, globalapi.RegisterOutcome{State: globalapi.RegisterStateExpired})
}

func (st *strongState) reserved(name string) (pid.PID, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if ex, ok := st.exclusions[name]; ok {
		return ex.pid, true
	}
	return pid.PID{}, false
}

func (st *strongState) addWaiter(name string, w *strongWaiter) {
	st.mu.Lock()
	st.waiters[name] = append(st.waiters[name], w)
	st.mu.Unlock()
}

func (st *strongState) removeWaiter(name string, w *strongWaiter) {
	st.mu.Lock()
	defer st.mu.Unlock()
	ws := st.waiters[name]
	for i, x := range ws {
		if x == w {
			st.waiters[name] = append(ws[:i], ws[i+1:]...)
			break
		}
	}
	if len(st.waiters[name]) == 0 {
		delete(st.waiters, name)
	}
	remaining := false
	for _, other := range st.waiters[name] {
		if other.attemptID == w.attemptID {
			remaining = true
			break
		}
	}
	if !remaining {
		delete(st.terminalReason, w.attemptID)
		delete(st.terminalEpoch, w.attemptID)
		delete(st.terminalMissing, w.attemptID)
	}
}

func (st *strongState) deliver(name, attemptID string, owner pid.PID, out globalapi.RegisterOutcome) {
	st.mu.Lock()
	ws := append([]*strongWaiter(nil), st.waiters[name]...)
	st.mu.Unlock()
	for _, w := range ws {
		if w.attemptID != attemptID || !w.pid.Equal(owner) {
			continue
		}
		select {
		case w.ch <- out:
		default:
		}
	}
}

func (st *strongState) deliverAttempt(name, attemptID string, out globalapi.RegisterOutcome) {
	st.mu.Lock()
	ws := append([]*strongWaiter(nil), st.waiters[name]...)
	st.mu.Unlock()
	for _, w := range ws {
		if w.attemptID != attemptID {
			continue
		}
		select {
		case w.ch <- out:
		default:
		}
	}
}

func (st *strongState) armTimer(name, attemptID string, epoch, version uint64, deadlineUnixNano int64) {
	d := time.Until(time.Unix(0, deadlineUnixNano))
	if d < 0 {
		d = 0
	}
	run := st.svc.reconciler.Load()
	var timer *time.Timer
	var replaced *time.Timer
	st.mu.Lock()
	if current, ok := st.timers[name]; ok {
		if current.attemptID == attemptID && current.epoch == epoch && current.version == version {
			st.mu.Unlock()
			return
		}
		// A stale continuation may arrive after a replacement has installed
		// its timer. The KV version orders reservations even without Raft
		// epochs, so an older observation cannot replace a newer timer.
		if current.version > version || (current.version == version && current.epoch >= epoch) {
			st.mu.Unlock()
			return
		}
		replaced = current.timer
	}
	timer = time.AfterFunc(d, func() {
		st.mu.Lock()
		current, ok := st.timers[name]
		if !ok || current.timer != timer || current.attemptID != attemptID || current.epoch != epoch || current.version != version {
			st.mu.Unlock()
			return
		}
		delete(st.timers, name)
		st.mu.Unlock()
		_ = st.reconcileForRun(name, run)
	})
	st.timers[name] = strongTimer{timer: timer, attemptID: attemptID, epoch: epoch, version: version}
	st.mu.Unlock()
	if replaced != nil {
		replaced.Stop()
	}
}

func (st *strongState) stopTimer(name, attemptID string) {
	st.mu.Lock()
	t, ok := st.timers[name]
	if !ok || t.attemptID != attemptID {
		st.mu.Unlock()
		return
	}
	delete(st.timers, name)
	st.mu.Unlock()
	t.timer.Stop()
}

func contains(s []pid.NodeID, v pid.NodeID) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
