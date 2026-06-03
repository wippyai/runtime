// SPDX-License-Identifier: MPL-2.0

// Package kvbacked implements the cluster-wide name registry on top of the
// shared kv engine (under the reserved _sys:registry namespace) instead of a
// dedicated raft FSM. It satisfies both the globalapi.Registry (write) and
// topology.GlobalRegistry (cross-scope read) facades so it is a drop-in for the
// FSM-backed global.Service. Consistent-scope ownership uses linearizable kv
// txns; Strong-scope orchestration is layered on the same keyspace.
package kvbacked

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology/namereg/global"
	"go.uber.org/zap"
)

// Reserved keyspace under the shared kv for the name registry.
const (
	registryPrefix = "_sys:registry:"
	activePrefix   = registryPrefix + "active:"
	pidxPrefix     = registryPrefix + "pidx:"
	nidxPrefix     = registryPrefix + "nidx:"
)

// maxResolveRetries bounds the conflict-resolution CAS loop when an override
// ResolveFunc awards a contested name to the incoming claimant. Exhaustion under
// sustained contention returns a retryable error rather than a hard failure.
const maxResolveRetries = 32

func activeKey(name string) string { return activePrefix + name }

// pidIndexBase is the scan prefix for every name owned by p.
func pidIndexBase(p pid.PID) string { return pidxPrefix + p.String() + ":" }

// nodeIndexBase is the scan prefix for every name owned by a pid on node.
func nodeIndexBase(node pid.NodeID) string { return nidxPrefix + node + ":" }

func pidIndexKey(p pid.PID, name string) string { return pidIndexBase(p) + name }
func nodeIndexKey(p pid.PID, name string) string {
	return nodeIndexBase(p.Node) + p.String() + ":" + name
}

// activeValue is the stored payload of an active name binding.
type activeValue struct {
	PID           string       `codec:"p"`
	Name          string       `codec:"n"`
	RequiredNodes []pid.NodeID `codec:"r,omitempty"`
	Strong        bool         `codec:"s,omitempty"`
}

// indexValue is the payload of a reverse-index key: it carries the canonical
// (pid, name) so scans never parse identity back out of the physical key.
type indexValue struct {
	PID  string `codec:"p"`
	Name string `codec:"n"`
}

func encode(v any) ([]byte, error) {
	var buf []byte
	if err := codec.NewEncoderBytes(&buf, &codec.MsgpackHandle{}).Encode(v); err != nil {
		return nil, err
	}
	return buf, nil
}

func decodeInto(data []byte, v any) error {
	return codec.NewDecoderBytes(data, &codec.MsgpackHandle{}).Decode(v)
}

func decodeActive(data []byte) (activeValue, error) {
	var v activeValue
	err := decodeInto(data, &v)
	return v, err
}

func decodeIndex(data []byte) (indexValue, error) {
	var v indexValue
	err := codec.NewDecoderBytes(data, &codec.MsgpackHandle{}).Decode(&v)
	return v, err
}

// leaderReadEngine is the optional forwarded-leader-read surface of the raft
// engine, giving read-your-writes on a follower after a forwarded write.
type leaderReadEngine interface {
	GetViaLeader(key string) (kvapi.Entry, error)
}

// barrierEngine is the optional leader-barrier surface: blocks until the local
// FSM has applied all committed commands so a subsequent local read is
// linearizable. Used before the Strong promote/expire decision.
type barrierEngine interface {
	BarrierLeader() error
}

// Service is the kv-backed name registry.
type Service struct {
	engine     kvapi.Engine
	leaderRead leaderReadEngine
	topo       topology.Topology
	leaderFn   func() bool
	strong     *strongState
	dissem     *global.Dissem
	logger     *zap.Logger
	barrier    func() error
	resolve    globalapi.ResolveFunc
	self       pid.PID
	monitored  sync.Map
	selfNode   pid.NodeID
	ready      atomic.Bool
}

// ConfigureDissem attaches the active-binding dissemination plane so non-member
// nodes (no raft FSM) resolve names from the gossiped cache. The Dissem must be
// registered as the membership UserDelegate by the caller.
func (s *Service) ConfigureDissem(d *global.Dissem) { s.dissem = d }

// SetLeaderFunc sets the leadership predicate used by the dissem translator
// (leader broadcasts, followers apply locally).
func (s *Service) SetLeaderFunc(fn func() bool) {
	if fn != nil {
		s.leaderFn = fn
	}
}

// get reads a key through the leader when the backend supports it, so a write
// path observes its own and prior committed writes even on a follower.
func (s *Service) get(key string) (kvapi.Entry, error) {
	if s.leaderRead != nil {
		return s.leaderRead.GetViaLeader(key)
	}
	return s.engine.Get(key)
}

