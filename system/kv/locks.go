// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"errors"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// LockHostID is the relay host the lock service registers to receive holder exit
// events for auto-release.
const LockHostID pid.HostID = "syslock"

// lockPrefix is the reserved system namespace for distributed locks in the kv.
const lockPrefix = "_sys:lock:"

var lockServiceKey = &ctxapi.Key{Name: "kv.lock.service"}

// LockService implements distributed locks over the shared kv: a lock is a
// SetIfAbsent of the holder PID at _sys:lock:<name> (linearizable via raft +
// leader-forwarding). It auto-releases a holder's locks when the holder process
// exits (topology monitor) or its node leaves (cluster.NodeLeft -> ReapNode).
type LockService struct {
	engine    kvapi.Engine
	topo      topology.Topology
	logger    *zap.Logger
	self      pid.PID
	monitored sync.Map // holder pid string -> struct{}
}

// NewLockService builds the service. topo may be nil to disable process-exit
// monitoring (node-leave reaping still works via ReapNode).
func NewLockService(engine kvapi.Engine, topo topology.Topology, localNode string, logger *zap.Logger) *LockService {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &LockService{
		engine: engine,
		topo:   topo,
		logger: logger.Named("kv-lock"),
		self:   pid.PID{Node: localNode, Host: LockHostID},
	}
}

func lockKey(name string) string { return lockPrefix + name }

// Acquire takes the lock for holder if free; returns true when acquired.
func (s *LockService) Acquire(name string, holder pid.PID) (bool, error) {
	_, stored, err := s.engine.SetIfAbsent(lockKey(name), []byte(holder.String()))
	if err != nil {
		return false, err
	}
	if stored {
		s.monitor(holder)
	}
	return stored, nil
}

// Release frees the lock iff held by holder; returns true when released.
func (s *LockService) Release(name string, holder pid.PID) (bool, error) {
	e, err := s.engine.Get(lockKey(name))
	if err != nil {
		if errors.Is(err, kvapi.ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}
	if string(e.Value) != holder.String() {
		return false, nil
	}
	if err := s.engine.Delete(lockKey(name)); err != nil {
		if errors.Is(err, kvapi.ErrKeyNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Holder returns the current lock holder, or (zero, false) if free.
func (s *LockService) Holder(name string) (pid.PID, bool, error) {
	e, err := s.engine.Get(lockKey(name))
	if err != nil {
		if errors.Is(err, kvapi.ErrKeyNotFound) {
			return pid.PID{}, false, nil
		}
		return pid.PID{}, false, err
	}
	p, perr := pid.ParsePID(string(e.Value))
	if perr != nil {
		return pid.PID{}, false, nil
	}
	return p, true, nil
}

func (s *LockService) monitor(holder pid.PID) {
	if s.topo == nil {
		return
	}
	key := holder.String()
	if _, loaded := s.monitored.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	if err := s.topo.Monitor(s.self, holder); err != nil {
		s.monitored.Delete(key)
		s.logger.Debug("lock monitor failed", zap.String("holder", key), zap.Error(err))
	}
}

// ReapPID releases every lock held by p (called on holder process exit).
// PIDs are compared by String() because pid.PID carries an unexported cached
// string that makes struct == unreliable.
func (s *LockService) ReapPID(p pid.PID) {
	want := p.String()
	s.monitored.Delete(want)
	s.reap(func(h pid.PID) bool { return h.String() == want })
}

// ReapNode releases every lock held by a PID on the departed node.
func (s *LockService) ReapNode(node pid.NodeID) {
	s.reap(func(h pid.PID) bool { return h.Node == node })
}

// reap deletes every lock whose holder matches. Idempotent: a concurrent reap on
// another node that already deleted the key is a harmless ErrKeyNotFound.
func (s *LockService) reap(match func(pid.PID) bool) {
	var victims []string
	_ = s.engine.Scan(lockPrefix, func(e kvapi.Entry) bool {
		if p, err := pid.ParsePID(string(e.Value)); err == nil && match(p) {
			victims = append(victims, e.Key)
		}
		return true
	})
	for _, k := range victims {
		if err := s.engine.Delete(k); err != nil && !errors.Is(err, kvapi.ErrKeyNotFound) {
			s.logger.Debug("lock reap delete failed", zap.String("key", k), zap.Error(err))
		}
	}
}

// Send implements relay.Receiver: it reaps locks of holders whose exit events
// arrive on the topology events topic.
func (s *LockService) Send(pkg *relay.Package) error {
	defer relay.ReleasePackage(pkg)
	for _, msg := range pkg.Messages {
		if msg.Topic != topology.TopicEvents {
			continue
		}
		for _, p := range msg.Payloads {
			if ev, ok := p.Data().(*topology.ExitEvent); ok {
				s.ReapPID(ev.From)
			}
		}
	}
	return nil
}

// WithLockService stores the lock service in the app context.
func WithLockService(ctx context.Context, ls *LockService) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(lockServiceKey) == nil {
		ac.With(lockServiceKey, ls)
	}
	return ctx
}

// GetLockService retrieves the lock service from the context, or nil.
func GetLockService(ctx context.Context) *LockService {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if v := ac.Get(lockServiceKey); v != nil {
		if ls, ok := v.(*LockService); ok {
			return ls
		}
	}
	return nil
}

var _ relay.Receiver = (*LockService)(nil)
