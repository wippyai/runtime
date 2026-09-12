// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/cluster/raft/multiplex"
	"go.uber.org/zap"
)

// raftApplyTimeout bounds a single raft Apply.
const raftApplyTimeout = 5 * time.Second

// raftSubmitter is the narrow slice of raftapi.Service the engine needs:
// propose a command, learn leadership, and resolve the leader for forwarding.
// raftapi.Service satisfies it.
type raftSubmitter interface {
	Apply(cmd []byte, timeout time.Duration) (*raftapi.ApplyResponse, error)
	IsLeader() bool
	Leader() (raftapi.ServerID, raftapi.ServerAddress, error)
	Barrier(timeout time.Duration) error
	CommitIndex() uint64
}

// LinearizableEngine is a kvapi.Engine that can also serve barriered reads and
// scans stamped with the cluster commit index. Only the raft backend satisfies
// it; the kv-backed name registry requires it.
type LinearizableEngine interface {
	kvapi.Engine
	GetLinearizable(key string) (kvapi.Entry, error)
	ScanAtIndex(prefix string, fn func(kvapi.Entry) bool) (uint64, error)
}

// leaseSweepInterval is how often the leader scans for expired leases.
const leaseSweepInterval = time.Second

// DefaultForwardingConcurrency bounds concurrent received KV request batches.
const DefaultForwardingConcurrency = 16

// RaftEngine implements kvapi.Engine over the shared cluster raft. Reads are
// local from the replicated FSM; writes are proposed through raft on the leader.
// A single RaftEngine is shared node-wide; store.kv.raft entries scope it by
// key namespace.
type RaftEngine struct {
	raft             raftSubmitter
	bus              event.Bus
	ctx              context.Context
	fsm              *RaftFSM
	logger           *zap.Logger
	router           relay.Receiver
	deadlines        map[kvapi.LeaseID]time.Time
	pending          map[uint64]forwardReply[applyResult]
	pendingReads     map[uint64]forwardReply[readResult]
	cancel           context.CancelFunc
	localNode        string
	forwardWait      time.Duration
	wg               sync.WaitGroup
	leaseSeq         atomic.Uint64
	schedMu          sync.Mutex
	fwdMu            sync.Mutex
	forwardAdmission sync.Mutex
	forwardSlots     chan struct{}
	forwardRefusals  chan forwardRefusal
	forwardStarted   bool
	forwardStopped   bool
}

// NewRaftEngine builds the shared engine. localNode scopes generated lease ids.
// router carries leader-forwarded writes; nil disables forwarding (writes then
// only succeed on the leader).
func NewRaftEngine(raft raftSubmitter, fsm *RaftFSM, bus event.Bus, localNode string, router relay.Receiver, logger *zap.Logger) *RaftEngine {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &RaftEngine{
		raft:            raft,
		fsm:             fsm,
		bus:             bus,
		logger:          logger.Named("kv-raft"),
		router:          router,
		localNode:       localNode,
		forwardWait:     forwardWaitTimeout,
		forwardSlots:    make(chan struct{}, DefaultForwardingConcurrency),
		forwardRefusals: make(chan forwardRefusal, DefaultForwardingConcurrency),
		deadlines:       make(map[kvapi.LeaseID]time.Time),
		pending:         make(map[uint64]forwardReply[applyResult]),
		pendingReads:    make(map[uint64]forwardReply[readResult]),
	}
}

// ConfigureForwarding bounds retained request batches and workers. Configure
// before publishing the host or calling Start; replies do not consume this budget.
func (e *RaftEngine) ConfigureForwarding(maxConcurrentRequests int) error {
	e.forwardAdmission.Lock()
	defer e.forwardAdmission.Unlock()
	if maxConcurrentRequests <= 0 || e.forwardStarted || e.forwardStopped {
		return fmt.Errorf("kv: forwarding limits require a positive bound before startup")
	}
	e.forwardSlots = make(chan struct{}, maxConcurrentRequests)
	e.forwardRefusals = make(chan forwardRefusal, maxConcurrentRequests)
	return nil
}

// Start launches the leader-side lease expiry sweeper.
func (e *RaftEngine) Start(ctx context.Context) error {
	e.forwardAdmission.Lock()
	defer e.forwardAdmission.Unlock()
	if e.forwardStarted || e.forwardStopped {
		return fmt.Errorf("kv: engine already started or stopped")
	}
	e.ctx, e.cancel = context.WithCancel(ctx)
	e.forwardStarted = true
	e.wg.Add(2)
	go e.refusalLoop()
	go e.leaseSweeper()
	return nil
}

// Stop closes forwarded request admission, cancels pending forwarding waits,
// and joins accepted request workers before releasing the engine's lifetime.
func (e *RaftEngine) Stop() error {
	e.forwardAdmission.Lock()
	e.forwardStopped = true
	if e.cancel != nil {
		e.cancel()
	}
	e.forwardAdmission.Unlock()
	e.wg.Wait()
	return nil
}

