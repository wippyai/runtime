// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"fmt"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// WatchLimits bounds the subscriptions and undelivered events retained by one
// KV backend. MaxEvents applies to each watcher (including its in-flight
// delivery); MaxBytes reserves payload and key bytes across all watchers on
// the backend. Queue metadata occupies additional bounded descriptor space.
type WatchLimits struct {
	MaxSubscriptions int
	MaxEvents        int
	MaxBytes         int64
}

// All KV backends share this one bounded watch policy. Watch delivery is
// best-effort only until these limits are reached: overflow invalidates the
// observer, which must subscribe and seed its snapshot again.
var defaultWatchLimits = WatchLimits{
	MaxSubscriptions: 128,
	MaxEvents:        256,
	MaxBytes:         32 << 20,
}

// DefaultWatchLimits returns the ordinary backend policy for local use and
// config defaults. Callers can configure an engine before creating watchers.
func DefaultWatchLimits() WatchLimits { return defaultWatchLimits }

func (limits WatchLimits) internal() watchLimits {
	return watchLimits{
		maxWatchers: limits.MaxSubscriptions,
		maxEvents:   limits.MaxEvents,
		maxBytes:    limits.MaxBytes,
	}
}

func defaultWatchSource() *watchSource {
	source, err := newWatchSource(defaultWatchLimits.internal())
	if err != nil {
		panic("kv: invalid default watch limits")
	}
	return source
}

// SetWatchLimits can run only while no subscriptions are active. Existing
// subscriptions cannot change their memory budgets partway through a feed.
func (s *watchSource) setLimits(limits WatchLimits) error {
	configured := limits.internal()
	if !validWatchLimits(configured) {
		return fmt.Errorf("kv: invalid watch limits: %+v", limits)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return kvapi.ErrKVClosed
	}
	if len(s.watches) != 0 {
		return fmt.Errorf("kv: cannot reconfigure watch limits with active subscriptions")
	}
	s.limits = configured
	return nil
}
