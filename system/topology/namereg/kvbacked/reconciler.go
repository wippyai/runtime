// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology/namereg/global"
	"go.uber.org/zap"
)

type reconcilerLifecycle struct {
	bootstrapped       atomic.Bool // first authority snapshot completed; not transient readiness
	recover            chan struct{}
	recoveryGeneration uint64        // protected by admission
	refresh            chan struct{} // participant feed hints; nil for local-watch owners
	ctx                context.Context
	cancel             context.CancelFunc
	done               chan struct{}
	workers            sync.WaitGroup
	calls              sync.WaitGroup
	admission          sync.Mutex
	stopping           bool
	mutationsSealed    atomic.Bool
	mutationCount      int           // protected by admission
	mutationsDrained   chan struct{} // allocated only on explicit seal
}

// StartReconciler drives the registry off the kv watch stream: active-binding
// changes feed the dissem cache (so non-members resolve names), and Strong
// pending/ack/reject changes advance the Strong state machine. No-op when
// neither dissem nor Strong is configured. The watcher stops when ctx ends.
// Successful startup owns this Service for its lifetime: restarting requires a
// new Service (including fresh dissemination state). Failed startup may retry.
func (s *Service) StartReconciler(ctx context.Context) error {
	if s.strong != nil && s.strong.participants != nil {
		return fmt.Errorf("enrolled naming requires participant bootstrap")
	}
	var recovery *participantRecovery
	if s.strong != nil {
		// Membership-only registries also lose watch progress during leader changes.
		// Re-subscribe before re-reading the local replica; retain exclusions and
		// keep admission closed until seed and queued watch events reconcile. Never
		// replay the failed transaction or infer its outcome from transport errors.
		recovery = &participantRecovery{
			interval: s.strong.retryInterval,
			refresh:  func(ctx context.Context) error { return ctx.Err() },
		}
	}
	return s.startReconciler(ctx, nil, recovery)
}

// startReconciler acquires the lifecycle and subscribes before bootstrap, so
// enrollment/snapshot installation cannot leave an unobserved update window.
func (s *Service) startReconciler(ctx context.Context, bootstrap func(context.Context) error, recovery *participantRecovery) (err error) {
	if s.strong == nil && s.dissem == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.strong != nil {
		if s.nonMember != nil && s.nonMember() {
			return fmt.Errorf("naming participant without a local replica requires an authority feed")
		}
		if _, ok := s.engine.(kvapi.LocalSnapshotReader); !ok {
			return fmt.Errorf("registry reconciliation requires atomic local snapshot reads")
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	run := &reconcilerLifecycle{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	if recovery != nil {
		run.recover = make(chan struct{}, 1)
	}
	if !s.reconciler.CompareAndSwap(nil, run) {
		cancel()
		return fmt.Errorf("registry reconciler already started; use a new service after shutdown")
	}
	if s.strong != nil {
		s.strong.mu.Lock()
		s.strong.timersSealed = false
		s.strong.mu.Unlock()
	}
	defer func() {
		if err != nil {
			cancel()
			if s.strong != nil {
				_ = s.strong.stopTimers(context.Background())
			}
			run.closeAdmission()
			run.calls.Wait()
			run.workers.Wait()
			close(run.done)
			s.reconciler.CompareAndSwap(run, nil)
		}
	}()
	w, err := s.engine.Watch(ctx, registryPrefix)
	if err != nil {
		cancel()
		return err
	}
	if bootstrap != nil {
		s.ready.Store(false)
		if err := bootstrap(ctx); err != nil {
			cancel()
			_ = w.Close()
			return err
		}
	}
	if err := s.seed(); err != nil {
		cancel()
		_ = w.Close()
		return err
	}
	if err := ctx.Err(); err != nil {
		cancel()
		_ = w.Close()
		return err
	}
	// The node has now learned and latched the cluster's in-flight/active Strong
	// reservations; name-readiness can flip so cross-scope guards see them.
	run.bootstrapped.Store(true)
	s.ready.Store(true)
	if s.dissem != nil {
		run.workers.Go(s.dissem.RunGC)
	}
	if s.strong != nil {
		run.workers.Go(func() { s.leaderSweep(ctx) })
	}
	go func() {
		defer func() {
			// A stopped update stream cannot justify further cross-scope
			// admission. Stop the associated sweep even if the parent lives.
			s.ready.Store(false)
			cancel()
			if w != nil {
				_ = w.Close()
				w = nil
			}
			if s.dissem != nil {
				s.dissem.Stop()
			}
			if s.strong != nil {
				_ = s.strong.stopTimers(context.Background())
			}
			run.closeAdmission()
			run.calls.Wait()
			run.workers.Wait()
			close(run.done)
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-run.recover:
				if w != nil {
					_ = w.Close()
					w = nil
				}
				replacement, err := s.recoverParticipantWatch(ctx, recovery)
				if err != nil {
					return
				}
				w = replacement
			case ev, ok := <-w.Events():
				var syncErr error
				if !ok {
					syncErr = fmt.Errorf("registry update stream closed")
				} else {
					syncErr = s.handleWatchEvent(ev)
				}
				if syncErr != nil {
					s.ready.Store(false)
					s.logger.Error("registry synchronization failed", zap.Error(syncErr))
					if recovery == nil {
						return
					}
					if w != nil {
						_ = w.Close()
						w = nil
					}
					replacement, err := s.recoverParticipantWatch(ctx, recovery)
					if err != nil {
						return
					}
					w = replacement
				}
			}
		}
	}()
	return nil
}

// leaderSweep periodically re-drives every in-flight pending while this node is
// the leader. It re-arms deadline timers and resumes promotion/expiry after a
// leadership change (a new leader has no timers for pendings opened under the
// old one) and backstops any missed watch event.
func (s *Service) leaderSweep(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.leaderFn() {
				if err := s.strong.reconcileAllPending(); err != nil {
					s.logger.Debug("registry pending scan failed", zap.Error(err))
				}
				if err := s.strong.reclaimStrongResults(ctx); err != nil {
					s.logger.Debug("registry result reclamation failed", zap.Error(err))
				}
			}
		}
	}
}

