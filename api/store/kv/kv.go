// SPDX-License-Identifier: MPL-2.0

// Package kv defines the low-level coordination key-value engine that backs the
// store.kv.* store kinds. Keys are strings and values are bytes; the user-facing
// api/store wrappers transcode registry.ID / payload.Payload onto this engine.
package kv

import (
	"context"
	"time"
)

// Version is a monotonically increasing revision assigned to each key mutation.
// Version 0 means the key does not exist.
type Version = uint64

// Entry is a key-value pair with its version and optional lease binding.
type Entry struct {
	Key     string
	LeaseID LeaseID
	Value   []byte
	Version Version
}

// LeaseID uniquely identifies a lease within an engine instance.
type LeaseID string

// Engine is the low-level coordination key-value store. Reads are always local
// (served from an in-memory replica); writes may replicate depending on the
// backend (raft for store.kv.raft, gossip CRDT for store.kv.crdt).
type Engine interface {
	// Get retrieves the value and version for a key, or ErrKeyNotFound.
	Get(key string) (Entry, error)

	// Set stores a value unconditionally and returns the new version.
	Set(key string, value []byte) (Version, error)

	// Delete removes a key, or returns ErrKeyNotFound.
	Delete(key string) error

	// SetIfAbsent stores only if the key does not exist. Returns the version
	// and true when stored, or the existing version and false otherwise.
	SetIfAbsent(key string, value []byte) (Version, bool, error)

	// CompareAndSwap updates only if the current version matches expect.
	// Returns the new version and true on success, or the actual version and
	// false on mismatch.
	CompareAndSwap(key string, expect Version, value []byte) (Version, bool, error)

	// Scan iterates entries whose keys start with prefix; fn returns false to
	// stop.
	Scan(prefix string, fn func(Entry) bool) error

	// Watch observes changes to keys matching prefix (empty watches all). The
	// returned Watcher must be Closed when no longer needed.
	Watch(ctx context.Context, prefix string) (Watcher, error)

	// GrantLease creates a lease that must be renewed before its TTL expires.
	// Keys written with SetWithLease are deleted when the lease expires or is
	// revoked.
	GrantLease(ctx context.Context, ttl time.Duration) (Lease, error)

	// SetWithLease stores a value bound to a lease and returns the new version.
	SetWithLease(key string, value []byte, lease LeaseID) (Version, error)

	// SetIfAbsentWithLease combines SetIfAbsent with lease binding.
	SetIfAbsentWithLease(key string, value []byte, lease LeaseID) (Version, bool, error)
}

// Lease is a time-bound ownership handle. Keys bound to a lease are deleted when
// it expires or is revoked.
type Lease interface {
	// ID returns the lease identifier.
	ID() LeaseID

	// TTL returns the original time-to-live.
	TTL() time.Duration

	// KeepAlive renews the lease for another TTL period.
	KeepAlive(ctx context.Context) error

	// Revoke expires the lease and deletes all attached keys.
	Revoke(ctx context.Context) error

	// Done is closed when the lease expires or is revoked.
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
	Current  *Entry // after the change (nil on delete/expire)
	Previous *Entry // before the change (nil on create)
	Type     WatchEventType
}

// Watcher delivers change events for keys matching a prefix.
type Watcher interface {
	// Events returns the channel delivering watch events.
	Events() <-chan WatchEvent

	// Close stops the watcher and releases resources.
	Close() error
}
