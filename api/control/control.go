package control

//
//import (
//	"context"
//
//	"github.com/ponyruntime/pony/api/events"
//	"github.com/ponyruntime/pony/api/registry"
//)
//
//const (
//	// System is the event system for the control component.
//	System events.System = "control"
//
//	// RegisterEntityRequest is the event kind for requesting the registration of a managed entity.
//	RegisterEntityRequest events.Kind = "control.register.entity"
//	// DeregisterEntityRequest is the event kind for requesting the deregistration of a managed entity.
//	DeregisterEntityRequest events.Kind = "control.deregister.entity"
//	// CommandStart is the event kind for starting an entity.
//	CommandStart events.Kind = "control.command.start"
//	// CommandStop is the event kind for stopping an entity.
//	CommandStop events.Kind = "control.command.stop"
//	// CommandRestart is the event kind for restarting an entity.
//	CommandRestart events.Kind = "control.command.restart"
//
//	// StatusStarting is the event kind for an entity reporting it is starting.
//	StatusStarting events.Kind = "control.status.starting"
//	// StatusRunning is the event kind for an entity reporting it is running.
//	StatusRunning events.Kind = "control.status.running"
//	// StatusStopping is the event kind for an entity reporting it is stopping.
//	StatusStopping events.Kind = "control.status.stopping"
//	// StatusStopped is the event kind for an entity reporting it has stopped.
//	StatusStopped events.Kind = "control.status.stopped"
//	// StatusFailed is the event kind for an entity reporting a failure.
//	StatusFailed events.Kind = "control.status.failed"
//
//	// CommandAccepted is the event kind for an entity acknowledging a command.
//	CommandAccepted events.Kind = "control.command.accepted"
//	// CommandRejected is the event kind for an entity rejecting a command.
//	CommandRejected events.Kind = "control.command.rejected"
//)
//
//// OperationalStatus represents the current operational status of a managed entity.
//type OperationalStatus string
//
//const (
//	StatusUnknown  OperationalStatus = "unknown"
//	StatusPending  OperationalStatus = "pending" // Awaiting execution or resources
//	StatusStarting OperationalStatus = "starting"
//	StatusRunning  OperationalStatus = "running"
//	StatusStopping OperationalStatus = "stopping"
//	StatusStopped  OperationalStatus = "stopped"
//	StatusFailed   OperationalStatus = "failed"
//	StatusDegraded OperationalStatus = "degraded" // Partially functional
//)
//
//// ManagedEntityInfo provides basic information about a managed entity.
//type ManagedEntityInfo struct {
//	ID     registry.ID
//	Kind   registry.Kind
//	Status OperationalStatus
//}
//
//// ControlEventPayload is a generic interface for control event payloads.
//type ControlEventPayload interface {
//	GetContext() context.Context
//}
//
//// RegisterEntityRequestPayload defines the payload for register entity requests.
//type RegisterEntityRequestPayload struct {
//	Ctx  context.Context
//	ID   registry.ID
//	Kind registry.Kind // Optionally include the kind if not derivable from context
//	// Potentially other registration details
//}
//
//func (p *RegisterEntityRequestPayload) GetContext() context.Context {
//	return p.Ctx
//}
//
//// DeregisterEntityRequestPayload defines the payload for deregister entity requests.
//type DeregisterEntityRequestPayload struct {
//	Ctx context.Context
//	ID  registry.ID
//}
//
//func (p *DeregisterEntityRequestPayload) GetContext() context.Context {
//	return p.Ctx
//}
//
//// CommandEventPayload defines the payload for command events sent to entities.
//type CommandEventPayload struct {
//	Ctx context.Context
//}
//
//func (p *CommandEventPayload) GetContext() context.Context {
//	return p.Ctx
//}
//
//// StatusEventPayload defines the payload for status events reported by entities.
//type StatusEventPayload struct {
//	Ctx    context.Context
//	Status OperationalStatus
//	Data   map[string]any // Optional data related to the status
//}
//
//func (p *StatusEventPayload) GetContext() context.Context {
//	return p.Ctx
//}
//
//// Control defines the interface for the entity control system.
//type Control interface {
//	// RegisterEntity registers a managed entity with the control system.
//	RegisterEntity(ctx context.Context, id registry.ID) error
//	// DeregisterEntity deregisters a managed entity from the control system.
//	DeregisterEntity(ctx context.Context, id registry.ID) error
//	// StartEntity initiates the start process for a managed entity.
//	StartEntity(ctx context.Context, id registry.ID) error
//	// StopEntity initiates the stop process for a managed entity.
//	StopEntity(ctx context.Context, id registry.ID) error
//	// RestartEntity initiates the restart process for a managed entity.
//	RestartEntity(ctx context.Context, id registry.ID) error
//	// GetOperationalStatus returns the current operational status of a managed entity.
//	GetOperationalStatus(ctx context.Context, id registry.ID) (OperationalStatus, error)
//	// ListManagedEntities returns a list of all currently managed entities.
//	ListManagedEntities(ctx context.Context) ([]ManagedEntityInfo, error)
//}
