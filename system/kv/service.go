// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/event"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"go.uber.org/zap"
)

// action is a serialized operation submitted to the event loop.
type action func()

// Service implements kvapi.Engine using an in-memory store with a single-goroutine
// event loop for serialized writes and atomic snapshot pointer for lock-free reads.
type Service struct {
	bus      event.Bus
	state    *state
	leases   *leaseManager
	logger   *zap.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	actions  chan action
	snap     atomic.Pointer[stateSnapshot]
	name     string // scope name, used as event.System for watch
	leaseSeq uint64
	wg       sync.WaitGroup
}

// NewService creates a new in-memory KV service.
func NewService(name string, bus event.Bus, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		name:    name,
		bus:     bus,
		logger:  logger.Named("kv").Named(name),
		state:   newState(),
		leases:  newLeaseManager(),
		actions: make(chan action, 256),
	}
}

// Start begins the event loop.
func (s *Service) Start(ctx context.Context) (<-chan any, error) {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.snap.Store(s.state.snapshot())

	s.wg.Add(1)
	go s.eventLoop()

	s.logger.Info("kv service started")
	return nil, nil //nolint:nilnil
}

// Stop shuts down the service.
func (s *Service) Stop(_ context.Context) error {
	s.logger.Info("kv service stopping")
	s.cancel()
	s.wg.Wait()
	s.snap.Store(nil)
	s.logger.Info("kv service stopped")
	return nil
}

// eventLoop processes all actions sequentially and manages lease expiry.
func (s *Service) eventLoop() {
	defer s.wg.Done()

	var leaseTimer *time.Timer
	var leaseC <-chan time.Time

	resetLeaseTimer := func() {
		if leaseTimer != nil {
			leaseTimer.Stop()
		}
		if next, ok := s.leases.nextExpiry(); ok {
			d := time.Until(next)
			if d < 0 {
				d = 0
			}
			leaseTimer = time.NewTimer(d)
			leaseC = leaseTimer.C
		} else {
			leaseTimer = nil
			leaseC = nil
		}
	}

	resetLeaseTimer()

	for {
		select {
		case <-s.ctx.Done():
			if leaseTimer != nil {
				leaseTimer.Stop()
			}
			return

		case fn, ok := <-s.actions:
			if !ok {
				return
			}
			fn()
			resetLeaseTimer()

		case <-leaseC:
			s.processExpiredLeases()
			resetLeaseTimer()
		}
	}
}

// processExpiredLeases handles all leases that have expired.
func (s *Service) processExpiredLeases() {
	now := time.Now()
	expired := s.leases.expired(now)
	if len(expired) == 0 {
		return
	}

	for _, leaseID := range expired {
		keys := s.state.removeLease(leaseID)
		for _, key := range keys {
			prev := s.state.del(key)
			if prev != nil {
				s.emitEvent(kvapi.WatchExpired, nil, prev)
			}
		}
		if handle, ok := s.leases.handles[leaseID]; ok {
			handle.close()
			delete(s.leases.handles, leaseID)
		}
	}

	s.publishSnapshot()
	s.logger.Debug("leases expired", zap.Int("count", len(expired)))
}

// submit sends an action to the event loop.
func (s *Service) submit(fn action) {
	select {
	case s.actions <- fn:
	case <-s.ctx.Done():
	}
}

// submitAndWait sends an action and blocks until it completes.
func (s *Service) submitAndWait(fn func() error) error {
	done := make(chan error, 1)
	s.submit(func() {
		done <- fn()
	})
	select {
	case err := <-done:
		return err
	case <-s.ctx.Done():
		return kvapi.ErrKVClosed
	}
}

// publishSnapshot rebuilds the atomic snapshot after a state mutation.
func (s *Service) publishSnapshot() {
	s.snap.Store(s.state.snapshot())
}

// --- kvapi.Engine read operations (lock-free, from snapshot) ---

