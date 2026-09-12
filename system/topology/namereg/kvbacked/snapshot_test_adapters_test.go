// SPDX-License-Identifier: MPL-2.0

package kvbacked

import kvapi "github.com/wippyai/runtime/api/store/kv"

func (e *countedReconcilerEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}
func (e *blockedEventsEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}
func (e *beforePromotionEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}
func (e *readinessEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}
func (e *admissionSeedEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}
func (e *beforeExpiryEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	return e.Engine.(kvapi.LocalSnapshotReader).ReadLocalSnapshot(keys)
}
func (e failingActiveRead) ReadLocalSnapshot([]string) (map[string]kvapi.Entry, uint64, error) {
	return nil, 0, e.failure
}
