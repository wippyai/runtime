// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"go.uber.org/zap"
)

// Strong-scope on kv. A reservation is a pending header key; each required node
// attests by writing its own ack key (replicated by raft — no ack relay); the
// leader promotes when every required observation is present, or expires on
// deadline. LOCAL/EVENTUAL bindings are independent and never veto a vote.
// reconcile(name) is the single state-machine step run on every kv change and on
// the leader deadline timer; it is idempotent and safe to call redundantly.

const (
	pendingPrefix = registryPrefix + "pending:"
	ackPrefix     = registryPrefix + "ack:"
	resultPrefix  = registryPrefix + "result:"
)

// strongOwnerConflict reports a competing owner in the shared global namespace.
const strongOwnerConflict = "global_owner_conflict"

func pendingKey(name string) string { return pendingPrefix + name }

// Escape name and node components so a colon-bearing value cannot alias the
// fixed attempt segment.
var voteComponentEscaper = strings.NewReplacer("%", "%25", ":", "%3A")

func voteComponent(s string) string { return voteComponentEscaper.Replace(s) }

func ackBase(name, attemptID string) string {
	return ackPrefix + voteComponent(name) + ":" + attemptID + ":"
}

func ackKey(name, attemptID string, node pid.NodeID) string {
	return ackBase(name, attemptID) + voteComponent(node)
}

// terminalResult is committed as an intermediate watch event when a pending
// Strong attempt reaches a terminal state. The key is removed by the same
// transaction, so the event is the durable handoff while the result itself is
// never left in the keyspace. AttemptID binds the evidence to the pending
// header that the transaction version-checks.
type terminalResult struct {
	Name      string       `codec:"n"`
	AttemptID string       `codec:"a"`
	Reason    string       `codec:"r"`
	Missing   []pid.NodeID `codec:"m,omitempty"`
	Epoch     uint64       `codec:"e"`
}

func resultKey(name, attemptID string) string {
	return resultPrefix + strconv.Itoa(len(name)) + ":" + name + ":" + attemptID
}

func decodeTerminalResult(data []byte) (terminalResult, error) {
	var v terminalResult
	if err := decodeInto(data, &v); err != nil {
		return v, err
	}
	if v.AttemptID == "" {
		return v, fmt.Errorf("missing Strong attempt identity in terminal result")
	}
	return v, nil
}

// pendingHeader keeps its identity across required-node changes. Entry.Epoch
// changes on every pending rewrite and cannot identify the registration.
type pendingHeader struct {
	PID              string       `codec:"p"`
	Name             string       `codec:"n"`
	AttemptID        string       `codec:"a"`
	NodeID           pid.NodeID   `codec:"d"`
	RequiredNodes    []pid.NodeID `codec:"r"`
	DeadlineUnixNano int64        `codec:"dl"`
	CreatedAt        int64        `codec:"c"`
}

const strongAttemptBytes = 16

func newStrongAttempt() (string, error) {
	var id [strongAttemptBytes]byte
	if _, err := cryptorand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate Strong attempt identity: %w", err)
	}
	return hex.EncodeToString(id[:]), nil
}

func decodePending(data []byte) (pendingHeader, error) {
	var v pendingHeader
	if err := decodeInto(data, &v); err != nil {
		return v, err
	}
	if v.AttemptID == "" {
		return v, fmt.Errorf("missing Strong attempt identity")
	}
	return v, nil
}

type strongWaiter struct {
	ch        chan strongCompletion
	attemptID string
}

type strongCompletion struct {
	terminal *terminalResult
	out      globalapi.RegisterOutcome
}

type strongTimer struct {
	timer     *time.Timer
	attemptID string
	version   uint64
	wakeAt    int64
}

// StrongDeps are the cluster hooks the Strong plane needs. Only the leader
// samples Members when it stamps a new pending attempt. The captured cohort is
// immutable for that attempt; future attempts take a fresh live-member view.
type StrongDeps struct {
	Members           func() ([]pid.NodeID, error)
	IsLeader          func() bool
	ObserveLeadership func() raftapi.Leadership
	Clock             func() time.Time
	Deadline          time.Duration
}

