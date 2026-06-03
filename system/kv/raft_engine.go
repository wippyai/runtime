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
}

// leaseSweepInterval is how often the leader scans for expired leases.
const leaseSweepInterval = time.Second

// RaftEngine implements kvapi.Engine over the shared cluster raft. Reads are
// local from the replicated FSM; writes are proposed through raft on the leader.
// A single RaftEngine is shared node-wide; store.kv.raft entries scope it by
// key namespace.
type RaftEngine struct {
	raft      raftSubmitter
	bus       event.Bus
	ctx       context.Context
	fsm       *RaftFSM
	logger    *zap.Logger
	router    relay.Receiver
	deadlines map[kvapi.LeaseID]time.Time
	pending   map[uint64]chan applyResult
	cancel    context.CancelFunc
	localNode string
	wg        sync.WaitGroup
	leaseSeq  atomic.Uint64
	schedMu   sync.Mutex
	fwdMu     sync.Mutex
}

// NewRaftEngine builds the shared engine. localNode scopes generated lease ids.
// router carries leader-forwarded writes; nil disables forwarding (writes then
// only succeed on the leader).
func NewRaftEngine(raft raftSubmitter, fsm *RaftFSM, bus event.Bus, localNode string, router relay.Receiver, logger *zap.Logger) *RaftEngine {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &RaftEngine{
		raft:      raft,
		fsm:       fsm,
		bus:       bus,
		logger:    logger.Named("kv-raft"),
		router:    router,
		localNode: localNode,
		deadlines: make(map[kvapi.LeaseID]time.Time),
		pending:   make(map[uint64]chan applyResult),
	}
}

// Start launches the leader-side lease expiry sweeper.
func (e *RaftEngine) Start(ctx context.Context) error {
	e.ctx, e.cancel = context.WithCancel(ctx)
	e.wg.Add(1)
	go e.leaseSweeper()
	return nil
}

// Stop halts the sweeper.
func (e *RaftEngine) Stop() error {
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
	return nil
}

// propose submits a command through raft. On the leader it applies directly; on
// a follower it forwards to the leader over the relay (when a router is wired).
func (e *RaftEngine) propose(c command) (applyResult, error) {
	data := append([]byte{multiplex.KVDomain}, encodeCommand(c)...)
	resp, err := e.raft.Apply(data, raftApplyTimeout)
	if errors.Is(err, raftapi.ErrNotLeader) && e.router != nil {
		res, ferr := e.forwardToLeader(data)
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
	if _, err := e.propose(command{Op: opLeaseGrant, LeaseID: id, TTLms: ttl.Milliseconds()}); err != nil {
		return nil, err
	}
	e.trackLease(id, ttl)

	h := newLease(id, ttl)
	h.keepAlive = func(_ context.Context) error {
		if _, err := e.propose(command{Op: opLeaseRenew, LeaseID: id}); err != nil {
			return err
		}
		e.trackLease(id, ttl)
		return nil
	}
	h.revoke = func(_ context.Context) error {
		_, err := e.propose(command{Op: opLeaseRevoke, LeaseID: id})
		e.forgetLease(id)
		return err
	}
	return h, nil
}

func (e *RaftEngine) trackLease(id kvapi.LeaseID, ttl time.Duration) {
	e.schedMu.Lock()
	e.deadlines[id] = time.Now().Add(ttl)
	e.schedMu.Unlock()
}

func (e *RaftEngine) forgetLease(id kvapi.LeaseID) {
	e.schedMu.Lock()
	delete(e.deadlines, id)
	e.schedMu.Unlock()
}

// leaseSweeper proposes a revoke for each expired lease while this node is the
// leader. On gaining leadership it re-arms deadlines from the replicated lease
// set (resetting the clock — a lease may outlive its TTL by up to one failover,
// the same bound etcd accepts). Followers never expire leases.
func (e *RaftEngine) leaseSweeper() {
	defer e.wg.Done()
	ticker := time.NewTicker(leaseSweepInterval)
	defer ticker.Stop()

	wasLeader := false
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			isLeader := e.raft.IsLeader()
			if isLeader && !wasLeader {
				e.rearmFromState()
			}
			wasLeader = isLeader
			if !isLeader {
				continue
			}
			e.sweepExpired()
		}
	}
}

func (e *RaftEngine) rearmFromState() {
	now := time.Now()
	leases := e.fsm.leaseSnapshot()
	e.schedMu.Lock()
	for id, ttlMs := range leases {
		if _, ok := e.deadlines[id]; !ok {
			ttl := msToDuration(ttlMs)
			e.deadlines[id] = now.Add(ttl)
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

var _ kvapi.Engine = (*RaftEngine)(nil)
