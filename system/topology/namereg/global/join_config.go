// SPDX-License-Identifier: MPL-2.0

package global

import (
	"errors"
	"fmt"
	"time"
)

// JoinConfig controls resource bounds for a name-registry enrollment snapshot.
//
// The defaults are intentionally finite: a snapshot that exceeds MaxEntries or
// MaxBytes must fail as a whole, and callers must bound concurrent snapshot work
// with MaxConcurrent. Timeout bounds the authority barrier and reply-wait budget; discovery and
// transport sends retain their own lifecycle bounds.
type JoinConfig struct {
	Timeout       time.Duration
	MaxEntries    int
	MaxBytes      int
	MaxConcurrent int
}

const (
	defaultJoinTimeout       = 10 * time.Second
	defaultJoinMaxEntries    = 65536
	defaultJoinMaxBytes      = 16 << 20
	defaultJoinMaxConcurrent = 4
)

var (
	// ErrJoinSnapshotTooLarge means a complete authoritative join snapshot
	// exceeded its configured entry or byte bound. Callers must not use a
	// truncated snapshot as authoritative state.
	ErrJoinSnapshotTooLarge = errors.New("join snapshot too large")
	// ErrJoinBusy means the configured concurrent join-snapshot limit is full.
	// Callers should fail closed or retry according to their enrollment policy.
	ErrJoinBusy = errors.New("join snapshot busy")
)

// DefaultJoinConfig returns the bounded join-snapshot defaults. The values are
// 10 seconds, 65,536 entries, 16 MiB, and four concurrent snapshots.
func DefaultJoinConfig() JoinConfig {
	return JoinConfig{
		Timeout:       defaultJoinTimeout,
		MaxEntries:    defaultJoinMaxEntries,
		MaxBytes:      defaultJoinMaxBytes,
		MaxConcurrent: defaultJoinMaxConcurrent,
	}
}

// Validate rejects a configuration that would disable a required join bound.
// Validation errors identify the invalid field; ErrJoinSnapshotTooLarge and
// ErrJoinBusy describe runtime admission failures and are exported separately
// for the snapshot implementation to reuse.
func (c JoinConfig) Validate() error {
	switch {
	case c.Timeout <= 0:
		return fmt.Errorf("join timeout must be positive: %s", c.Timeout)
	case c.MaxEntries <= 0:
		return fmt.Errorf("join max entries must be positive: %d", c.MaxEntries)
	case c.MaxBytes <= 0:
		return fmt.Errorf("join max bytes must be positive: %d", c.MaxBytes)
	case c.MaxConcurrent <= 0:
		return fmt.Errorf("join max concurrent must be positive: %d", c.MaxConcurrent)
	default:
		return nil
	}
}
