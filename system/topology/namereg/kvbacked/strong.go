// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"strconv"
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

func ackBase(name string, epoch uint64) string {
	return ackPrefix + name + ":" + strconv.FormatUint(epoch, 10) + ":"
}

func ackKey(name string, epoch uint64, node pid.NodeID) string { return ackBase(name, epoch) + node }

func rejectBase(name string, epoch uint64) string {
	return rejectPrefix + name + ":" + strconv.FormatUint(epoch, 10) + ":"
}

func rejectKey(name string, epoch uint64, node pid.NodeID) string {
	return rejectBase(name, epoch) + node
}

// pendingHeader is the stored payload of a Strong reservation. Epoch is not
// stored here: the authoritative epoch is the kv entry's raft index (Entry.Epoch)
// of the pending key, derived on read so every node agrees on the instance id.
type pendingHeader struct {
	PID              string       `codec:"p"`
	Name             string       `codec:"n"`
	NodeID           pid.NodeID   `codec:"d"`
	RequiredNodes    []pid.NodeID `codec:"r"`
	DeadlineUnixNano int64        `codec:"dl"`
	CreatedAt        int64        `codec:"c"`
}

func decodePending(data []byte) (pendingHeader, error) {
	var v pendingHeader
	err := decodeInto(data, &v)
	return v, err
}

type exclusionState uint8

const (
	exclusionPending exclusionState = iota
	exclusionActive
)

type strongExclusion struct {
	pid   pid.PID
	epoch uint64
	state exclusionState
}

type strongWaiter struct {
	ch chan globalapi.RegisterOutcome
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
	svc            *Service
	membership     func() []pid.NodeID
	isLeader       func() bool
	localConflict  func(name string, p pid.PID) (pid.PID, bool)
	clock          func() time.Time
	logger         *zap.Logger
	exclusions     map[string]strongExclusion
	timers         map[string]*time.Timer
	waiters        map[string][]*strongWaiter
	terminalReason map[string]string
	deadline       time.Duration
	mu             sync.Mutex
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
	s.strong = &strongState{
		svc:            s,
		membership:     deps.Membership,
		isLeader:       isLeader,
		localConflict:  localConflict,
		clock:          clock,
		deadline:       deadline,
		logger:         s.logger.Named("strong"),
		exclusions:     make(map[string]strongExclusion),
		timers:         make(map[string]*time.Timer),
		waiters:        make(map[string][]*strongWaiter),
		terminalReason: make(map[string]string),
	}
}

func (s *Service) registerStrong(ctx context.Context, name string, p pid.PID) (globalapi.RegisterOutcome, error) {
	if s.strong == nil || s.strong.membership == nil {
		return globalapi.RegisterOutcome{}, globalapi.ErrNotAvailable
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
	return true
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

	hdr, err := encode(pendingHeader{
		PID:              p.String(),
		Name:             name,
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

	pe, err := st.svc.get(pendingKey(name))
	if err != nil {
		return globalapi.RegisterOutcome{}, err
	}
	epoch := pe.Epoch

	waiter := &strongWaiter{ch: make(chan globalapi.RegisterOutcome, 1)}
	st.addWaiter(name, waiter)
	defer st.removeWaiter(name, waiter)

	st.reconcile(name)

	select {
	case <-ctx.Done():
		return globalapi.RegisterOutcome{Epoch: epoch}, ctx.Err()
	case out := <-waiter.ch:
		return st.finalize(name, p, out)
	}
}

func (st *strongState) finalize(name string, p pid.PID, out globalapi.RegisterOutcome) (globalapi.RegisterOutcome, error) {
	switch out.State {
	case globalapi.RegisterStateActive:
		if out.PID.String() != p.String() {
			return globalapi.RegisterOutcome{ExistingPID: out.PID}, globalapi.ErrNameAlreadyRegistered
		}
		return globalapi.RegisterOutcome{PID: p, Epoch: out.Epoch, State: globalapi.RegisterStateActive}, nil
	case globalapi.RegisterStateExpired:
		if st.takeReason(name) == strongRejectConflict {
			return out, &globalapi.StrongConflictError{Name: name, Epoch: out.Epoch, Reason: strongRejectConflict}
		}
		return out, &globalapi.StrongRegistrationTimeoutError{Name: name, Epoch: out.Epoch}
	default:
		return out, globalapi.ErrNotAvailable
	}
}

func (st *strongState) setReason(name, reason string) {
	st.mu.Lock()
	st.terminalReason[name] = reason
	st.mu.Unlock()
}

func (st *strongState) takeReason(name string) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	r := st.terminalReason[name]
	delete(st.terminalReason, name)
	return r
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
func (st *strongState) reconcile(name string) {
	// Active wins: deliver success, convert the exclusion to Active, stop timing.
	if e, err := st.svc.get(activeKey(name)); err == nil {
		if av, derr := decodeActive(e.Value); derr == nil && av.Strong {
			ap, _ := pid.ParsePID(av.PID)
			st.onActive(name, e.Epoch, ap)
			return
		}
	}

	pe, err := st.svc.get(pendingKey(name))
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		st.onTerminal(name)
		return
	}
	if err != nil {
		return
	}
	hdr, derr := decodePending(pe.Value)
	if derr != nil {
		return
	}
	epoch := pe.Epoch
	pendingPID, _ := pid.ParsePID(hdr.PID)

	st.attest(name, epoch, pendingPID, hdr.RequiredNodes)

	if st.isLeader() {
		st.leaderDrive(name, epoch, pe.Version, hdr)
	}
}

// attest makes this node ack (and latch an exclusion) or reject the pending,
// once, based on a cross-scope local conflict check.
func (st *strongState) attest(name string, epoch uint64, pendingPID pid.PID, required []pid.NodeID) {
	if !contains(required, st.svc.selfNode) {
		return
	}
	if _, err := st.svc.engine.Get(ackKey(name, epoch, st.svc.selfNode)); err == nil {
		return // already acked
	}
	if _, err := st.svc.engine.Get(rejectKey(name, epoch, st.svc.selfNode)); err == nil {
		return // already rejected
	}
	if cp, conflict := st.localConflict(name, pendingPID); conflict && cp.String() != pendingPID.String() {
		st.setReason(name, strongRejectConflict)
		if _, _, err := st.svc.engine.SetIfAbsent(rejectKey(name, epoch, st.svc.selfNode), []byte(strongRejectConflict)); err != nil {
			st.logger.Debug("strong reject write failed", zap.String("name", name), zap.Error(err))
		}
		return
	}
	st.latch(name, pendingPID, epoch)
	if _, _, err := st.svc.engine.SetIfAbsent(ackKey(name, epoch, st.svc.selfNode), []byte(st.svc.selfNode)); err != nil {
		st.logger.Debug("strong ack write failed", zap.String("name", name), zap.Error(err))
	}
}

// leaderDrive promotes when the ack set is complete, or expires on reject or
// deadline. Runs only on the leader.
func (st *strongState) leaderDrive(name string, epoch, headerVer uint64, hdr pendingHeader) {
	for _, n := range hdr.RequiredNodes {
		if _, err := st.svc.engine.Get(rejectKey(name, epoch, n)); err == nil {
			st.leaderExpire(name, epoch, headerVer, hdr, strongRejectConflict)
			return
		}
	}
	if st.clock().UnixNano() > hdr.DeadlineUnixNano {
		st.leaderExpire(name, epoch, headerVer, hdr, "deadline")
		return
	}
	if !st.complete(name, epoch, hdr.RequiredNodes) {
		st.armTimer(name, hdr.DeadlineUnixNano)
		return
	}
	st.leaderPromote(name, epoch, headerVer, hdr)
}

func (st *strongState) complete(name string, epoch uint64, required []pid.NodeID) bool {
	for _, n := range required {
		if _, err := st.svc.engine.Get(ackKey(name, epoch, n)); err != nil {
			return false
		}
	}
	return true
}

func (st *strongState) leaderPromote(name string, epoch, headerVer uint64, hdr pendingHeader) {
	av, err := encode(activeValue{PID: hdr.PID, Name: name, RequiredNodes: hdr.RequiredNodes, Strong: true})
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
		ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: ackKey(name, epoch, n)})
	}
	if committed, terr := st.svc.engine.Txn(ops); terr != nil || !committed {
		return
	}
	st.takeReason(name)
	st.stopTimer(name)
	st.reconcile(name)
}