func (s *Service) Get(key string) (kvapi.Entry, error) {
	snap := s.snap.Load()
	if snap == nil {
		return kvapi.Entry{}, kvapi.ErrKVClosed
	}
	e := snap.get(key)
	if e == nil {
		return kvapi.Entry{}, kvapi.ErrKeyNotFound
	}
	return *e, nil
}

func (s *Service) Scan(prefix string, fn func(kvapi.Entry) bool) error {
	snap := s.snap.Load()
	if snap == nil {
		return kvapi.ErrKVClosed
	}
	snap.scan(prefix, fn)
	return nil
}

// GetLinearizable is a plain Get on the single-node in-memory engine, which is
// trivially linearizable. Present so Service satisfies LinearizableEngine.
func (s *Service) GetLinearizable(key string) (kvapi.Entry, error) { return s.Get(key) }

// ScanAtIndex scans and returns the current global version as the as-of index.
func (s *Service) ScanAtIndex(prefix string, fn func(kvapi.Entry) bool) (uint64, error) {
	if err := s.Scan(prefix, fn); err != nil {
		return 0, err
	}
	var idx uint64
	err := s.submitAndWait(func() error { idx = s.state.version; return nil })
	return idx, err
}

// --- kvapi.Engine write operations (serialized through event loop) ---

func (s *Service) Set(key string, value []byte) (kvapi.Version, error) {
	var ver kvapi.Version
	err := s.submitAndWait(func() error {
		prev, v := s.state.set(key, value, "")
		ver = v
		s.emitPut(key, prev)
		s.publishSnapshot()
		return nil
	})
	return ver, err
}

func (s *Service) Delete(key string) error {
	return s.submitAndWait(func() error {
		prev := s.state.del(key)
		if prev == nil {
			return kvapi.ErrKeyNotFound
		}
		s.emitEvent(kvapi.WatchDelete, nil, prev)
		s.publishSnapshot()
		return nil
	})
}

func (s *Service) SetIfAbsent(key string, value []byte) (kvapi.Version, bool, error) {
	var ver kvapi.Version
	var ok bool
	err := s.submitAndWait(func() error {
		ver, ok = s.state.setIfAbsent(key, value, "")
		if ok {
			s.emitPut(key, nil)
			s.publishSnapshot()
		}
		return nil
	})
	return ver, ok, err
}

func (s *Service) CompareAndSwap(key string, expect kvapi.Version, value []byte) (kvapi.Version, bool, error) {
	var ver kvapi.Version
	var ok bool
	err := s.submitAndWait(func() error {
		prev := s.state.get(key)
		ver, ok = s.state.cas(key, expect, value)
		if ok {
			s.emitPut(key, prev)
			s.publishSnapshot()
		}
		return nil
	})
	return ver, ok, err
}

func (s *Service) CompareAndDelete(key string, expect kvapi.Version) (bool, error) {
	var deleted bool
	err := s.submitAndWait(func() error {
		prev := s.state.get(key)
		deleted, _ = s.state.compareAndDelete(key, expect)
		if deleted {
			s.emitEvent(kvapi.WatchDelete, nil, prev)
			s.publishSnapshot()
		}
		return nil
	})
	return deleted, err
}

func (s *Service) Txn(ops []kvapi.TxnOp) (bool, error) {
	var committed bool
	err := s.submitAndWait(func() error {
		for _, op := range ops {
			if !condHolds(op.Cond, op.Expect, s.state.get(op.Key)) {
				return nil
			}
		}
		for _, op := range ops {
			switch op.Kind {
			case kvapi.TxnPut:
				prev := s.state.get(op.Key)
				s.state.set(op.Key, op.Value, "")
				s.emitPut(op.Key, prev)
			case kvapi.TxnDelete:
				if prev := s.state.del(op.Key); prev != nil {
					s.emitEvent(kvapi.WatchDelete, nil, prev)
				}
			case kvapi.TxnCheck:
			}
		}
		committed = true
		s.publishSnapshot()
		return nil
	})
	return committed, err
}