type strongState struct {
	members           func() ([]pid.NodeID, error)
	svc               *Service
	isLeader          func() bool
	observeLeadership func() raftapi.Leadership
	clock             func() time.Time
	logger            *zap.Logger
	timers            map[string]*strongTimer
	waiters           map[string][]*strongWaiter
	owner             atomic.Pointer[reconcilerOwner]
	deadline          time.Duration
	mu                sync.Mutex
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
	isLeader := deps.IsLeader
	if isLeader == nil {
		isLeader = func() bool { return true }
	}
	s.SetLeaderFunc(isLeader)
	s.strong = &strongState{
		members:           deps.Members,
		svc:               s,
		isLeader:          isLeader,
		observeLeadership: deps.ObserveLeadership,
		clock:             clock,
		deadline:          deadline,
		logger:            s.logger.Named("strong"),
		timers:            make(map[string]*strongTimer),
		waiters:           make(map[string][]*strongWaiter),
	}
}

func (s *Service) registerStrong(ctx context.Context, name string, p pid.PID) (globalapi.RegisterOutcome, error) {
	if s.strong == nil {
		return globalapi.RegisterOutcome{}, globalapi.ErrNotAvailable
	}
	// Strong callers need an ordered watch to observe their committed outcome.
	// This readiness condition does not apply to LOCAL/EVENTUAL registries.
	if !s.nameReady() {
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
	// Clients do not replicate pending Strong claims and cannot observe their
	// own Strong result through this reconciler. Weak scopes are independent.
	if s.strong != nil && s.nonMember != nil && s.nonMember() {
		return false
	}
	// No Strong plane -> no observer barrier needed. Otherwise callers wait
	// until the reconciler has seeded and begun ordered outcome delivery.
	if s.strong == nil {
		return true
	}
	run := s.reconciler.Load()
	if run == nil || !s.ready.Load() || run.ctx.Err() != nil {
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
	run := st.svc.reconciler.Load()
	if run == nil || run.watch.Load() == nil {
		return globalapi.RegisterOutcome{}, globalapi.ErrNotReady
	}
	watch := run.watch.Load()
	select {
	case <-watch.Done():
		return globalapi.RegisterOutcome{}, globalapi.ErrNotReady
	default:
	}
	attemptID, err := newStrongAttempt()
	if err != nil {
		return globalapi.RegisterOutcome{}, err
	}
	nodeID := p.Node
	if nodeID == "" {
		nodeID = st.svc.selfNode
	}
	deadline := st.clock().Add(st.deadline)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) && time.Until(dl) >= 50*time.Millisecond {
		deadline = dl
	}
	// Bound result waiting even when the caller supplies a distant deadline.
	// An earlier caller deadline is preserved by context.WithDeadline. The
	// grace lets the normal expiry transaction win before reporting uncertainty.
	ctx, cancel := context.WithDeadline(ctx, deadline.Add(2*time.Second))
	defer cancel()

	// Enroll before publishing pending. The watcher may observe and promote a
	// committed attempt before the submitting Txn call returns on this node.
	waiter := &strongWaiter{attemptID: attemptID, ch: make(chan strongCompletion, 1)}
	st.addWaiter(name, waiter)
	defer st.removeWaiter(name, waiter)

	for {
		if err := ctx.Err(); err != nil {
			return globalapi.RegisterOutcome{}, err
		}
		if !st.svc.nameReady() {
			return globalapi.RegisterOutcome{}, globalapi.ErrNotReady
		}
		hdr, err := encode(pendingHeader{
			PID:       p.String(),
			Name:      name,
			AttemptID: attemptID,
			NodeID:    nodeID,
			// The current leader stamps RequiredNodes after publication.
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
		if committed {
			break
		}
		// A concurrent owner or pending attempt won. If it disappeared before
		// the conflict read, retry publication under the original deadline.
		if out, err := st.conflictOutcome(name, p); !errors.Is(err, globalapi.ErrNotAvailable) {
			return out, err
		}
		select {
		case <-ctx.Done():
			return globalapi.RegisterOutcome{}, ctx.Err()
		case <-watch.Done():
			return globalapi.RegisterOutcome{}, globalapi.ErrNotReady
		case <-time.After(10 * time.Millisecond):
		}
	}

	select {
	case <-watch.Done():
		return globalapi.RegisterOutcome{}, globalapi.ErrNotReady
	case <-run.ctx.Done():
		return globalapi.RegisterOutcome{}, globalapi.ErrNotReady
	case <-ctx.Done():
		return globalapi.RegisterOutcome{}, ctx.Err()
	case completion := <-waiter.ch:
		return st.finalize(name, p, completion)
	}
}

func (st *strongState) finalize(name string, p pid.PID, completion strongCompletion) (globalapi.RegisterOutcome, error) {
	out := completion.out
	switch out.State {
	case globalapi.RegisterStateActive:
		if out.PID.String() != p.String() {
			return globalapi.RegisterOutcome{ExistingPID: out.PID}, globalapi.ErrNameAlreadyRegistered
		}
		return globalapi.RegisterOutcome{PID: p, Epoch: out.Epoch, State: globalapi.RegisterStateActive}, nil
	case globalapi.RegisterStateExpired:
		if completion.terminal == nil {
			return out, globalapi.ErrNotAvailable
		}
		result := completion.terminal
		if result.Reason == strongOwnerConflict {
			return out, &globalapi.StrongConflictError{Name: name, Epoch: result.Epoch, Reason: strongOwnerConflict}
		}
		missing := make([]string, len(result.Missing))
		copy(missing, result.Missing)
		return out, &globalapi.StrongRegistrationTimeoutError{Name: name, Epoch: result.Epoch, MissingAcks: missing}
	default:
		return out, globalapi.ErrNotAvailable
	}
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
	} else if !errors.Is(err, kvapi.ErrKeyNotFound) {
		return globalapi.RegisterOutcome{}, err
	}
	if e, err := st.svc.get(pendingKey(name)); err == nil {
		if hdr, derr := decodePending(e.Value); derr == nil {
			existing, _ := pid.ParsePID(hdr.PID)
			if existing.String() == p.String() {
				return globalapi.RegisterOutcome{PID: p, Epoch: e.Epoch}, nil
			}
			return globalapi.RegisterOutcome{ExistingPID: existing}, globalapi.ErrPendingConflict
		}
	} else if !errors.Is(err, kvapi.ErrKeyNotFound) {
		return globalapi.RegisterOutcome{}, err
	}
	return globalapi.RegisterOutcome{}, globalapi.ErrNotAvailable
}

// reconcile advances the Strong state machine for name. Safe to call on any node
// on any observed change and on the leader deadline tick; idempotent.
func (st *strongState) reconcile(name string) reconcileReport {
	return st.reconcileForRun(name, nil)
}

func (st *strongState) reconcileForRun(name string, run *reconcilerLifecycle) reconcileReport {
	report := reconcileReport{name: name}
	if run != nil && (st.svc.reconciler.Load() != run || run.ctx.Err() != nil) {
		return report
	}
	if st.svc.localRead == nil {
		report.err = fmt.Errorf("registry reconciliation requires atomic local snapshot reads")
		return report
	}
	entries, _, err := st.svc.localRead.ReadLocalSnapshot([]string{activeKey(name), pendingKey(name)})
	if err != nil {
		report.err = fmt.Errorf("read registry snapshot for %q: %w", name, err)
		return report
	}
	if run != nil && (st.svc.reconciler.Load() != run || run.ctx.Err() != nil) {
		return report
	}
	if active, found := entries[activeKey(name)]; found {
		av, err := decodeActive(active.Value)
		if err != nil {
			report.err = fmt.Errorf("registry record %q: %w", active.Key, err)
			return report
		}
		if err := validateNamingRecord(active.Key, activePrefix, av.Name, av.PID); err != nil {
			report.err = err
			return report
		}
		if av.Strong {
			report.attemptID = av.AttemptID
			return report
		}
	}
	pe, found := entries[pendingKey(name)]
	if !found {
		return report
	}
	hdr, err := decodePending(pe.Value)
	if err != nil {
		report.err = fmt.Errorf("registry record %q: %w", pe.Key, err)
		return report
	}
	if err := validateNamingRecord(pe.Key, pendingPrefix, hdr.Name, hdr.PID); err != nil {
		report.err = err
		return report
	}
	pendingPID, err := pid.ParsePID(hdr.PID)
	if err != nil {
		report.err = fmt.Errorf("registry record %q: invalid owner: %w", pe.Key, err)
		return report
	}
	report.attemptID = hdr.AttemptID
	if len(hdr.RequiredNodes) == 0 {
		if st.isLeader() {
			if ok, err := st.assignObservers(name, pe.Version, hdr); err != nil {
				report.err = err
			} else if !ok {
				report.retryAt = st.clock().Add(time.Second).UnixNano()
			}
		}
		return report
	}
	voteOK, err := st.attest(name, pe.Epoch, pe.Version, hdr.AttemptID, pendingPID, hdr.RequiredNodes)
	if err != nil {
		report.err = err
		return report
	}
	if !voteOK {
		report.retryAt = st.clock().Add(time.Second).UnixNano()
	}

	if st.isLeader() && voteOK {
		retryAt, err := st.leaderDrive(name, pe.Epoch, pe.Version, hdr)
		if err != nil {
			report.err = err
			return report
		}
		if retryAt != 0 {
			report.retryAt = retryAt
		}
	}
	return report
}

func (st *strongState) mark(name string) {
	if owner := st.owner.Load(); owner != nil {
		owner.mark(name)
		return
	}
	st.reconcile(name)
}

func (st *strongState) retire(name, attemptID string) {
	if owner := st.owner.Load(); owner != nil {
		owner.retire(name, attemptID)
	}
}

// assignObservers runs only on the current leader. The submitting node cannot
// omit observers using its own stale membership view. The version check binds
// the snapshot to this pending attempt; later membership changes affect only
// future attempts, never weaken an in-flight acknowledgement requirement.
func (st *strongState) assignObservers(name string, version uint64, hdr pendingHeader) (bool, error) {
	if !st.isLeader() || len(hdr.RequiredNodes) != 0 {
		return false, nil
	}
	if st.members == nil {
		return false, fmt.Errorf("Strong observation requires a Raft membership snapshot")
	}
	nodes, err := st.members()
	if err != nil {
		return false, fmt.Errorf("read Strong observer cohort: %w", err)
	}
	if len(nodes) == 0 {
		return false, fmt.Errorf("empty Strong observer cohort")
	}
	seen := make(map[pid.NodeID]bool, len(nodes))
	hdr.RequiredNodes = make([]pid.NodeID, 0, len(nodes))
	for _, node := range nodes {
		if node == "" {
			return false, fmt.Errorf("empty Strong observer identity")
		}
		if !seen[node] {
			seen[node] = true
			hdr.RequiredNodes = append(hdr.RequiredNodes, node)
		}
	}
	if !seen[st.svc.selfNode] {
		return false, fmt.Errorf("Strong observer cohort omits its leader")
	}
	sort.Strings(hdr.RequiredNodes)
	value, err := encode(hdr)
	if err != nil {
		return false, err
	}
	committed, err := st.svc.engine.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Cond: kvapi.CondVersion, Key: pendingKey(name), Expect: version, Value: value},
	})
	if err != nil {
		// A leadership change or lost reply can interrupt this write. The
		// conditional retry observes the same attempt rather than stopping its
		// ordered watcher on a transient submission failure.
		st.logger.Debug("Strong cohort submission failed", zap.String("name", name), zap.Error(err))
		return false, nil
	}
	return committed, nil
}

