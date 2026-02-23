// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"sort"

	"github.com/wippyai/runtime/api/registry"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
)

// serviceInfoAdapter adapts Supervisor to implement supervisorapi.ServiceInfo interface.
type serviceInfoAdapter struct {
	sup *Supervisor
}

// NewServiceInfoAdapter creates an adapter that exposes supervisor state as supervisorapi.ServiceInfo.
func NewServiceInfoAdapter(sup *Supervisor) supervisorapi.ServiceInfo {
	return &serviceInfoAdapter{sup: sup}
}

// GetState implements supervisorapi.ServiceInfo.
func (a *serviceInfoAdapter) GetState(id registry.ID) (supervisorapi.ServiceState, error) {
	state, err := a.sup.GetState(id.String())
	if err != nil {
		return supervisorapi.ServiceState{}, err
	}

	return supervisorapi.ServiceState{
		ID:         id,
		Status:     state.Status,
		Details:    state.Details,
		Desired:    state.Desired,
		RetryCount: state.RetryCount,
		LastUpdate: state.LastUpdate,
		StartedAt:  state.StartedAt,
	}, nil
}

// GetAllStates implements supervisorapi.ServiceInfo.
func (a *serviceInfoAdapter) GetAllStates() []supervisorapi.ServiceState {
	states := a.sup.GetAllStates()
	result := make([]supervisorapi.ServiceState, 0, len(states))

	for idStr, state := range states {
		result = append(result, supervisorapi.ServiceState{
			ID:         registry.ParseID(idStr),
			Status:     state.Status,
			Details:    state.Details,
			Desired:    state.Desired,
			RetryCount: state.RetryCount,
			LastUpdate: state.LastUpdate,
			StartedAt:  state.StartedAt,
		})
	}

	// Sort by ID for stable ordering
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID.String() < result[j].ID.String()
	})

	return result
}
