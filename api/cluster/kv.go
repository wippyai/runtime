// SPDX-License-Identifier: MPL-2.0

package cluster

import (
	"context"
	"time"
)

// Version is a monotonically increasing revision number assigned to each
// key mutation. Version 0 means the key does not exist.
type Version = uint64

// Entry represents a key-value pair with version and optional lease binding.
type Entry struct {
	Key     string
	Value   []byte
	Version Version
	LeaseID LeaseID
}

// LeaseID uniquely identifies a lease within a KV instance.
type LeaseID string

// KV is the low-level coordination key-value store.
//
// String keys, byte values. Used internally for process naming, leader
// election, distributed locks, session tracking, and workflow ownership.
// User-level stores (api/store) wrap this with registry.ID / payload.Payload
// transcoding.
//
// Reads are always local (served from an in-memory replica). Writes may
// involve replication depending on the consistency mode of the underlying
// implementation.
type KV interface {
	// Get retrieves the value and version for a key.
	// Returns ErrKeyNotFound if the key does not exist.
	Get(key string) (Entry, error)

	// Set stores a value unconditionally, overwriting any existing entry.
	// Returns the new version.
	Set(key string, value []byte) (Version, error)

	// Delete removes a key. Returns ErrKeyNotFound if the key does not exist.
	Delete(key string) error

	// SetIfAbsent stores the value only if the key does not exist.
	// Returns the version and true if stored, or the existing entry's
	// version and false if the key already exists.
	SetIfAbsent(key string, value []byte) (Version, bool, error)

	// CompareAndSwap updates the value only if the current version matches
	// the expected version. Returns the new version and true on success,
	// or the actual version and false on mismatch.
	CompareAndSwap(key string, expect Version, value []byte) (Version, bool, error)

	// Scan iterates over entries whose keys start with the given prefix.
	// The callback returns true to continue, false to stop.
	Scan(prefix string, fn func(Entry) bool) error

	// Watch observes changes to keys matching the given prefix.
	// An empty prefix watches all keys. The returned Watcher must be
	// closed when no longer needed. Events are delivered via the event
	// bus using the KV instance's event system identifier.
	Watch(ctx context.Context, prefix string) (Watcher, error)

	// Lease operations.

	// GrantLease creates a lease that must be renewed before its TTL
	// expires. Keys written with SetWithLease are automatically deleted
	// when their lease expires or is revoked.
	GrantLease(ctx context.Context, ttl time.Duration) (Lease, error)

	// SetWithLease stores a value bound to a lease. The key is
	// automatically deleted when the lease expires or is revoked.
	// Returns the new version.
	SetWithLease(key string, value []byte, lease LeaseID) (Version, error)

	// SetIfAbsentWithLease combines SetIfAbsent with lease binding.
	SetIfAbsentWithLease(key string, value []byte, lease LeaseID) (Version, bool, error)
}

// Lease represents a time-bound ownership handle. Keys bound to a lease
// are automatically deleted when the lease expires or is revoked.
type Lease interface {
	// ID returns the unique identifier for this lease.
	ID() LeaseID

	// TTL returns the original time-to-live.
	TTL() time.Duration

	// KeepAlive renews the lease for another TTL period.
	KeepAlive(ctx context.Context) error

	// Revoke explicitly expires the lease and deletes all attached keys.
	Revoke(ctx context.Context) error

	// Done returns a channel closed when the lease expires or is revoked.
	Done() <-chan struct{}
}

// WatchEventType describes the kind of change observed.
type WatchEventType int

const (
	// WatchPut indicates a key was created or updated.
	WatchPut WatchEventType = iota
	// WatchDelete indicates a key was explicitly deleted.
	WatchDelete
	// WatchExpired indicates a key was removed due to lease expiry.
	WatchExpired
)

// WatchEvent represents a single key change.
type WatchEvent struct {
	Type     WatchEventType
	Current  *Entry // after the change (nil on delete/expire)
	Previous *Entry // before the change (nil on create)
}

// Watcher delivers change events for keys matching a prefix.
type Watcher interface {
	// Events returns the channel delivering watch events.
	Events() <-chan WatchEvent

	// Close stops the watcher and releases resources.
	Close() error
}