// NewService builds the registry over engine. The engine must be linearizable
// for Strong-scope correctness (the raft backend); Consistent-scope works on
// any kvapi.Engine. resolve may be nil (defaults to first-write-wins).
func NewService(engine kvapi.Engine, selfNode pid.NodeID, resolve globalapi.ResolveFunc, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	if resolve == nil {
		resolve = globalapi.DefaultResolve
	}
	s := &Service{
		engine:   engine,
		selfNode: selfNode,
		resolve:  resolve,
		logger:   logger.Named("kvreg"),
		leaderFn: func() bool { return true },
		self:     pid.PID{Node: selfNode, Host: RegistryHostID},
	}
	if lr, ok := engine.(leaderReadEngine); ok {
		s.leaderRead = lr
	}
	if be, ok := engine.(barrierEngine); ok {
		s.barrier = be.BarrierLeader
	}
	return s
}

// Register registers name at Consistent scope.
func (s *Service) Register(ctx context.Context, name string, p pid.PID) (pid.PID, error) {
	out, err := s.RegisterScope(ctx, name, p, globalapi.Consistent)
	if err != nil {
		return out.ExistingPID, err
	}
	return out.PID, nil
}

// RegisterScope dispatches by scope. Local/Eventual are caller errors here.
func (s *Service) RegisterScope(ctx context.Context, name string, p pid.PID, mode globalapi.RegistrationMode) (globalapi.RegisterOutcome, error) {
	switch mode {
	case globalapi.Consistent:
		return s.registerConsistent(name, p)
	case globalapi.Strong:
		return s.registerStrong(ctx, name, p)
	default:
		return globalapi.RegisterOutcome{}, globalapi.ErrNotAvailable
	}
}

func (s *Service) registerConsistent(name string, p pid.PID) (globalapi.RegisterOutcome, error) {
	val, err := encode(activeValue{PID: p.String(), Name: name})
	if err != nil {
		return globalapi.RegisterOutcome{}, err
	}
	idx, err := encode(indexValue{PID: p.String(), Name: name})
	if err != nil {
		return globalapi.RegisterOutcome{}, err
	}

	committed, err := s.engine.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAbsent, Key: activeKey(name), Value: val},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: pidIndexKey(p, name), Value: idx},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: nodeIndexKey(p, name), Value: idx},
	})
	if err != nil {
		return globalapi.RegisterOutcome{}, err
	}
	if committed {
		e, gerr := s.get(activeKey(name))
		if gerr != nil {
			return globalapi.RegisterOutcome{}, gerr
		}
		s.monitor(p)
		return globalapi.RegisterOutcome{PID: p, Epoch: e.Epoch, State: globalapi.RegisterStateActive}, nil
	}
	return s.resolveConflict(name, p)
}

// resolveConflict handles a contested Consistent register: dedupe on identical
// PID, otherwise consult ResolveFunc and CAS-swap if the incoming claimant wins.
func (s *Service) resolveConflict(name string, incoming pid.PID) (globalapi.RegisterOutcome, error) {
	for attempt := 0; attempt < maxResolveRetries; attempt++ {
		e, err := s.get(activeKey(name))
		if errors.Is(err, kvapi.ErrKeyNotFound) {
			out, rerr := s.registerConsistent(name, incoming)
			return out, rerr
		}
		if err != nil {
			return globalapi.RegisterOutcome{}, err
		}
		av, derr := decodeActive(e.Value)
		if derr != nil {
			return globalapi.RegisterOutcome{}, derr
		}
		existing, perr := pid.ParsePID(av.PID)
		if perr != nil {
			return globalapi.RegisterOutcome{}, perr
		}
		if existing.String() == incoming.String() {
			return globalapi.RegisterOutcome{PID: incoming, Epoch: e.Epoch, State: globalapi.RegisterStateActive}, nil
		}
		winner := s.resolve(name, existing, incoming)
		if winner.String() == existing.String() {
			return globalapi.RegisterOutcome{PID: existing, ExistingPID: existing}, globalapi.ErrNameAlreadyRegistered
		}
		swapped, serr := s.swapOwner(name, e.Version, existing, incoming)
		if serr != nil {
			return globalapi.RegisterOutcome{}, serr
		}
		if swapped {
			ne, gerr := s.get(activeKey(name))
			if gerr != nil {
				return globalapi.RegisterOutcome{}, gerr
			}
			return globalapi.RegisterOutcome{PID: incoming, Epoch: ne.Epoch, State: globalapi.RegisterStateActive}, nil
		}
	}
	return globalapi.RegisterOutcome{}, globalapi.ErrNotReady
}

func (s *Service) swapOwner(name string, expectVer kvapi.Version, old, incoming pid.PID) (bool, error) {
	val, err := encode(activeValue{PID: incoming.String(), Name: name})
	if err != nil {
		return false, err
	}
	idx, err := encode(indexValue{PID: incoming.String(), Name: name})
	if err != nil {
		return false, err
	}
	return s.engine.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: activeKey(name), Expect: expectVer},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: activeKey(name), Value: val},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pidIndexKey(old, name)},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: nodeIndexKey(old, name)},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: pidIndexKey(incoming, name), Value: idx},
		{Kind: kvapi.TxnPut, Cond: kvapi.CondAny, Key: nodeIndexKey(incoming, name), Value: idx},
	})
}