// attest records only observation of this exact pending record. It never
// inspects, revokes, or excludes a LOCAL/EVENTUAL binding.
func (st *strongState) attest(name string, epoch, version uint64, attemptID string, pendingPID pid.PID, required []pid.NodeID) (bool, error) {
	if !contains(required, st.svc.selfNode) {
		return true, nil
	}
	if st.svc.localRead == nil {
		return false, fmt.Errorf("registry reconciliation requires atomic local snapshot reads")
	}
	entries, _, err := st.svc.localRead.ReadLocalSnapshot([]string{pendingKey(name)})
	if err != nil {
		return false, fmt.Errorf("read registry snapshot for %q: %w", name, err)
	}
	current, exists := entries[pendingKey(name)]
	if !exists || current.Version != version {
		return false, nil
	}
	header, err := decodePending(current.Value)
	if err != nil {
		return false, fmt.Errorf("registry record %q: %w", current.Key, err)
	}
	if header.AttemptID != attemptID || header.Name != name {
		return false, fmt.Errorf("registry record %q: pending identity changed without version change", current.Key)
	}
	if !contains(header.RequiredNodes, st.svc.selfNode) {
		return true, nil
	}
	return st.writeVote(name, version, attemptID, ackKey(name, attemptID, st.svc.selfNode), []byte(st.svc.selfNode))
}

