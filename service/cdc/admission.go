// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"sync"

	api "github.com/wippyai/runtime/api/service/cdc"
)

// admission owns reservations, not queues. Acquiring never waits behind slow
// consumers; releasing is idempotent on every failed/open/closed stream path.
type admission struct {
	rejected                 uint64
	mu                       sync.Mutex
	limits                   api.SubscriptionLimits
	subscriptions, snapshots int
	bytes                    int64
}

func newAdmission(limits api.SubscriptionLimits) *admission {
	return &admission{limits: limits.Effective()}
}

func subscriptionLimits(source ManagedSource) api.SubscriptionLimits {
	if configured, ok := source.(interface{ SubscriptionLimits() api.SubscriptionLimits }); ok {
		return configured.SubscriptionLimits()
	}
	return api.SubscriptionLimits{}
}

func (a *admission) configure(limits api.SubscriptionLimits) {
	a.mu.Lock()
	a.limits = limits.Effective()
	a.mu.Unlock()
}

func (a *admission) acquire(bytes int64, snapshot bool) (func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if bytes < 0 || a.subscriptions >= a.limits.MaxSubscriptions || bytes > a.limits.MaxBytes-a.bytes || (snapshot && a.snapshots >= a.limits.MaxSnapshots) {
		a.rejected++
		return nil, api.ErrSubscriptionLimit
	}
	a.subscriptions++
	a.bytes += bytes
	if snapshot {
		a.snapshots++
	}
	return sync.OnceFunc(func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.subscriptions--
		a.bytes -= bytes
		if snapshot {
			a.snapshots--
		}
	}), nil
}

func (a *admission) stats() api.SubscriptionStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return api.SubscriptionStats{Active: a.subscriptions, Snapshots: a.snapshots, ReservedBytes: a.bytes, Rejected: a.rejected}
}