// seed primes local state from the current kv snapshot: dissem cache from active
// bindings, and the Strong machine from in-flight pending reservations.
func (s *Service) seed() error {
	if s.strong != nil {
		if err := s.strong.reconcileAllPending(); err != nil {
			return err
		}
	}
	// Scan active records once for both consumers. A skipped malformed
	// record cannot justify opening cross-scope admission after startup.
	var recordErr error
	err := s.engine.Scan(activePrefix, func(e kvapi.Entry) bool {
		active, err := decodeActive(e.Value)
		if err != nil {
			recordErr = fmt.Errorf("registry record %q: %w", e.Key, err)
			return false
		}
		if recordErr = validateNamingRecord(e.Key, activePrefix, active.Name, active.PID); recordErr != nil {
			return false
		}
		name := strings.TrimPrefix(e.Key, activePrefix)
		if s.dissem != nil {
			s.translateActive(name, e.Value, e.Epoch, false)
		}
		if s.strong != nil && active.Strong {
			if err := s.strong.reconcile(name); err != nil {
				recordErr = err
				return false
			}
		}
		return true
	})
	if err != nil {
		return err
	}
	return recordErr
}

func validateNamingRecord(key, prefix, name, owner string) error {
	if key != prefix+name {
		return fmt.Errorf("registry record %q: name mismatch", key)
	}
	if _, err := pid.ParsePID(owner); err != nil {
		return fmt.Errorf("registry record %q: invalid owner: %w", key, err)
	}
	return nil
}

func (s *Service) handleWatchEvent(ev kvapi.WatchEvent) error {
	key := ""
	switch {
	case ev.Current != nil:
		key = ev.Current.Key
	case ev.Previous != nil:
		key = ev.Previous.Key
	}
	// Registry keys are never lease-bound, so a WatchExpired here is a design
	// violation. It is still handled safely below (as a delete); surface it.
	if ev.Type == kvapi.WatchExpired && strings.HasPrefix(key, registryPrefix) {
		s.logger.Debug("registry key expired via lease (unexpected)", zap.String("key", key))
	}
	switch {
	case strings.HasPrefix(key, activePrefix):
		name := strings.TrimPrefix(key, activePrefix)
		if ev.Current != nil {
			// Dot is the op's raft index (ev.Index), authoritative regardless of
			// whether the snapshot Entry carries Epoch.
			s.translateActive(name, ev.Current.Value, ev.Index, false)
		} else {
			s.translateActive(name, nil, ev.Index, true)
		}
		if s.strong != nil {
			return s.strong.reconcile(name)
		}
	case strings.HasPrefix(key, pendingPrefix):
		if s.strong != nil {
			return s.strong.reconcile(strings.TrimPrefix(key, pendingPrefix))
		}
	case strings.HasPrefix(key, participantAckPrefix), strings.HasPrefix(key, participantRejectPrefix):
		if s.strong != nil {
			if name, ok := participantVoteName(key); ok {
				return s.strong.reconcile(name)
			}
		}
	case strings.HasPrefix(key, ackPrefix), strings.HasPrefix(key, rejectPrefix):
		if s.strong != nil {
			if err := s.strong.reconcileAllPending(); err != nil {
				return err
			}
		}
	}
	return nil
}