func (st *strongState) writeVote(name string, version uint64, attemptID, key string, value []byte) (bool, error) {
	ok, err := st.svc.engine.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pendingKey(name), Expect: version},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: key, Value: value},
	})
	if err != nil {
		st.logger.Debug("strong vote write failed", zap.String("name", name), zap.Error(err))
		return false, nil // submission may be uncertain; retry the same vote
	}
	if ok {
		return true, nil
	}
	// Concurrent idempotent votes are success. A changed pending record is
	// never voted for based on the stale snapshot held by this worker.
	if _, err := st.svc.engine.Get(key); err != nil {
		if errors.Is(err, kvapi.ErrKeyNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("read Strong vote for %q: %w", name, err)
	}
	pe, err := st.svc.engine.Get(pendingKey(name))
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read pending vote owner for %q: %w", name, err)
	}
	if pe.Version == version {
		header, err := decodePending(pe.Value)
		if err != nil {
			return false, fmt.Errorf("registry record %q: %w", pe.Key, err)
		}
		if header.AttemptID == attemptID {
			return true, nil
		}
	}
	return false, nil
}

// leaderDrive promotes when the observation set is complete, or expires on
// deadline. Runs only on the leader.
func (st *strongState) leaderDrive(name string, epoch, headerVer uint64, hdr pendingHeader) (int64, error) {
	// Watch-driven ACK events imply preceding votes are applied locally.
	complete, err := st.complete(name, hdr.AttemptID, hdr.RequiredNodes)
	if err != nil {
		return 0, fmt.Errorf("read Strong acknowledgements for %q: %w", name, err)
	}
	if complete {
		committed, err := st.leaderPromote(name, epoch, headerVer, hdr)
		if err != nil {
			return 0, err
		}
		if !committed {
			return st.clock().Add(time.Second).UnixNano(), nil
		}
		return 0, nil
	}
	if st.clock().UnixNano() <= hdr.DeadlineUnixNano {
		if st.owner.Load() == nil {
			st.armTimerVersion(name, hdr.AttemptID, headerVer, hdr.DeadlineUnixNano)
		}
		return hdr.DeadlineUnixNano, nil
	}
	// Deadline reached. Barrier so a committed-but-unapplied ack set is not
	// falsely expired, then re-check completion before giving up. The barrier
	// runs on deadline attempts; failures retry with a delay. It is not needed
	// on the normal acknowledgement path.
	if st.svc.barrier != nil {
		if err := st.svc.barrier(); err != nil {
			// A past deadline would schedule an immediate callback loop.
			if st.owner.Load() == nil {
				st.armTimerVersion(name, hdr.AttemptID, headerVer, time.Now().Add(time.Second).UnixNano())
			}
			return time.Now().Add(time.Second).UnixNano(), nil
		}
	}
	complete, err = st.complete(name, hdr.AttemptID, hdr.RequiredNodes)
	if err != nil {
		return 0, fmt.Errorf("read Strong acknowledgements for %q: %w", name, err)
	}
	if complete {
		committed, err := st.leaderPromote(name, epoch, headerVer, hdr)
		if err != nil {
			return 0, err
		}
		if !committed {
			return st.clock().Add(time.Second).UnixNano(), nil
		}
		return 0, nil
	}
	committed, err := st.leaderExpire(name, epoch, headerVer, hdr, "deadline")
	if err != nil {
		return 0, err
	}
	if !committed {
		return st.clock().Add(time.Second).UnixNano(), nil
	}
	return 0, nil
}

