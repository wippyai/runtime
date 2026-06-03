// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"

	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

// Strong-scope orchestration is layered onto the same _sys:registry keyspace in
// a later phase. Until the Strong control plane is wired, the kv-backed service
// reports no local reservation and a completed barrier (single-backend mode),
// and rejects Strong writes so callers fall back rather than silently degrade.

func (s *Service) registerStrong(_ context.Context, _ string, _ pid.PID) (globalapi.RegisterOutcome, error) {
	return globalapi.RegisterOutcome{}, globalapi.ErrNotAvailable
}

func (s *Service) unregisterStrong(_ context.Context, _ string) (bool, error) {
	return false, globalapi.ErrNotAvailable
}

func (s *Service) strongReserved(_ string) (pid.PID, bool) {
	return pid.PID{}, false
}

func (s *Service) nameReady() bool {
	return true
}
