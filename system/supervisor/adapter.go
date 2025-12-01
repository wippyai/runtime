package supervisor

import (
	"sort"

	"github.com/wippyai/runtime/api/registry"
	systemapi "github.com/wippyai/runtime/api/system"
)

// serviceInfoAdapter adapts Supervisor to implement system.ServiceInfo interface.
type serviceInfoAdapter struct {
	sup *Supervisor
}

// NewServiceInfoAdapter creates an adapter that exposes supervisor state as system.ServiceInfo.
func NewServiceInfoAdapter(sup *Supervisor) systemapi.ServiceInfo {
	return &serviceInfoAdapter{sup: sup}
}

// GetState implements system.ServiceInfo.
func (a *serviceInfoAdapter) GetState(id registry.ID) (systemapi.ServiceState, error) {
	state, err := a.sup.GetState(id.String())
	if err != nil {
		return systemapi.ServiceState{}, err
	}

	return systemapi.ServiceState{
		ID:         id,
		Status:     state.Status,
		Details:    state.Details,
		Desired:    state.Desired,
		RetryCount: state.RetryCount,
		LastUpdate: state.LastUpdate,
		StartedAt:  state.StartedAt,
	}, nil
}

// GetAllStates implements system.ServiceInfo.
func (a *serviceInfoAdapter) GetAllStates() []systemapi.ServiceState {
	states := a.sup.GetAllStates()
	result := make([]systemapi.ServiceState, 0, len(states))

	for idStr, state := range states {
		result = append(result, systemapi.ServiceState{
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