// Unregister removes the Consistent-scope registration for a name.
func (s *Service) Unregister(ctx context.Context, name string) (bool, error) {
	return s.UnregisterScope(ctx, name, globalapi.Consistent)
}

// UnregisterScope removes the registration for the given scope.
func (s *Service) UnregisterScope(ctx context.Context, name string, mode globalapi.RegistrationMode) (bool, error) {
	if mode == globalapi.Strong {
		return s.unregisterStrong(ctx, name)
	}
	e, err := s.get(activeKey(name))
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	av, derr := decodeActive(e.Value)
	if derr != nil {
		return false, derr
	}
	owner, perr := pid.ParsePID(av.PID)
	if perr != nil {
		return false, perr
	}
	return s.engine.Txn([]kvapi.TxnOp{
		{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: activeKey(name), Expect: e.Version},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: activeKey(name)},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pidIndexKey(owner, name)},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: nodeIndexKey(owner, name)},
	})
}

// Lookup reads a name from the local kv replica (stale-tolerant).
func (s *Service) Lookup(_ context.Context, name string, opts ...globalapi.LookupOption) (globalapi.LookupResult, error) {
	var o globalapi.LookupOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.ByPID != nil {
		names := s.namesForPID(*o.ByPID)
		return globalapi.LookupResult{PID: *o.ByPID, NamesForPID: names, Found: len(names) > 0}, nil
	}
	e, err := s.engine.Get(activeKey(name))
	if errors.Is(err, kvapi.ErrKeyNotFound) {
		if s.dissem != nil {
			if p, ok := s.dissem.Lookup(name); ok {
				return globalapi.LookupResult{PID: p, Found: true}, nil
			}
		}
		return globalapi.LookupResult{Found: false}, nil
	}
	if err != nil {
		return globalapi.LookupResult{}, err
	}
	av, derr := decodeActive(e.Value)
	if derr != nil {
		return globalapi.LookupResult{}, derr
	}
	p, perr := pid.ParsePID(av.PID)
	if perr != nil {
		return globalapi.LookupResult{}, perr
	}
	return globalapi.LookupResult{PID: p, Found: true}, nil
}

func (s *Service) namesForPID(p pid.PID) []string {
	var names []string
	_ = s.engine.Scan(pidIndexBase(p), func(e kvapi.Entry) bool {
		if iv, err := decodeIndex(e.Value); err == nil {
			names = append(names, iv.Name)
		}
		return true
	})
	return names
}

// Remove removes all names registered to p.
func (s *Service) Remove(_ context.Context, p pid.PID) error {
	s.reap(pidIndexBase(p))
	return nil
}

// RemoveNode removes all names owned by processes on nodeID.
func (s *Service) RemoveNode(_ context.Context, nodeID pid.NodeID) error {
	s.reap(nodeIndexBase(nodeID))
	return nil
}

// reap deletes every binding referenced by the reverse-index keys under base.
// Idempotent: a key already removed by a concurrent reaper is harmless.
func (s *Service) reap(base string) {
	type victim struct {
		p    pid.PID
		name string
	}
	var victims []victim
	_ = s.engine.Scan(base, func(e kvapi.Entry) bool {
		iv, err := decodeIndex(e.Value)
		if err != nil {
			return true
		}
		p, perr := pid.ParsePID(iv.PID)
		if perr != nil {
			return true
		}
		victims = append(victims, victim{p: p, name: iv.Name})
		return true
	})
	for _, v := range victims {
		s.deleteBinding(v.p, v.name)
	}
}

// deleteBinding reaps p's binding for name. p's stale reverse-index keys are
// always removed, but the active binding is deleted only if it still belongs to
// p (version-guarded) — so reaping a dead PID never clobbers a name that a live
// PID has since taken over.
func (s *Service) deleteBinding(p pid.PID, name string) {
	ops := []kvapi.TxnOp{
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: pidIndexKey(p, name)},
		{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: nodeIndexKey(p, name)},
	}
	if e, err := s.engine.Get(activeKey(name)); err == nil {
		if av, derr := decodeActive(e.Value); derr == nil {
			if owner, perr := pid.ParsePID(av.PID); perr == nil && owner.String() == p.String() {
				ops = append(ops,
					kvapi.TxnOp{Kind: kvapi.TxnCheck, Cond: kvapi.CondVersion, Key: activeKey(name), Expect: e.Version},
					kvapi.TxnOp{Kind: kvapi.TxnDelete, Cond: kvapi.CondAny, Key: activeKey(name)},
				)
			}
		}
	}
	if _, err := s.engine.Txn(ops); err != nil {
		s.logger.Debug("kvreg reap delete failed", zap.String("name", name), zap.Error(err))
	}
}

// IsStrongReserved reports a local Strong reservation for name. Implemented in
// the Strong-scope layer; no reservation exists at Consistent scope.
func (s *Service) IsStrongReserved(name string) (pid.PID, bool) {
	return s.strongReserved(name)
}

// NameReady reports whether the join-epoch barrier has completed.
func (s *Service) NameReady() bool {
	return s.nameReady()
}

var (
	_ globalapi.Registry = (*Service)(nil)
)
