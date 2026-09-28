// SPDX-License-Identifier: MPL-2.0

package kv

import kvapi "github.com/wippyai/runtime/api/store/kv"

var _ kvapi.LocalSnapshotReader = (*Service)(nil)
var _ kvapi.LocalSnapshotReader = (*RaftEngine)(nil)
var _ kvapi.LocalSnapshotScanner = (*Service)(nil)
var _ kvapi.LocalSnapshotScanner = (*RaftEngine)(nil)

func (s *Service) ScanLocalSnapshot(prefix string, fn func(kvapi.Entry, uint64) bool) error {
	snapshot := s.snap.Load()
	if snapshot == nil {
		return kvapi.ErrKVClosed
	}
	snapshot.scan(prefix, func(e kvapi.Entry) bool { return fn(e, snapshot.version) })
	return nil
}

func (e *RaftEngine) ScanLocalSnapshot(prefix string, fn func(kvapi.Entry, uint64) bool) error {
	if e.fsm == nil {
		return kvapi.ErrKVClosed
	}
	snapshot := e.fsm.snap.Load()
	if snapshot == nil {
		return kvapi.ErrKVClosed
	}
	snapshot.scan(prefix, func(item kvapi.Entry) bool { return fn(item, snapshot.index) })
	return nil
}

// ReadLocalSnapshot returns entries from one immutable in-memory publication.
// It deliberately does not barrier or forward: naming reconciliation needs a
// coherent local observation, while its write path retains the normal raft
// version guards.
func (s *Service) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	snapshot := s.snap.Load()
	entries, err := snapshot.getMany(keys)
	if err != nil {
		return nil, 0, err
	}
	return entries, snapshot.version, nil
}

// ReadLocalSnapshot returns entries from one immutable raft-FSM publication.
// The returned index is the applied KV index for that publication.
func (e *RaftEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	if e.fsm == nil {
		return nil, 0, kvapi.ErrKVClosed
	}
	snapshot := e.fsm.snap.Load()
	entries, err := snapshot.getMany(keys)
	if err != nil {
		return nil, 0, err
	}
	return entries, snapshot.index, nil
}
