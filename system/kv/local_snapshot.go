// SPDX-License-Identifier: MPL-2.0

package kv

import kvapi "github.com/wippyai/runtime/api/store/kv"

var _ kvapi.LocalSnapshotReader = (*Service)(nil)
var _ kvapi.LocalSnapshotReader = (*RaftEngine)(nil)

func (s *Service) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	snapshot := s.snap.Load()
	entries, err := snapshot.getMany(keys)
	if err != nil {
		return nil, 0, err
	}
	return entries, snapshot.version, nil
}

func (e *RaftEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	snapshot := e.fsm.snap.Load()
	entries, err := snapshot.getMany(keys)
	if err != nil {
		return nil, 0, err
	}
	return entries, snapshot.index, nil
}
