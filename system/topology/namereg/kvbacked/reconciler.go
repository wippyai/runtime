// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/topology/namereg/global"
	"go.uber.org/zap"
)

type reconcilerLifecycle struct{ ctx context.Context }

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
	ctx, cancel := context.WithCancel(ctx)
	run := &reconcilerLifecycle{ctx: ctx}
	if !s.reconciler.CompareAndSwap(nil, run) {
		cancel()
		return fmt.Errorf("registry reconciler already started; use a new service after shutdown")
	}
	defer func() {
		if err != nil {
			cancel()
			s.reconciler.CompareAndSwap(run, nil)
		}
	}()
	w, err := s.engine.Watch(ctx, registryPrefix)
	if err != nil {
		cancel()
		return err
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
	s.ready.Store(true)
	if s.dissem != nil {
		go s.dissem.RunGC()
	}
	if s.strong != nil {
		go s.leaderSweep(ctx)
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
			case ev, ok := <-w.Events():
				if !ok {
					return
				}
				s.handleWatchEvent(ev)
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
			s.strong.reconcile(name)
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

func (s *Service) handleWatchEvent(ev kvapi.WatchEvent) {
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
			s.strong.reconcile(name)
		}
	case strings.HasPrefix(key, pendingPrefix):
		if s.strong != nil {
			s.strong.reconcile(strings.TrimPrefix(key, pendingPrefix))
		}
	case strings.HasPrefix(key, ackPrefix), strings.HasPrefix(key, rejectPrefix):
		if s.strong != nil {
			if err := s.strong.reconcileAllPending(); err != nil {
				s.logger.Debug("registry pending scan failed", zap.Error(err))
			}
		}
	}
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
		st.reconcile(n)
	}
	return nil
}
