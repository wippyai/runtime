// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/topology/namereg/global"
	"go.uber.org/zap"
)

type reconcilerLifecycle struct {
	ctx    context.Context
	cancel context.CancelFunc
	watch  atomic.Pointer[reconcilerWatch]
	failed atomic.Bool
}

type reconcilerWatch struct{ kvapi.Watcher }

// StartReconciler drives the registry off the kv watch stream: active-binding
// changes feed the dissem cache (so non-members resolve names), and Strong
// pending/ack/reject changes advance the Strong state machine. No-op when
// neither dissem nor Strong is configured. The watcher stops when ctx ends.
// Successful startup owns this Service for its lifetime: restarting requires a
// new Service (including fresh dissemination state). Failed startup may retry.
func (s *Service) StartReconciler(ctx context.Context) (err error) {
	if s.strong == nil && s.dissem == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.strong != nil && s.nonMember != nil && s.nonMember() {
		return fmt.Errorf("naming participant without a local replica requires an authority feed")
	}
	if s.strong != nil {
		if _, ok := s.engine.(kvapi.LocalSnapshotReader); !ok {
			return fmt.Errorf("registry reconciliation requires atomic local snapshot reads")
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	run := &reconcilerLifecycle{ctx: ctx, cancel: cancel}
	s.reconcilerMu.Lock()
	installed := s.reconciler.CompareAndSwap(nil, run)
	s.reconcilerMu.Unlock()
	if !installed {
		cancel()
		return fmt.Errorf("registry reconciler already started; use a new service after shutdown")
	}
	defer func() {
		if err != nil {
			s.reconcilerMu.Lock()
			cancel()
			s.reconciler.CompareAndSwap(run, nil)
			s.reconcilerMu.Unlock()
		}
	}()
	w, err := s.engine.Watch(ctx, registryPrefix)
	if err != nil {
		cancel()
		return err
	}
	run.watch.Store(&reconcilerWatch{Watcher: w})
	// The delivery worker may be blocked in a snapshot read. Watch validity
	// must close admission independently of that worker's next receive.
	go func() {
		select {
		case <-w.Done():
			watchErr := w.Err()
			if watchErr == nil {
				watchErr = kvapi.ErrWatchClosed
			}
			s.failReconciler(run, watchErr)
		case <-ctx.Done():
		}
		if watchErr := w.Err(); errors.Is(watchErr, kvapi.ErrWatchOverflow) || errors.Is(watchErr, kvapi.ErrWatchReset) {
			s.logger.Error("registry watch invalidated; naming reconciliation stopped; restart required", zap.Error(watchErr))
		}
	}()
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
	select {
	case <-w.Done():
		cancel()
		_ = w.Close()
		return fmt.Errorf("registry watch invalid during seed: %w", w.Err())
	default:
	}
	// The node has now learned and latched the cluster's in-flight/active Strong
	// reservations; name-readiness can flip so cross-scope guards see them.
	s.ready.Store(true)
	if s.dissem != nil {
		go s.dissem.RunGC()
	}
	if s.strong != nil {
		go s.leaderSweep(run)
	}
	go func() {
		defer func() {
			// A stopped update stream cannot justify further cross-scope
			// admission. Stop the associated sweep even if the parent lives.
			s.ready.Store(false)
			cancel()
			_ = w.Close()
		}()
		if s.dissem != nil {
			defer s.dissem.Stop()
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.Done():
				return
			case ev, ok := <-w.Events():
				if !ok {
					return
				}
				select {
				case <-w.Done():
					return
				default:
				}
				if ctx.Err() != nil {
					return
				}
				if err := s.handleWatchEvent(ev); err != nil {
					s.failReconciler(run, err)
					s.logger.Error("registry synchronization failed", zap.Error(err))
					return
				}
			}
		}
	}()
	return nil
}

// failReconciler closes admission when a required local observation fails.
// The owner context also releases in-flight Strong waiters and stops both the
// watch and sweep workers. A service without an active reconciler remains
// usable in direct/unit-test mode.
func (s *Service) failReconciler(run *reconcilerLifecycle, err error) {
	if err == nil || run == nil {
		return
	}
	s.reconcilerMu.Lock()
	defer s.reconcilerMu.Unlock()
	if s.reconciler.Load() != run || run.ctx.Err() != nil || !run.failed.CompareAndSwap(false, true) {
		return
	}
	s.ready.Store(false)
	run.cancel()
}

// leaderSweep periodically re-drives every in-flight pending while this node is
// the leader. It re-arms deadline timers and resumes promotion/expiry after a
// leadership change (a new leader has no timers for pendings opened under the
// old one) and backstops any missed watch event.
func (s *Service) leaderSweep(run *reconcilerLifecycle) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-run.ctx.Done():
			return
		case <-t.C:
			if run.ctx.Err() != nil {
				return
			}
			if s.leaderFn() {
				if err := s.strong.reconcileAllPending(); err != nil {
					s.failReconciler(run, err)
					s.logger.Error("registry pending scan failed", zap.Error(err))
					return
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

func (s *Service) reconcileContext() context.Context {
	if run := s.reconciler.Load(); run != nil {
		return run.ctx
	}
	return context.Background()
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
			attemptID := ""
			if ev.Current == nil && ev.Previous != nil {
				if previous, err := decodeActive(ev.Previous.Value); err == nil && previous.Strong {
					attemptID = previous.AttemptID
				}
			}
			return s.strong.reconcileDeleted(name, attemptID, false)
		}
	case strings.HasPrefix(key, pendingPrefix):
		if s.strong != nil {
			name := strings.TrimPrefix(key, pendingPrefix)
			attemptID := ""
			if ev.Current == nil && ev.Previous != nil {
				if previous, err := decodePending(ev.Previous.Value); err == nil {
					attemptID = previous.AttemptID
				}
			}
			return s.strong.reconcileDeleted(name, attemptID, true)
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