func (st *strongState) voteExists(key string) (bool, error) {
	_, err := st.svc.engine.Get(key)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		return false, nil
	}
	return false, err
}

func (st *strongState) complete(name, attemptID string, required []pid.NodeID) (bool, error) {
	for _, n := range required {
		acked, err := st.voteExists(ackKey(name, attemptID, n))
		if err != nil {
			return false, err
		}
		if !acked {
			return false, nil
		}
	}
	return true, nil
}

func (st *strongState) leaderPromote(name string, epoch, headerVer uint64, hdr pendingHeader) (bool, error) {
	if len(hdr.RequiredNodes) == 0 {
		return false, fmt.Errorf("cannot promote an unstamped Strong attempt")
	}
	av, err := encode(activeValue{PID: hdr.PID, Name: name, AttemptID: hdr.AttemptID, RequiredNodes: hdr.RequiredNodes, Strong: true})
	if err != nil {
		return false, err
	}
	p, err := pid.ParsePID(hdr.PID)
	if err != nil {
		return false, err
	}
	idx, err := encode(indexValue{PID: hdr.PID, Name: name})
	if err != nil {
		return false, err
	}
	ops := []kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pendingKey(name), Expect: headerVer},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pendingKey(name)},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: activeKey(name), Value: av},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: pidIndexKey(p, name), Value: idx},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: nodeIndexKey(p, name), Value: idx},
	}
	for _, n := range hdr.RequiredNodes {
		// The leader's preceding scan is only a hint. Validate every required
		// observation again in the committed promotion transaction.
		ops = append(ops,
			kvapi.TxnOp{Kind: kvapi.TxnCheck, Cond: kvapi.CondExists, Key: ackKey(name, hdr.AttemptID, n)},
			kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: ackKey(name, hdr.AttemptID, n)},
		)
	}
	committed, terr := st.svc.engine.Txn(ops)
	if terr != nil {
		return false, nil
	}
	if !committed {
		// Promotion failed. If a conflicting binding now holds the name (active
		// not absent), the reservation lost -> terminal conflict (otherwise it is
		// a benign version race the next reconcile/sweep retries). Without this a
		// complete-but-unpromotable pending would retry forever and hang the
		// waiter until its deadline.
		active, gerr := st.voteExists(activeKey(name))
		if gerr != nil {
			return false, fmt.Errorf("read active naming binding for %q: %w", name, gerr)
		}
		if active {
			return st.leaderExpire(name, epoch, headerVer, hdr, strongOwnerConflict)
		}
		return false, nil
	}
	return true, nil
}