// propose submits a command through raft. On the leader it applies directly; on
// a follower it forwards to the leader over the relay (when a router is wired).
func (e *RaftEngine) propose(c command) (applyResult, error) {
	return e.proposeRaw(encodeCommand(c))
}

// proposeRaw submits an already-encoded kv command (single command or txn)
// through raft, forwarding to the leader on a follower.
func (e *RaftEngine) proposeRaw(cmd []byte) (applyResult, error) {
	return e.proposeRawContext(context.Background(), cmd)
}

func (e *RaftEngine) proposeRawContext(ctx context.Context, cmd []byte) (applyResult, error) {
	if err := ctx.Err(); err != nil {
		return applyResult{}, err
	}
	data := append([]byte{multiplex.KVDomain}, cmd...)
	resp, err := e.raft.Apply(data, raftApplyTimeout)
	if errors.Is(err, raftapi.ErrNotLeader) && e.router != nil {
		if err := ctx.Err(); err != nil {
			return applyResult{}, err
		}
		if e.ctx == nil {
			return applyResult{}, staticErr("kv: engine not started")
		}
		owned, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(e.ctx, cancel)
		defer func() { stop(); cancel() }()
		res, ferr := e.forwardToLeaderHopContext(owned, data, 0)
		if ferr != nil {
			return applyResult{}, ferr
		}
		return res, res.Err
	}
	if err != nil {
		return applyResult{}, err
	}
	res, ok := resp.Response.(applyResult)
	if !ok {
		return applyResult{}, fmt.Errorf("kv: unexpected apply response %T", resp.Response)
	}
	return res, res.Err
}

// --- kvapi.Engine reads (local) ---

func (e *RaftEngine) Get(key string) (kvapi.Entry, error) {
	ent, ok := e.fsm.get(key)
	if !ok {
		return kvapi.Entry{}, kvapi.ErrKeyNotFound
	}
	return ent, nil
}

func (e *RaftEngine) Scan(prefix string, fn func(kvapi.Entry) bool) error {
	e.fsm.scan(prefix, fn)
	return nil
}

// BarrierLeader blocks until the local FSM has applied every committed command,
// so a subsequent local read is linearizable. It is a no-op off the leader
// (hashicorp raft Barrier is a leader operation).
func (e *RaftEngine) BarrierLeader() error {
	if !e.raft.IsLeader() {
		return nil
	}
	return e.raft.Barrier(raftApplyTimeout)
}

// GetLinearizable issues a raft barrier so the local FSM has applied every
// committed command, then reads the key locally.
func (e *RaftEngine) GetLinearizable(key string) (kvapi.Entry, error) {
	if err := e.raft.Barrier(raftApplyTimeout); err != nil {
		return kvapi.Entry{}, err
	}
	return e.Get(key)
}

// SnapshotAuthority returns a routing hint, not proof that a snapshot is current.
// ScanAtIndex still barriers on every request, including after this observation.
func (e *RaftEngine) SnapshotAuthority() (string, error) {
	if e.raft.IsLeader() {
		return e.localNode, nil
	}
	leader, _, err := e.raft.Leader()
	return leader, err
}

// ScanAtIndex barriers on the leader, then reads entries and their applied KV
// index from one immutable snapshot. Other Raft domains may have newer indexes.
func (e *RaftEngine) ScanAtIndex(prefix string, fn func(kvapi.Entry) bool) (uint64, error) {
	revision, _, err := e.ScanAtIndexSince(prefix, 0, fn)
	return revision, err
}

// ScanAtIndexSince performs the same authority barrier as ScanAtIndex. Only a
// nonzero matching revision permits skipping the scan; local replica equality
// never bypasses the barrier. Comparison and scan use one published snapshot.
func (e *RaftEngine) ScanAtIndexSince(prefix string, known uint64, fn func(kvapi.Entry) bool) (uint64, bool, error) {
	if err := e.raft.Barrier(raftApplyTimeout); err != nil {
		return 0, false, err
	}
	snap := e.fsm.snap.Load()
	if known != 0 && known == snap.index {
		return snap.index, true, nil
	}
	snap.scan(prefix, fn)
	return snap.index, false, nil
}

func (e *RaftEngine) Watch(ctx context.Context, prefix string) (kvapi.Watcher, error) {
	if e.bus == nil {
		return nil, fmt.Errorf("kv: event bus not available")
	}
	return newWatcher(ctx, e.bus, e.fsm.EventSystem(), prefix)
}

// --- kvapi.Engine writes (proposed) ---

func (e *RaftEngine) Set(key string, value []byte) (kvapi.Version, error) {
	res, err := e.propose(command{Op: opSet, Key: key, Value: value})
	return res.Version, err
}

func (e *RaftEngine) Delete(key string) error {
	_, err := e.propose(command{Op: opDelete, Key: key})
	return err
}

func (e *RaftEngine) SetIfAbsent(key string, value []byte) (kvapi.Version, bool, error) {
	res, err := e.propose(command{Op: opSetIfAbsent, Key: key, Value: value})
	return res.Version, res.OK, err
}

