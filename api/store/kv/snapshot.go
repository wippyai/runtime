// SPDX-License-Identifier: MPL-2.0

package kv

// LocalSnapshotReader reads a bounded set of keys from one published local
// revision. Missing keys are absent from the returned map; returned values are
// owned by the caller. Repeated keys have one result. The index describes that
// same snapshot, not a newer committed revision sampled separately.
//
// This is an optional engine capability. It provides an atomic local view, not
// a quorum or leader freshness guarantee, and performs no network round trip.
// Callers requiring this guarantee must not emulate it with separate Get calls.
type LocalSnapshotReader interface {
	ReadLocalSnapshot(keys []string) (map[string]Entry, uint64, error)
}

// ConditionalSnapshotReader compares an authority-confirmed revision and scans
// only if it changed. known must describe a complete prior scan of this same
// prefix in the same authority's revision lineage. Zero always requests a scan.
// The revision covers the engine's full keyspace; unrelated writes may cause a
// conservative changed result. An error never authorizes cached-state reuse.
// Distributed implementations must confirm authority even for equality.
// unchanged=true guarantees the visitor was not called; false returns the same
// immutable snapshot's revision alongside its entries. Early visitor stop does
// not produce a complete snapshot suitable for caching.
type ConditionalSnapshotReader interface {
	ScanAtIndexSince(prefix string, known uint64, visit func(Entry) bool) (revision uint64, unchanged bool, err error)
}