func (st *strongState) leaderExpire(name string, epoch, headerVer uint64, hdr pendingHeader, reason string) (bool, error) {
	// Compute the missing-ack set before the txn deletes the ack keys, so the
	// timeout error can report which nodes failed to ack.
	var missing []pid.NodeID
	ops := []kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pendingKey(name), Expect: headerVer},
	}
	for _, n := range hdr.RequiredNodes {
		ack := ackKey(name, hdr.AttemptID, n)
		_, err := st.svc.engine.Get(ack)
		if err != nil && !errors.Is(err, kvapi.ErrKeyNotFound) {
			return false, fmt.Errorf("read Strong acknowledgement for %q: %w", name, err)
		}
		absent := errors.Is(err, kvapi.ErrKeyNotFound)
		if absent {
			missing = append(missing, n)
		}
		if reason == "deadline" {
			// Keep the reported missing set true at commit, not merely at
			// the leader's earlier read.
			condition := kvapi.CondExists
			if absent {
				condition = kvapi.CondAbsent
			}
			ops = append(ops,
				kvapi.TxnOp{Kind: kvapi.TxnCheck, Cond: condition, Key: ack},
			)
		}
		ops = append(ops,
			kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: ack},
		)
	}
	if reason == "deadline" && len(missing) == 0 {
		return st.leaderPromote(name, epoch, headerVer, hdr)
	}
	result, err := encode(terminalResult{
		Name:      name,
		AttemptID: hdr.AttemptID,
		Reason:    reason,
		Missing:   missing,
		Epoch:     epoch,
	})
	if err != nil {
		return false, err
	}
	// Put the attempt-bound result before deleting the pending header. KV watch
	// delivery preserves this intermediate event even though the result key is
	// deleted before the transaction completes. A stale header or a pre-existing
	// result aborts the whole transaction and therefore emits no terminal event.
	// Keep the result Put ahead of pending deletion in the ordered watch stream;
	// the result Delete follows all terminal cleanup below.
	ops = append([]kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: pendingKey(name), Expect: headerVer},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: resultKey(name, hdr.AttemptID), Value: result},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pendingKey(name)},
	}, ops[1:]...)
	ops = append(ops, kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: resultKey(name, hdr.AttemptID)})
	if committed, terr := st.svc.engine.Txn(ops); terr != nil || !committed {
		return false, nil
	}
	return true, nil
}