func (e *RaftEngine) CompareAndDelete(key string, expect kvapi.Version) (bool, error) {
	res, err := e.propose(command{Op: opCompareAndDelete, Key: key, Expect: expect})
	return res.OK, err
}

// TxnContext propagates cancellation through forwarded transaction admission
// and reply waiting. After dispatch, cancellation is an uncertain outcome, not
// proof of rollback. A synchronous local Raft Apply remains joined until its
// existing bounded Apply call returns; a known committed result is preserved.
func (e *RaftEngine) TxnContext(ctx context.Context, ops []kvapi.TxnOp) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	res, err := e.proposeRawContext(ctx, encodeTxn(ops))
	return res.OK, err
}

func (e *RaftEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	res, err := e.proposeRaw(encodeTxn(ops))
	return res.OK, err
}

func (e *RaftEngine) CompareAndSwap(key string, expect kvapi.Version, value []byte) (kvapi.Version, bool, error) {
	res, err := e.propose(command{Op: opCAS, Key: key, Value: value, Expect: expect})
	return res.Version, res.OK, err
}

func (e *RaftEngine) SetWithLease(key string, value []byte, lease kvapi.LeaseID) (kvapi.Version, error) {
	res, err := e.propose(command{Op: opSetWithLease, Key: key, Value: value, LeaseID: lease})
	return res.Version, err
}

func (e *RaftEngine) SetIfAbsentWithLease(key string, value []byte, lease kvapi.LeaseID) (kvapi.Version, bool, error) {
	res, err := e.propose(command{Op: opSetIfAbsentWithLease, Key: key, Value: value, LeaseID: lease})
	return res.Version, res.OK, err
}

// --- leases ---

func (e *RaftEngine) GrantLease(_ context.Context, ttl time.Duration) (kvapi.Lease, error) {
	seq := e.leaseSeq.Add(1)
	id := kvapi.LeaseID(fmt.Sprintf("%s-lease-%d", e.localNode, seq))
	expiresAt := time.Now().Add(ttl)
	if _, err := e.propose(command{Op: opLeaseGrant, LeaseID: id, TTLms: ttl.Milliseconds(), ExpiresAtMs: expiresAt.UnixMilli()}); err != nil {
		return nil, err
	}
	e.trackLease(id, expiresAt)

	h := newLease(id, ttl)
	h.keepAlive = func(_ context.Context) error {
		renewed := time.Now().Add(ttl)
		if _, err := e.propose(command{Op: opLeaseRenew, LeaseID: id, TTLms: ttl.Milliseconds(), ExpiresAtMs: renewed.UnixMilli()}); err != nil {
			return err
		}
		e.trackLease(id, renewed)
		return nil
	}
	h.revoke = func(_ context.Context) error {
		_, err := e.propose(command{Op: opLeaseRevoke, LeaseID: id})
		e.forgetLease(id)
		return err
	}
	return h, nil
}

func (e *RaftEngine) trackLease(id kvapi.LeaseID, deadline time.Time) {
	e.schedMu.Lock()
	e.deadlines[id] = deadline
	e.schedMu.Unlock()
}

func (e *RaftEngine) forgetLease(id kvapi.LeaseID) {
	e.schedMu.Lock()
	delete(e.deadlines, id)
	e.schedMu.Unlock()
}

// leaseSweeper proposes a revoke for each expired lease while this node is the
// leader. On gaining leadership it re-arms deadlines from the replicated absolute
// deadline of each lease, so a lease honors its original expiry across any number
// of leadership changes instead of having its clock reset. Followers never expire
// leases.
func (e *RaftEngine) leaseSweeper() {
	defer e.wg.Done()
	ticker := time.NewTicker(leaseSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			if !e.raft.IsLeader() {
				continue
			}
			// Re-arm every tick so the leader picks up leases granted anywhere
			// in the cluster (e.g. a TTL'd write issued on a follower, applied
			// via forwarding), not only on a leadership change. rearmFromState
			// only adds leases not already scheduled, so this is idempotent.
			e.rearmFromState()
			e.sweepExpired()
		}
	}
}

func (e *RaftEngine) rearmFromState() {
	leases := e.fsm.leaseSnapshot()
	e.schedMu.Lock()
	for id, expiresAtMs := range leases {
		if _, ok := e.deadlines[id]; !ok {
			e.deadlines[id] = time.UnixMilli(expiresAtMs)
		}
	}
	e.schedMu.Unlock()
}

func (e *RaftEngine) sweepExpired() {
	now := time.Now()
	var expired []kvapi.LeaseID
	e.schedMu.Lock()
	for id, dl := range e.deadlines {
		if !dl.After(now) {
			expired = append(expired, id)
		}
	}
	e.schedMu.Unlock()

	for _, id := range expired {
		if _, err := e.propose(command{Op: opLeaseRevoke, LeaseID: id}); err != nil {
			e.logger.Debug("lease revoke failed", zap.String("lease", string(id)), zap.Error(err))
			continue
		}
		e.forgetLease(id)
	}
}

var (
	_ kvapi.Engine       = (*RaftEngine)(nil)
	_ LinearizableEngine = (*RaftEngine)(nil)
)
