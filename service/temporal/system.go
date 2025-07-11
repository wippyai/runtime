package temporal

import (
	"context"
	"fmt"
	"sync"

	temporaltq "github.com/ponyruntime/pony/service/temporal/task_queue"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/service/temporal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// System handles temporal service events
type System struct {
	log     *zap.Logger
	bus     event.Bus
	factory temporaltq.HostFactory
	hosts   sync.Map // map[registry.id]WorkerHost
}

// NewSystem creates a new temporal service manager with a custom task queue factory
func NewSystem(
	bus event.Bus,
	logger *zap.Logger,
	factory temporaltq.HostFactory,
) *System {
	return &System{
		log:     logger,
		bus:     bus,
		factory: factory,
	}
}

// Pattern returns the event matching criteria for this handler
func (m *System) Pattern() eventbus.Pattern {
	return eventbus.Pattern{System: temporal.System, Kind: "*"}
}

// Handle processes temporal system events
func (m *System) Handle(ctx context.Context, evt event.Event) error {
	if evt.System != temporal.System {
		return nil // Ignore non-temporal events
	}

	m.log.Debug("handling temporal event",
		zap.String("system", evt.System),
		zap.String("kind", evt.Kind),
		zap.String("path", evt.Path))

	// Process event based on kind
	switch evt.Kind {
	case temporal.TaskQueueRegister:
		return m.handleTaskQueueRegister(ctx, evt)
	case temporal.TaskQueueUpdate:
		return m.handleTaskQueueUpdate(ctx, evt)
	case temporal.TaskQueueDelete:
		return m.handleTaskQueueDelete(ctx, evt)
	case temporal.WorkflowRegister:
		return m.handleWorkflowRegister(ctx, evt)
	case temporal.WorkflowUpdate:
		return m.handleWorkflowUpdate(ctx, evt)
	case temporal.WorkflowDelete:
		return m.handleWorkflowDelete(ctx, evt)
	case temporal.ActivityRegister:
		return m.handleActivityRegister(ctx, evt)
	case temporal.ActivityUpdate:
		return m.handleActivityUpdate(ctx, evt)
	case temporal.ActivityDelete:
		return m.handleActivityDelete(ctx, evt)
	default:
		return nil // Ignore other events
	}
}

// Task Queue Handlers

func (m *System) handleTaskQueueRegister(ctx context.Context, evt event.Event) error {
	// Parse task queue registration from event
	registration, ok := evt.Data.(*temporal.TaskQueueRegistration)
	if !ok {
		return fmt.Errorf("expected TaskQueueRegistration, got %T", evt.Data)
	}

	// Validate the task queue registration
	if err := registration.Validate(); err != nil {
		m.sendRejectEvent(ctx, temporal.TaskQueueReject, evt, err.Error())
		return fmt.Errorf("invalid task queue registration: %w", err)
	}

	// Check if task queue id already exists
	if _, exists := m.hosts.Load(registration.ID); exists {
		m.sendRejectEvent(ctx, temporal.TaskQueueReject, evt,
			fmt.Sprintf("task queue %s already registered", registration.ID.String()))
		return fmt.Errorf("task queue %s already registered", registration.ID.String())
	}

	// Create task queue host using factory
	host, err := m.factory.CreateHost(registration)
	if err != nil {
		m.sendRejectEvent(ctx, temporal.TaskQueueReject, evt,
			fmt.Sprintf("failed to create task queue host: %s", err.Error()))
		return fmt.Errorf("failed to create task queue host: %w", err)
	}

	// Store the host in our hosts map
	m.hosts.Store(registration.ID, host)

	// Register as a delegated host in the process system
	m.bus.Send(ctx, event.Event{
		System: process.HostSystem,
		Kind:   process.HostRegister,
		Path:   registration.ID.String(),
		Data:   process.Delegated(host),
	})

	// Register with pubsub as a message host
	m.bus.Send(ctx, event.Event{
		System: pubsub.System,
		Kind:   pubsub.HostRegister,
		Path:   registration.ID.String(),
		Data:   pubsub.TransparentHost(host),
	})

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   registration.ID.String(),
		Data: &supervisor.Entry{
			Service: host,
			Config:  registration.GetLifecycleConfig(),
		},
	})

	// Send accept event
	m.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.TaskQueueAccept,
		Path:   evt.Path,
		Data:   registration,
	})

	m.log.Info("task queue registered",
		zap.String("id", registration.ID.String()),
		zap.String("task_queue", registration.TaskQueue))

	return nil
}