func (st *strongState) unreserve(name string) (bool, error) {
	pe, err := st.svc.get(pendingKey(name))
	if err == nil {
		hdr, derr := decodePending(pe.Value)
		if derr == nil {
			if _, err := st.leaderExpire(name, pe.Epoch, pe.Version, hdr, "unreserve"); err != nil {
				return false, err
			}
		}
	}
	return st.svc.UnregisterScope(context.Background(), name, globalapi.Consistent)
}

// --- committed outcomes + waiters + timers ---

func (st *strongState) onActive(name, attemptID string, epoch, observed uint64, ap pid.PID) {
	st.recordActive(name, attemptID, epoch, observed, ap)
	st.notifyActive(name, attemptID, epoch, ap)
}

func (st *strongState) recordActive(name, attemptID string, epoch, observed uint64, ap pid.PID) {
	st.retire(name, attemptID)
	st.stopTimerAttempt(name, attemptID)
}

func (st *strongState) notifyActive(name, attemptID string, epoch uint64, ap pid.PID) {
	st.svc.monitor(ap)
	st.deliver(name, attemptID, strongCompletion{out: globalapi.RegisterOutcome{
		PID: ap, Epoch: epoch, State: globalapi.RegisterStateActive,
	}})
}

func (st *strongState) onTerminal(name, attemptID string, observed uint64) {
	st.retire(name, attemptID)
	st.stopTimerAttempt(name, attemptID)
	// Absence is not a terminal outcome: promotion also deletes pending.
	// Only a matching committed result resolves the caller's failure.
}

// reserved is introspection of the global namespace only. Weak registries
// never use it as an admission condition.
func (st *strongState) reserved(name string) (pid.PID, bool) {
	if entry, err := st.svc.engine.Get(activeKey(name)); err == nil {
		if active, err := decodeActive(entry.Value); err == nil && active.Strong {
			p, err := pid.ParsePID(active.PID)
			return p, err == nil
		}
	}
	if entry, err := st.svc.engine.Get(pendingKey(name)); err == nil {
		if pending, err := decodePending(entry.Value); err == nil {
			p, err := pid.ParsePID(pending.PID)
			return p, err == nil
		}
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

func (st *strongState) deliver(name, attemptID string, completion strongCompletion) {
	if attemptID == "" {
		return
	}
	st.mu.Lock()
	ws := append([]*strongWaiter(nil), st.waiters[name]...)
	st.mu.Unlock()
	for _, w := range ws {
		if w.attemptID != attemptID {
			continue
		}
		select {
		case w.ch <- completion:
		default:
		}
	}
}

func (st *strongState) armTimerAttempt(name, attemptID string, deadlineUnixNano int64) {
	st.armTimerVersion(name, attemptID, 0, deadlineUnixNano)
}

func (st *strongState) armTimerVersion(name, attemptID string, version uint64, deadlineUnixNano int64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if old := st.timers[name]; old != nil {
		if old.version > version {
			return
		}
		if old.attemptID == attemptID && old.wakeAt <= deadlineUnixNano {
			return
		}
		old.timer.Stop()
	}
	d := time.Until(time.Unix(0, deadlineUnixNano))
	if d < 0 {
		d = 0
	}
	wake := &strongTimer{attemptID: attemptID, version: version, wakeAt: deadlineUnixNano}
	wake.timer = time.AfterFunc(d, func() { st.fireTimer(name, wake) })
	st.timers[name] = wake
}

func (st *strongState) fireTimer(name string, wake *strongTimer) {
	st.mu.Lock()
	if st.timers[name] != wake {
		st.mu.Unlock()
		return
	}
	delete(st.timers, name)
	st.mu.Unlock()
	if owner := st.owner.Load(); owner != nil {
		owner.retryAttempt(name, wake.attemptID)
	} else {
		st.mark(name)
	}
}

func (st *strongState) stopTimer(name string) {
	st.mu.Lock()
	t, ok := st.timers[name]
	delete(st.timers, name)
	st.mu.Unlock()
	if ok {
		t.timer.Stop()
	}
}

func (st *strongState) stopTimerAttempt(name, attemptID string) {
	st.mu.Lock()
	wake := st.timers[name]
	if wake == nil || wake.attemptID != attemptID {
		st.mu.Unlock()
		return
	}
	delete(st.timers, name)
	st.mu.Unlock()
	wake.timer.Stop()
}

func contains(s []pid.NodeID, v pid.NodeID) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
