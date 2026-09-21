// SPDX-License-Identifier: MPL-2.0

package kv

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