// translateActive feeds one active-binding change into the dissem plane: the
// leader broadcasts it to the gossip mesh; followers seed their local cache. The
// dot is the raft index (Entry.Epoch on a put, the delete's apply Index on a
// tombstone), monotonic per-name so the cache converges.
func (s *Service) translateActive(name string, value []byte, raftIndex uint64, deleted bool) {
	if s.dissem == nil {
		return
	}
	ev := global.BindingEvent{Name: name, RaftIndex: raftIndex, Deleted: deleted}
	if !deleted {
		av, err := decodeActive(value)
		if err != nil {
			return
		}
		p, perr := pid.ParsePID(av.PID)
		if perr != nil {
			return
		}
		ev.PID = p
	}
	if s.leaderFn() {
		s.dissem.LeaderBroadcast(ev)
	} else {
		s.dissem.LocalApply(ev)
	}
}

func (st *strongState) reconcileAllPending() error {
	var names []string
	var recordErr error
	if err := st.svc.engine.Scan(pendingPrefix, func(e kvapi.Entry) bool {
		header, err := decodePending(e.Value)
		if err != nil {
			recordErr = fmt.Errorf("registry record %q: %w", e.Key, err)
			return false
		}
		if recordErr = validateNamingRecord(e.Key, pendingPrefix, header.Name, header.PID); recordErr != nil {
			return false
		}
		names = append(names, strings.TrimPrefix(e.Key, pendingPrefix))
		return true
	}); err != nil {
		return err
	}
	if recordErr != nil {
		return recordErr
	}
	for _, n := range names {
		if err := st.reconcile(n); err != nil {
			return err
		}
	}
	return nil
}

// reconcileContext is the lifetime of the current startup/watch owner. Direct
// standalone calls have no reconciler lifetime and use their existing behavior.
func (s *Service) reconcileContext() context.Context {
	if run := s.reconciler.Load(); run != nil {
		return run.ctx
	}
	return context.Background()
}

// StopReconciler closes admission, cancels the watch owner, and joins its workers
// and timer callbacks. A context deadline bounds only the wait; it does not
// report completion or release ownership. Call again to finish a canceled wait.
func (s *Service) StopReconciler(ctx context.Context) error {
	run := s.reconciler.Load()
	if run == nil {
		return nil
	}
	s.ready.Store(false)
	run.closeAdmission()
	select {
	case <-run.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (run *reconcilerLifecycle) closeAdmission() {
	run.admission.Lock()
	run.stopping = true
	run.cancel()
	run.admission.Unlock()
}

// admitMutation joins direct mutation calls to the current owner.
// Standalone services preserve their existing caller-owned lifetime.
func (s *Service) admitMutation(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	run := s.reconciler.Load()
	if run == nil {
		// Configured cluster participants cannot fall back to standalone writes
		// before enrollment, or after bootstrap failed and released its owner.
		if s.strong != nil && s.strong.participants != nil {
			return nil, globalapi.ErrNotReady
		}
		return func() {}, nil
	}
	run.admission.Lock()
	defer run.admission.Unlock()
	if s.strong != nil && s.strong.participants != nil && !run.bootstrapped.Load() {
		return nil, globalapi.ErrNotReady
	}
	if run.stopping || run.mutationsSealed.Load() || run.ctx.Err() != nil {
		return nil, context.Canceled
	}
	run.calls.Add(1)
	run.mutationCount++
	return func() {
		run.admission.Lock()
		run.mutationCount--
		run.calls.Done()
		if run.mutationCount == 0 && run.mutationsDrained != nil {
			close(run.mutationsDrained)
		}
		run.admission.Unlock()
	}, nil
}
