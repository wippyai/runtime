// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"strings"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/topology/namereg/global"
)

// StartReconciler drives the registry off the kv watch stream: active-binding
// changes feed the dissem cache (so non-members resolve names), and Strong
// pending/ack/reject changes advance the Strong state machine. No-op when
// neither dissem nor Strong is configured. The watcher stops when ctx ends.
func (s *Service) StartReconciler(ctx context.Context) error {
	if s.strong == nil && s.dissem == nil {
		return nil
	}
	w, err := s.engine.Watch(ctx, registryPrefix)
	if err != nil {
		return err
	}
	s.seed()
	go func() {
		defer func() { _ = w.Close() }()
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

// seed primes local state from the current kv snapshot: dissem cache from active
// bindings, and the Strong machine from in-flight pending reservations.
func (s *Service) seed() {
	if s.dissem != nil {
		_ = s.engine.Scan(activePrefix, func(e kvapi.Entry) bool {
			s.translateActive(strings.TrimPrefix(e.Key, activePrefix), e.Value, e.Epoch, false)
			return true
		})
	}
	if s.strong != nil {
		s.strong.reconcileAllPending()
	}
}

func (s *Service) handleWatchEvent(ev kvapi.WatchEvent) {
	key := ""
	switch {
	case ev.Current != nil:
		key = ev.Current.Key
	case ev.Previous != nil:
		key = ev.Previous.Key
	}
	switch {
	case strings.HasPrefix(key, activePrefix):
		name := strings.TrimPrefix(key, activePrefix)
		if ev.Current != nil {
			s.translateActive(name, ev.Current.Value, ev.Current.Epoch, false)
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
			s.strong.reconcileAllPending()
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

func (st *strongState) reconcileAllPending() {
	var names []string
	_ = st.svc.engine.Scan(pendingPrefix, func(e kvapi.Entry) bool {
		names = append(names, strings.TrimPrefix(e.Key, pendingPrefix))
		return true
	})
	for _, n := range names {
		st.reconcile(n)
	}
}
