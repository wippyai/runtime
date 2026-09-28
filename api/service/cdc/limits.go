// SPDX-License-Identifier: MPL-2.0

package cdc

// SubscriptionStats describes admission reservations, not RSS or queue fill.
type SubscriptionStats struct {
	Active        int    `json:"active"`
	Snapshots     int    `json:"snapshots"`
	ReservedBytes int64  `json:"reserved_bytes"`
	Rejected      uint64 `json:"rejected"`
}

// SubscriptionLimits bounds the source's admitted subscriptions, including
// their worst-case driver backlog reservations. Zero selects finite defaults.
// Snapshot slots remain reserved until the snapshot-enabled stream closes.
type SubscriptionLimits struct {
	MaxSubscriptions         int   `json:"max_subscriptions,omitempty"`
	MaxSnapshotSubscriptions int   `json:"max_snapshot_subscriptions,omitempty"`
	MaxBytes                 int64 `json:"max_bytes,omitempty"`
}

func (l SubscriptionLimits) Validate() error {
	if l.MaxSubscriptions < 0 || l.MaxSnapshotSubscriptions < 0 || l.MaxBytes < 0 {
		return ErrInvalidSubscriptionLimits
	}
	return nil
}

func (l SubscriptionLimits) Effective() SubscriptionLimits {
	if l.MaxSubscriptions == 0 {
		l.MaxSubscriptions = 1024
	}
	if l.MaxSnapshotSubscriptions == 0 {
		l.MaxSnapshotSubscriptions = 4
	}
	if l.MaxBytes == 0 {
		l.MaxBytes = 256 << 20
	}
	return l
}
