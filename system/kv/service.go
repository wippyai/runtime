// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
	"go.uber.org/zap"
)

// action is a serialized operation submitted to the event loop.
type action func()

// Service implements kvapi.Engine using an in-memory store with a single-goroutine
// event loop for serialized writes and atomic snapshot pointer for lock-free reads.
type Service struct {
	state    *state
	leases   *leaseManager
	watch    *watchSource
	logger   *zap.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	actions  chan action
	snap     atomic.Pointer[stateSnapshot]
	name     string
	outbox   []watchRecord
	leaseSeq uint64
	wg       sync.WaitGroup
	dirty    bool
}

// NewService creates a new in-memory KV service.
func NewService(name string, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		name:    name,
		watch:   defaultWatchSource(),
		logger:  logger.Named("kv").Named(name),
		state:   newState(),
		leases:  newLeaseManager(),
		actions: make(chan action, 256),
	}
}

// SetWatchLimits configures bounded delivery while no watchers are active.
func (s *Service) SetWatchLimits(limits WatchLimits) error {
	return s.watch.setLimits(limits)
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
	s.watch.close()
	s.cancel()
	s.wg.Wait()
	s.snap.Store(nil)
	s.logger.Info("kv service stopped")
	return nil
}

// eventLoop processes all actions sequentially and manages lease expiry.
func (s *Service) eventLoop() {
	defer s.wg.Done()
	defer s.watch.close()

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
			s.flush()
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
		err := fn()
		s.flush()
		done <- err
	})
	select {
	case err := <-done:
		return err
	case <-s.ctx.Done():
		// Cancellation does not interrupt the event loop's active action.
		// Join it before callers can read results captured by that action.
		s.wg.Wait()
		select {
		case err := <-done:
			return err
		default:
		}
		return kvapi.ErrKVClosed
	}
}

// publishSnapshot marks a complete action for atomic publication with its
// buffered notifications. The event loop flushes after all mutations finish.
func (s *Service) publishSnapshot() {
	s.dirty = true
}

func (s *Service) flush() {
	if !s.dirty {
		return
	}
	// Build the immutable view under the event-loop writer ownership. Hold
	// the watcher registration gate only for its pointer publication and
	// notifications, so subscriptions do not wait on snapshot construction.
	published := s.state.snapshot()
	s.watch.publishRecords(s.outbox, func() { s.snap.Store(published) })
	if len(s.outbox) > defaultWatchLimits.MaxEvents {
		// A rare huge transaction should not pin its staging array for the
		// entire service lifetime. Regular writes reuse their small buffer.
		s.outbox = nil
	} else {
		clear(s.outbox)
		s.outbox = s.outbox[:0]
	}
	s.dirty = false
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

// ScanAtIndex scans and returns the publication revision as the as-of index.
// This revision can advance on a deletion without changing Entry.Version.
func (s *Service) ScanAtIndex(prefix string, fn func(kvapi.Entry) bool) (uint64, error) {
	snap := s.snap.Load()
	if snap == nil {
		return 0, kvapi.ErrKVClosed
	}
	snap.scan(prefix, fn)
	return snap.version, nil
}

// --- kvapi.Engine write operations (serialized through event loop) ---

func (s *Service) Set(key string, value []byte) (kvapi.Version, error) {
	var ver kvapi.Version
	err := s.submitAndWait(func() error {
		prev, v := s.state.set(key, value, "")
		ver = v
		s.publishSnapshot()
		s.emitPut(key, prev)
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
		s.publishSnapshot()
		s.emitEvent(kvapi.WatchDelete, nil, prev)
		return nil
	})
}

func (s *Service) SetIfAbsent(key string, value []byte) (kvapi.Version, bool, error) {
	var ver kvapi.Version
	var ok bool
	err := s.submitAndWait(func() error {
		ver, ok = s.state.setIfAbsent(key, value, "")
		if ok {
			s.publishSnapshot()
			s.emitPut(key, nil)
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
			s.publishSnapshot()
			s.emitPut(key, prev)
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
			s.publishSnapshot()
			s.emitEvent(kvapi.WatchDelete, nil, prev)
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
		s.publishSnapshot()
		s.emitPut(key, prev)
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
			s.publishSnapshot()
			s.emitPut(key, nil)
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

// --- Watch ---

func (s *Service) Watch(ctx context.Context, prefix string) (kvapi.Watcher, error) {
	return s.watch.watch(ctx, prefix, nil)
}

// emitPut emits a WatchPut event for a key that was just written.
func (s *Service) emitPut(key string, prev *entry) {
	current := s.state.get(key)
	if current == nil {
		return
	}
	s.emitEvent(kvapi.WatchPut, current, prev)
}

// emitEvent records a watch event for this action's complete publication.
func (s *Service) emitEvent(typ kvapi.WatchEventType, current, prev *entry) {
	s.outbox = append(s.outbox, watchRecord{typ: typ, current: current, previous: prev})
}

// Verify interface compliance.
var (
	_ kvapi.Engine       = (*Service)(nil)
	_ LinearizableEngine = (*Service)(nil)
)