func (m *System) handleTaskQueueUpdate(ctx context.Context, evt event.Event) error {
	// Parse task queue registration from event
	registration, ok := evt.Data.(*temporal.TaskQueueRegistration)
	if !ok {
		return fmt.Errorf("expected TaskQueueRegistration, got %T", evt.Data)
	}

	// Validate the task queue registration
	if err := registration.Validate(); err != nil {
		m.sendRejectEvent(ctx, temporal.TaskQueueReject, evt, err.Error())
		return fmt.Errorf("invalid task queue registration: %w", err)
	}

	// Check if task queue exists
	hostValue, exists := m.hosts.Load(registration.ID)
	if !exists {
		m.sendRejectEvent(ctx, temporal.TaskQueueReject, evt,
			fmt.Sprintf("task queue %s not found", registration.ID.String()))
		return fmt.Errorf("task queue %s not found", registration.ID.String())
	}

	host, ok := hostValue.(temporaltq.WorkerHostAPI)
	if !ok {
		m.sendRejectEvent(ctx, temporal.TaskQueueReject, evt,
			fmt.Sprintf("invalid host type for %s", registration.ID.String()))
		return fmt.Errorf("invalid host type for %s", registration.ID.String())
	}

	// Update the host with new registration
	if err := host.Update(registration); err != nil {
		m.sendRejectEvent(ctx, temporal.TaskQueueReject, evt,
			fmt.Sprintf("failed to update task queue: %s", err.Error()))
		return fmt.Errorf("failed to update task queue: %w", err)
	}

	// Update supervisor configuration
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   registration.ID.String(),
		Data: &supervisor.Entry{
			Config: registration.GetLifecycleConfig(),
		},
	})

	// Send accept event
	m.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.TaskQueueAccept,
		Path:   evt.Path,
		Data:   registration,
	})

	m.log.Info("task queue updated",
		zap.String("id", registration.ID.String()),
		zap.String("task_queue", registration.TaskQueue))

	return nil
}

func (m *System) handleTaskQueueDelete(ctx context.Context, evt event.Event) error {
	// Parse task queue deletion from event
	deletion, ok := evt.Data.(*temporal.TaskQueueDeletion)
	if !ok {
		return fmt.Errorf("expected TaskQueueDeletion, got %T", evt.Data)
	}

	// Check if task queue exists
	if _, exists := m.hosts.Load(deletion.TaskQueue); !exists {
		return fmt.Errorf("task queue %s not found", deletion.TaskQueue.String())
	}

	// Unregister from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   deletion.TaskQueue.String(),
	})

	// Unregister from process hosts
	m.bus.Send(ctx, event.Event{
		System: process.HostSystem,
		Kind:   process.HostDelete,
		Path:   deletion.TaskQueue.String(),
	})

	// Unregister from pubsub
	m.bus.Send(ctx, event.Event{
		System: pubsub.System,
		Kind:   pubsub.HostDelete,
		Path:   deletion.TaskQueue.String(),
	})

	// Remove from hosts map
	m.hosts.Delete(deletion.TaskQueue)

	m.log.Info("task queue deleted", zap.String("id", deletion.TaskQueue.String()))
	return nil
}

// Workflow Handlers

func (m *System) handleWorkflowRegister(ctx context.Context, evt event.Event) error {
	// Parse workflow registration from event
	registration, ok := evt.Data.(*temporal.WorkflowRegistration)
	if !ok {
		return fmt.Errorf("expected WorkflowRegistration, got %T", evt.Data)
	}

	// Validate the workflow registration
	if err := m.validateWorkflowRegistration(registration); err != nil {
		m.sendRejectEvent(ctx, temporal.WorkflowReject, evt, err.Error())
		return fmt.Errorf("invalid workflow registration: %w", err)
	}

	// Get the host from the hosts map using task queue id
	hostValue, exists := m.hosts.Load(registration.TaskQueue)
	if !exists {
		m.sendRejectEvent(ctx, temporal.WorkflowReject, evt,
			fmt.Sprintf("task queue %s not found", registration.TaskQueue.String()))
		return fmt.Errorf("task queue %s not found", registration.TaskQueue.String())
	}

	host, ok := hostValue.(temporaltq.WorkerHostAPI)
	if !ok {
		m.sendRejectEvent(ctx, temporal.WorkflowReject, evt,
			fmt.Sprintf("invalid host type for %s", registration.TaskQueue.String()))
		return fmt.Errorf("invalid host type for %s", registration.TaskQueue.String())
	}

	// Register the workflow with the task queue host
	if err := host.RegisterWorkflow(ctx, registration); err != nil {
		m.sendRejectEvent(ctx, temporal.WorkflowReject, evt,
			fmt.Sprintf("failed to register workflow: %s", err.Error()))
		return fmt.Errorf("failed to register workflow: %w", err)
	}

	// Send accept event
	m.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.WorkflowAccept,
		Path:   evt.Path,
		Data:   registration,
	})

	m.log.Info("workflow registered",
		zap.String("name", registration.Name),
		zap.String("task_queue", registration.TaskQueue.String()))

	return nil
}

