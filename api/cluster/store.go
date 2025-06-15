// Package cluster provides a minimal façade over the Raft-backed key/value
// state machine used by the runtime.  Reads are served locally for speed;
// Sync creates an explicit linearization point when strict freshness is
// required.
package cluster

import (
	"context"
	"errors"
)

// ErrCAS indicates that Put or Delete failed because the current key revision
// did not match the caller-supplied expectRev value.
var ErrCAS = errors.New("compare-and-swap failed")

// Store offers linearizable writes and very fast local reads.  Keys are plain
// strings (conventionally path-like via . or /); values are opaque byte slices.
//
// Typical usage:
//
//	newRev, err := s.Put(ctx, "registry.pids.foo", payload, 0) // create-only
//	value, rev, _ := s.Get(ctx, "registry.pids.foo")
//	keys := s.Keys("/registry/")  // fast, may lag leader slightly
//	_ = s.Sync(ctx)               // ensure we are caught up to commit index
type Store interface {
	// Get fetches the current value and revision for key.  If the key does not
	// exist, val == nil and rev == 0.  The read is served from the local FSM
	// copy and involves no network I/O.
	Get(ctx context.Context, key string) (val []byte, rev uint64, err error)

	// Put stores val under key if the existing revision equals expectRev.
	//
	//   • expectRev == 0  →  key must not exist (create-only)
	//   • expectRev > 0   →  compare-and-swap
	//
	// On success the new revision is returned.  On mismatch ErrCAS is returned
	// and no state change occurs.
	Put(ctx context.Context, key string, val []byte, expectRev uint64) (newRev uint64, err error)

	// Delete removes key if its revision equals expectRev.
	//
	//   • expectRev == 0  →  unconditional delete
	//   • expectRev > 0   →  compare-and-swap
	//
	// The boolean ok reports whether the key was actually removed.
	Delete(ctx context.Context, key string, expectRev uint64) (ok bool, err error)

	// Keys returns every key that has the supplied prefix.  The snapshot is
	// taken from the local FSM copy and therefore does not block on network
	// communication; it may lag the leader by a few milliseconds.
	Keys(prefix string) []string

	// Sync blocks until the local node has applied the cluster’s current commit
	// index.  After Sync returns, subsequent Get or Keys calls reflect all
	// writes that committed before Sync was invoked.
	Sync(ctx context.Context) error

	// todo: locks and etc, merge with normal store
}
