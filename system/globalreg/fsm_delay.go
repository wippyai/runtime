// SPDX-License-Identifier: MPL-2.0

//go:build testdelay

package globalreg

import (
	"io"
	"os"
	"time"

	hraft "github.com/hashicorp/raft"
)

// DelayingFSM wraps an hraft.FSM and injects a configurable delay before
// each Apply call. This is used exclusively in integration tests (behind the
// "testdelay" build tag) to simulate slow Raft replication so that stale-read
// and fencing-token scenarios can be exercised on localhost.
//
// The delay is read from the WIPPY_FSM_APPLY_DELAY environment variable.
// Format: a Go duration string (e.g. "2s", "500ms"). If unset or invalid,
// no delay is applied.
type DelayingFSM struct {
	inner hraft.FSM
}

// NewDelayingFSM wraps the given FSM with optional Apply delay.
func NewDelayingFSM(inner hraft.FSM) *DelayingFSM {
	return &DelayingFSM{inner: inner}
}

func (d *DelayingFSM) Apply(log *hraft.Log) any {
	if delay := os.Getenv("WIPPY_FSM_APPLY_DELAY"); delay != "" {
		if dur, err := time.ParseDuration(delay); err == nil && dur > 0 {
			time.Sleep(dur)
		}
	}
	return d.inner.Apply(log)
}

func (d *DelayingFSM) Snapshot() (hraft.FSMSnapshot, error) {
	return d.inner.Snapshot()
}

func (d *DelayingFSM) Restore(rc io.ReadCloser) error {
	return d.inner.Restore(rc)
}