func (m *System) handleWorkflowUpdate(ctx context.Context, evt event.Event) error {
	// Parse workflow registration from event
	registration, ok := evt.Data.(*temporal.WorkflowRegistration)
	if !ok {
		return fmt.Errorf("expected WorkflowRegistration, got %T", evt.Data)
	}

	// Validate the workflow registration
	if err := m.validateWorkflowRegistration(registration); err != nil {
		m.sendRejectEvent(ctx, temporal.WorkflowReject, evt, err.Error())
		return fmt.Errorf("invalid workflow registration: %w", err)
	}

	// Get the host from the hosts map using task queue id
	hostValue, exists := m.hosts.Load(registration.TaskQueue)
	if !exists {
		m.sendRejectEvent(ctx, temporal.WorkflowReject, evt,
			fmt.Sprintf("task queue %s not found", registration.TaskQueue.String()))
		return fmt.Errorf("task queue %s not found", registration.TaskQueue.String())
	}

	host, ok := hostValue.(temporaltq.WorkerHostAPI)
	if !ok {
		m.sendRejectEvent(ctx, temporal.WorkflowReject, evt,
			fmt.Sprintf("invalid host type for %s", registration.TaskQueue.String()))
		return fmt.Errorf("invalid host type for %s", registration.TaskQueue.String())
	}

	// Register the updated workflow
	if err := host.RegisterWorkflow(ctx, registration); err != nil {
		m.sendRejectEvent(ctx, temporal.WorkflowReject, evt,
			fmt.Sprintf("failed to update workflow: %s", err.Error()))
		return fmt.Errorf("failed to update workflow: %w", err)
	}

	// Send accept event
	m.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.WorkflowAccept,
		Path:   evt.Path,
		Data:   registration,
	})

	m.log.Info("workflow updated",
		zap.String("name", registration.Name),
		zap.String("task_queue", registration.TaskQueue.String()))

	return nil
}

func (m *System) handleWorkflowDelete(ctx context.Context, evt event.Event) error {
	// Parse workflow deletion from event
	deletion, ok := evt.Data.(*temporal.WorkflowDeletion)
	if !ok {
		return fmt.Errorf("expected WorkflowDeletion, got %T", evt.Data)
	}

	// Get the host from the hosts map using task queue id
	hostValue, exists := m.hosts.Load(deletion.TaskQueue)
	if !exists {
		return fmt.Errorf("task queue %s not found", deletion.TaskQueue.String())
	}

	host, ok := hostValue.(temporaltq.WorkerHostAPI)
	if !ok {
		return fmt.Errorf("invalid host type for %s", deletion.TaskQueue.String())
	}

	// Delete the workflow by name
	if err := host.DeleteWorkflowByName(ctx, deletion.WorkflowName); err != nil {
		return fmt.Errorf("failed to delete workflow: %w", err)
	}

	m.log.Info("workflow deleted",
		zap.String("name", deletion.WorkflowName),
		zap.String("task_queue", deletion.TaskQueue.String()))
	return nil
}

// Activity Handlers

func (m *System) handleActivityRegister(ctx context.Context, evt event.Event) error {
	// Parse activity registration from event
	registration, ok := evt.Data.(*temporal.ActivityRegistration)
	if !ok {
		return fmt.Errorf("expected ActivityRegistration, got %T", evt.Data)
	}

	// Validate the activity registration
	if err := m.validateActivityRegistration(registration); err != nil {
		m.sendRejectEvent(ctx, temporal.ActivityReject, evt, err.Error())
		return fmt.Errorf("invalid activity registration: %w", err)
	}

	// Get the host from the hosts map using task queue id
	hostValue, exists := m.hosts.Load(registration.TaskQueue)
	if !exists {
		m.sendRejectEvent(ctx, temporal.ActivityReject, evt,
			fmt.Sprintf("task queue %s not found", registration.TaskQueue.String()))
		return fmt.Errorf("task queue %s not found", registration.TaskQueue.String())
	}

	host, ok := hostValue.(temporaltq.WorkerHostAPI)
	if !ok {
		m.sendRejectEvent(ctx, temporal.ActivityReject, evt,
			fmt.Sprintf("invalid host type for %s", registration.TaskQueue.String()))
		return fmt.Errorf("invalid host type for %s", registration.TaskQueue.String())
	}

	// Register the activity
	if err := host.RegisterActivity(ctx, registration); err != nil {
		m.sendRejectEvent(ctx, temporal.ActivityReject, evt,
			fmt.Sprintf("failed to register activity: %s", err.Error()))
		return fmt.Errorf("failed to register activity: %w", err)
	}

	// Send accept event
	m.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.ActivityAccept,
		Path:   evt.Path,
		Data:   registration,
	})

	m.log.Info("activity registered",
		zap.String("name", registration.Name),
		zap.String("task_queue", registration.TaskQueue.String()))

	return nil
}

