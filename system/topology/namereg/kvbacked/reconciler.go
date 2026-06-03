// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"strings"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// StartReconciler drives the Strong-scope state machine off the kv watch stream:
// every node reconciles a name when its pending/active/ack/reject keys change, so
// required nodes attest and the leader promotes or expires. No-op when Strong is
// not configured. The watcher stops when ctx is canceled.
func (s *Service) StartReconciler(ctx context.Context) error {
	if s.strong == nil {
		return nil
	}
	w, err := s.engine.Watch(ctx, registryPrefix)
	if err != nil {
		return err
	}
	s.strong.reconcileAllPending()
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
				s.strong.onWatchEvent(ev)
			}
		}
	}()
	return nil
}

func (st *strongState) onWatchEvent(ev kvapi.WatchEvent) {
	key := ""
	switch {
	case ev.Current != nil:
		key = ev.Current.Key
	case ev.Previous != nil:
		key = ev.Previous.Key
	}
	switch {
	case strings.HasPrefix(key, activePrefix):
		st.reconcile(strings.TrimPrefix(key, activePrefix))
	case strings.HasPrefix(key, pendingPrefix):
		st.reconcile(strings.TrimPrefix(key, pendingPrefix))
	case strings.HasPrefix(key, ackPrefix), strings.HasPrefix(key, rejectPrefix):
		st.reconcileAllPending()
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
