// SPDX-License-Identifier: MPL-2.0

package kv

import "context"

// LocalSnapshotReader reads the requested keys from one immutable local
// revision. Missing keys are absent from the returned map, duplicate keys are
// returned once, and values in the result are owned by the caller. The
// revision describes the same snapshot as the entries; callers must not
// emulate this capability with separate Get calls. The revision is a backend
// snapshot token: an in-memory Service returns its publication revision, while
// the raft engine returns its applied log index. It is independent of an
// individual Entry.Epoch.
//
// This is an optional capability. It is a local observation and does not
// provide a quorum or leader-freshness guarantee, nor does it perform a
// network round trip.
type LocalSnapshotReader interface {
	ReadLocalSnapshot(keys []string) (map[string]Entry, uint64, error)
}

// LocalSnapshotScanner iterates a prefix from one immutable local publication.
// Every callback receives that publication's revision and owns its detached
// Entry.Value. Returning false stops iteration. This is a local observation,
// not a leader-freshness or quorum guarantee.
type LocalSnapshotScanner interface {
	ScanLocalSnapshot(prefix string, fn func(Entry, uint64) bool) error
}

// AuthoritySnapshot is a detached, immutable-at-publication view of selected
// keys. Revision is the applied revision of the KV domain that published all
// entries in the view. A key absent from Entries was absent at that revision.
//
// The map and entry values are detached copies. Callers must treat the map as
// read-only after receiving it; a later KV mutation cannot change this view.
type AuthoritySnapshot struct {
	Entries  map[string]Entry
	Revision uint64
}

// Get returns the entry for key and whether it was present in the snapshot.
func (s AuthoritySnapshot) Get(key string) (Entry, bool) {
	e, ok := s.Entries[key]
	return e, ok
}

// AuthoritySnapshotReader reads a bounded set of keys from an authoritative
// publication point. Implementations must return one coherent revision and
// must not silently substitute a stale local replica.
type AuthoritySnapshotReader interface {
	ReadAuthoritySnapshot(context.Context, []string) (AuthoritySnapshot, error)
}