func (m *System) handleActivityUpdate(ctx context.Context, evt event.Event) error {
	// Parse activity registration from event
	registration, ok := evt.Data.(*temporal.ActivityRegistration)
	if !ok {
		return fmt.Errorf("expected ActivityRegistration, got %T", evt.Data)
	}

	// Validate the activity registration
	if err := m.validateActivityRegistration(registration); err != nil {
		m.sendRejectEvent(ctx, temporal.ActivityReject, evt, err.Error())
		return fmt.Errorf("invalid activity registration: %w", err)
	}

	// Get the host from the hosts map using task queue id
	hostValue, exists := m.hosts.Load(registration.TaskQueue)
	if !exists {
		m.sendRejectEvent(ctx, temporal.ActivityReject, evt,
			fmt.Sprintf("task queue %s not found", registration.TaskQueue.String()))
		return fmt.Errorf("task queue %s not found", registration.TaskQueue.String())
	}

	host, ok := hostValue.(temporaltq.WorkerHostAPI)
	if !ok {
		m.sendRejectEvent(ctx, temporal.ActivityReject, evt,
			fmt.Sprintf("invalid host type for %s", registration.TaskQueue.String()))
		return fmt.Errorf("invalid host type for %s", registration.TaskQueue.String())
	}

	// Register the updated activity
	if err := host.RegisterActivity(ctx, registration); err != nil {
		m.sendRejectEvent(ctx, temporal.ActivityReject, evt,
			fmt.Sprintf("failed to update activity: %s", err.Error()))
		return fmt.Errorf("failed to update activity: %w", err)
	}

	// Send accept event
	m.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.ActivityAccept,
		Path:   evt.Path,
		Data:   registration,
	})

	m.log.Info("activity updated",
		zap.String("name", registration.Name),
		zap.String("task_queue", registration.TaskQueue.String()))

	return nil
}

func (m *System) handleActivityDelete(ctx context.Context, evt event.Event) error {
	// Parse activity deletion from event
	deletion, ok := evt.Data.(*temporal.ActivityDeletion)
	if !ok {
		return fmt.Errorf("expected ActivityDeletion, got %T", evt.Data)
	}

	// Get the host from the hosts map using task queue id
	hostValue, exists := m.hosts.Load(deletion.TaskQueue)
	if !exists {
		return fmt.Errorf("task queue %s not found", deletion.TaskQueue.String())
	}

	host, ok := hostValue.(temporaltq.WorkerHostAPI)
	if !ok {
		return fmt.Errorf("invalid host type for %s", deletion.TaskQueue.String())
	}

	// Delete the activity by name
	if err := host.DeleteActivityByName(ctx, deletion.ActivityName); err != nil {
		return fmt.Errorf("failed to delete activity: %w", err)
	}

	m.log.Info("activity deleted",
		zap.String("name", deletion.ActivityName),
		zap.String("task_queue", deletion.TaskQueue.String()))
	return nil
}

// Helper methods

func (m *System) validateWorkflowRegistration(reg *temporal.WorkflowRegistration) error {
	if reg.Name == "" {
		return fmt.Errorf("workflow name cannot be empty")
	}

	if reg.TaskQueue.String() == "" {
		return fmt.Errorf("task queue reference cannot be empty")
	}

	return nil
}

func (m *System) validateActivityRegistration(reg *temporal.ActivityRegistration) error {
	if reg.Name == "" {
		return fmt.Errorf("activity name cannot be empty")
	}

	if reg.TaskQueue.String() == "" {
		return fmt.Errorf("task queue reference cannot be empty")
	}

	return nil
}

func (m *System) sendRejectEvent(ctx context.Context, kind event.Kind, source event.Event, reason string) {
	m.log.Error("event rejected",
		zap.String("system", source.System),
		zap.String("kind", source.Kind),
		zap.String("path", source.Path),
		zap.String("reason", reason))

	m.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   kind,
		Path:   source.Path,
		Data:   reason,
	})
}

// Ensure System implements EventHandler interface
var _ eventbus.EventHandler = (*System)(nil)
