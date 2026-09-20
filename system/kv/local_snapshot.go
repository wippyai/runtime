// SPDX-License-Identifier: MPL-2.0

package kv

import kvapi "github.com/wippyai/runtime/api/store/kv"

var _ kvapi.LocalSnapshotReader = (*Service)(nil)
var _ kvapi.LocalSnapshotReader = (*RaftEngine)(nil)

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
	if snapshot == nil {
		return nil, 0, kvapi.ErrKVClosed
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
	if snapshot == nil {
		return nil, 0, kvapi.ErrKVClosed
	}
	return entries, snapshot.index, nil
}