func (s *Service) SetWithLease(key string, value []byte, leaseID kvapi.LeaseID) (kvapi.Version, error) {
	var ver kvapi.Version
	err := s.submitAndWait(func() error {
		if _, ok := s.leases.getHandle(leaseID); !ok {
			return kvapi.ErrLeaseNotFound
		}
		prev, v := s.state.set(key, value, leaseID)
		ver = v
		s.emitPut(key, prev)
		s.publishSnapshot()
		return nil
	})
	return ver, err
}

func (s *Service) SetIfAbsentWithLease(key string, value []byte, leaseID kvapi.LeaseID) (kvapi.Version, bool, error) {
	var ver kvapi.Version
	var ok bool
	err := s.submitAndWait(func() error {
		if _, exists := s.leases.getHandle(leaseID); !exists {
			return kvapi.ErrLeaseNotFound
		}
		ver, ok = s.state.setIfAbsent(key, value, leaseID)
		if ok {
			s.emitPut(key, nil)
			s.publishSnapshot()
		}
		return nil
	})
	return ver, ok, err
}

// --- Lease operations ---

func (s *Service) GrantLease(_ context.Context, ttl time.Duration) (kvapi.Lease, error) {
	var handle *lease
	err := s.submitAndWait(func() error {
		s.leaseSeq++
		id := kvapi.LeaseID(fmt.Sprintf("%s-lease-%d", s.name, s.leaseSeq))
		s.state.addLease(id, ttl.Milliseconds(), time.Now().Add(ttl).UnixMilli())
		handle = s.leases.grant(id, ttl, time.Now())
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Wire up KeepAlive and Revoke to go through the event loop
	handle.keepAlive = func(_ context.Context) error {
		return s.submitAndWait(func() error {
			if !s.leases.renew(handle.id, time.Now()) {
				return kvapi.ErrLeaseNotFound
			}
			return nil
		})
	}
	handle.revoke = func(_ context.Context) error {
		return s.submitAndWait(func() error {
			keys := s.state.removeLease(handle.id)
			for _, key := range keys {
				prev := s.state.del(key)
				if prev != nil {
					s.emitEvent(kvapi.WatchExpired, nil, prev)
				}
			}
			s.leases.revoke(handle.id)
			s.publishSnapshot()
			return nil
		})
	}

	return handle, nil
}

// --- Watch (via event bus) ---

func (s *Service) Watch(ctx context.Context, prefix string) (kvapi.Watcher, error) {
	if s.bus == nil {
		return nil, fmt.Errorf("kv: event bus not available")
	}
	return newWatcher(ctx, s.bus, s.eventSystem(), prefix)
}

// eventSystem returns the event.System identifier for this KV instance.
func (s *Service) eventSystem() event.System {
	return "kv:" + s.name
}

// emitPut emits a WatchPut event for a key that was just written.
func (s *Service) emitPut(key string, prev *entry) {
	current := s.state.get(key)
	if current == nil {
		return
	}
	cur := entryToCluster(current)
	s.emitEvent(kvapi.WatchPut, cur, prev)
}

// emitEvent publishes a watch event to the event bus.
func (s *Service) emitEvent(typ kvapi.WatchEventType, current *kvapi.Entry, prev *entry) {
	if s.bus == nil {
		return
	}

	evt := kvapi.WatchEvent{
		Type:    typ,
		Current: current,
	}
	if prev != nil {
		p := entryToCluster(prev)
		evt.Previous = p
	}

	key := ""
	if current != nil {
		key = current.Key
	} else if prev != nil {
		key = prev.key
	}

	s.bus.Send(s.ctx, event.Event{
		System: s.eventSystem(),
		Kind:   key,
		Data:   evt,
	})
}

func entryToCluster(e *entry) *kvapi.Entry {
	if e == nil {
		return nil
	}
	return &kvapi.Entry{
		Key:     e.key,
		Value:   e.value,
		Version: e.version,
		LeaseID: e.leaseID,
		Epoch:   e.epoch,
	}
}

// Verify interface compliance.
var (
	_ kvapi.Engine       = (*Service)(nil)
	_ LinearizableEngine = (*Service)(nil)
)