func (st *strongState) leaderExpire(name string, epoch, headerVer uint64, hdr pendingHeader, reason string) {
	ops := []kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pendingKey(name), Expect: headerVer},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pendingKey(name)},
	}
	for _, n := range hdr.RequiredNodes {
		ops = append(ops,
			kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: ackKey(name, epoch, n)},
			kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: rejectKey(name, epoch, n)},
		)
	}
	if committed, terr := st.svc.engine.Txn(ops); terr != nil || !committed {
		return
	}
	st.setReason(name, reason)
	st.stopTimer(name)
	st.reconcile(name)
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

func (st *strongState) latch(name string, p pid.PID, epoch uint64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if ex, ok := st.exclusions[name]; ok && ex.epoch >= epoch {
		return
	}
	st.exclusions[name] = strongExclusion{pid: p, epoch: epoch, state: exclusionPending}
}

func (st *strongState) onActive(name string, epoch uint64, ap pid.PID) {
	st.mu.Lock()
	st.exclusions[name] = strongExclusion{pid: ap, epoch: epoch, state: exclusionActive}
	st.mu.Unlock()
	st.stopTimer(name)
	st.deliver(name, globalapi.RegisterOutcome{PID: ap, Epoch: epoch, State: globalapi.RegisterStateActive})
}

func (st *strongState) onTerminal(name string) {
	st.mu.Lock()
	ex, had := st.exclusions[name]
	delete(st.exclusions, name)
	st.mu.Unlock()
	st.stopTimer(name)
	if had {
		st.deliver(name, globalapi.RegisterOutcome{Epoch: ex.epoch, State: globalapi.RegisterStateExpired})
	} else {
		st.deliver(name, globalapi.RegisterOutcome{State: globalapi.RegisterStateExpired})
	}
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
}

func (st *strongState) deliver(name string, out globalapi.RegisterOutcome) {
	st.mu.Lock()
	ws := append([]*strongWaiter(nil), st.waiters[name]...)
	st.mu.Unlock()
	for _, w := range ws {
		select {
		case w.ch <- out:
		default:
		}
	}
}

func (st *strongState) armTimer(name string, deadlineUnixNano int64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.timers[name]; ok {
		return
	}
	d := time.Until(time.Unix(0, deadlineUnixNano))
	if d < 0 {
		d = 0
	}
	st.timers[name] = time.AfterFunc(d, func() {
		st.mu.Lock()
		delete(st.timers, name)
		st.mu.Unlock()
		st.reconcile(name)
	})
}

func (st *strongState) stopTimer(name string) {
	st.mu.Lock()
	t, ok := st.timers[name]
	delete(st.timers, name)
	st.mu.Unlock()
	if ok {
		t.Stop()
	}
}

func contains(s []pid.NodeID, v pid.NodeID) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
