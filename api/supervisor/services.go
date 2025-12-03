package supervisor

import (
	"context"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

var servicesCtxKey = &ctxapi.Key{Name: "supervisor.servicesCtxKey"}

// ServiceState represents the runtime state of a supervised service.
type ServiceState struct {
	ID         registry.ID `json:"id"`
	Status     string      `json:"status"`
	Details    any         `json:"details"`
	Desired    string      `json:"desired"`
	RetryCount int32       `json:"retry_count"`
	LastUpdate time.Time   `json:"last_update"`
	StartedAt  time.Time   `json:"started_at"`
}

// ServiceInfo provides access to information about running services.
type ServiceInfo interface {
	// GetState returns the current state of a service by ID.
	GetState(id registry.ID) (ServiceState, error)
	// GetAllStates returns states of all registered services.
	GetAllStates() []ServiceState
}

// GetServiceInfo retrieves the service info provider from context.
func GetServiceInfo(ctx context.Context) ServiceInfo {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if svc := ac.Get(servicesCtxKey); svc != nil {
		return svc.(ServiceInfo)
	}
	return nil
}

// WithServiceInfo stores the service info provider in context.
func WithServiceInfo(ctx context.Context, info ServiceInfo) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(servicesCtxKey) == nil {
		ac.With(servicesCtxKey, info)
	}
	return ctx
}
