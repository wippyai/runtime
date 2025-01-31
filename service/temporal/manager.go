package temporal

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/runtime/temporal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/service/temporal/activity"
	"github.com/ponyruntime/pony/service/temporal/client"
	"github.com/ponyruntime/pony/service/temporal/data_converter"
	tq "github.com/ponyruntime/pony/service/temporal/task_queue"
	"github.com/ponyruntime/pony/service/temporal/workflow"
	"go.temporal.io/sdk/converter"
	"go.uber.org/zap"
)

// Manager handles temporal service registration and lifecycle
type Manager struct {
	ctx        context.Context
	log        *zap.Logger
	bus        events.Bus
	dtt        payload.Transcoder
	dc         converter.DataConverter
	clients    *client.Manager
	taskQueues *tq.Manager
	activities *activity.Manager
	workflows  *workflow.Manager
}

// NewManager creates a new temporal service manager
func NewManager(
	bus events.Bus,
	dtt payload.Transcoder,
	exec runtime.Executor,
	workflows runtime.WorkflowRegistry,
	logger *zap.Logger,
) *Manager {
	return &Manager{
		log:        logger,
		bus:        bus,
		dtt:        dtt,
		dc:         data_converter.NewDataConverter(dtt, converter.GetDefaultDataConverter()),
		clients:    client.NewClientManager(logger),
		taskQueues: tq.NewTaskQueueManager(logger),
		activities: activity.NewActivityManager(logger, exec),
		workflows:  workflow.NewWorkflowManager(logger, workflows),
	}
}

func (m *Manager) Start(ctx context.Context) error {
	m.ctx = ctx
	return nil
}

// Helper methods
func (m *Manager) unmarshalAndValidate(data payload.Payload, cfg interface{}) error {
	if err := m.dtt.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if validator, ok := cfg.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}
	}

	return nil
}

func (m *Manager) registerWithSupervisor(
	ctx context.Context,
	id registry.ID,
	service supervisor.Service,
	config supervisor.LifecycleConfig,
) {
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   events.Path(id),
		Data: &supervisor.Entry{
			Service: service,
			Config:  config,
		},
	})
}

func (m *Manager) unregisterFromSupervisor(
	ctx context.Context,
	id registry.ID,
) {
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   events.Path(id),
	})
}

// Add handles registration of new temporal components
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required")
	}

	switch entry.Kind {
	case api.KindClient:
		return m.addClient(ctx, entry)
	case api.KindTaskQueue:
		return m.addTaskQueue(ctx, entry)
	case api.KindWorkflow:
		return m.addWorkflow(ctx, entry)
	case api.KindFunction:
		return m.addFunction(ctx, entry)
	default:
		m.log.Debug("ignoring entry kind", zap.String("kind", string(entry.Kind)))
		return nil
	}
}

func (m *Manager) addClient(ctx context.Context, entry registry.Entry) error {
	cfg := new(api.ClientConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	c, err := m.clients.AddClient(entry.ID, cfg, m.dc)
	if err != nil {
		return err
	}

	m.registerWithSupervisor(ctx, entry.ID, c, supervisor.LifecycleConfig{AutoStart: false})

	return nil
}

func (m *Manager) addTaskQueue(ctx context.Context, entry registry.Entry) error {
	cfg := new(api.TaskQueueConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	c, err := m.clients.GetClient(cfg.Client)
	if err != nil {
		return fmt.Errorf("failed to get client: %w", err)
	}

	service, err := m.taskQueues.AddTaskQueue(entry.ID, cfg, c)
	if err != nil {
		return fmt.Errorf("failed to init task queue: %w", err)
	}

	m.registerWithSupervisor(ctx, entry.ID, service, service.GetLifecycleConfig())

	return nil
}

func (m *Manager) addWorkflow(ctx context.Context, entry registry.Entry) error {
	cfg := new(api.WorkflowDefinition)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	taskQueue, err := m.taskQueues.Get(cfg.TaskQueue)
	if err != nil {
		return fmt.Errorf("failed to get task queue: %w", err)
	}

	w, err := m.workflows.InitWorkflow(m.ctx, entry.ID, cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize workflow: %w", err)
	}

	if err := taskQueue.RegisterWorkflow(string(entry.ID), w); err != nil {
		return fmt.Errorf("failed to register workflow with task queue: %w", err)
	}

	m.log.Info("workflow registered successfully",
		zap.String("id", string(entry.ID)),
		zap.String("task_queue", string(cfg.TaskQueue)),
	)
	return nil
}

func (m *Manager) addFunction(ctx context.Context, entry registry.Entry) error {
	cfg := new(api.ActivityDefinition)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	taskQueue, err := m.taskQueues.Get(cfg.TaskQueue)
	if err != nil {
		return fmt.Errorf("failed to get task queue: %w", err)
	}

	handler, err := m.activities.AddHandler(entry.ID, cfg, taskQueue.GetClient())
	if err != nil {
		return fmt.Errorf("failed to create activity handler: %w", err)
	}

	if err := taskQueue.RegisterActivity(cfg.Name, handler); err != nil {
		return fmt.Errorf("failed to register activity with task queue: %w", err)
	}

	m.log.Info("activity registered successfully",
		zap.String("id", cfg.Name),
		zap.String("task_queue", string(cfg.TaskQueue)),
	)
	return nil
}

// Update handles updates to existing temporal components
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required")
	}

	switch entry.Kind {
	case api.KindClient:
		return fmt.Errorf("temporal clients cannot be updated at runtime, flush and re-add")
	case api.KindTaskQueue:
		return m.updateTaskQueue(ctx, entry)
	case api.KindWorkflow:
		return m.updateWorkflow(ctx, entry)
	case api.KindFunction:
		return m.updateFunction(ctx, entry)
	default:
		m.log.Debug("ignoring entry kind", zap.String("kind", string(entry.Kind)))
		return nil
	}
}

func (m *Manager) updateTaskQueue(ctx context.Context, entry registry.Entry) error {
	cfg := new(api.TaskQueueConfig)
	if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	oldCfg, exists := m.taskQueues.GetConfig(entry.ID)
	if !exists {
		return fmt.Errorf("task queue %s not found", entry.ID)
	}

	if oldCfg.Client != cfg.Client {
		return fmt.Errorf("cannot change task queue client reference")
	}

	if err := m.taskQueues.Update(entry.ID, cfg); err != nil {
		return err
	}

	taskQueue, err := m.taskQueues.Get(entry.ID)
	if err != nil {
		return fmt.Errorf("failed to get task queue: %w", err)
	}

	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   events.Path(entry.ID),
		Data: &supervisor.Entry{
			Config: taskQueue.GetLifecycleConfig(),
		},
	})

	return nil
}

func (m *Manager) updateWorkflow(ctx context.Context, entry registry.Entry) error {
	// todo: implement and fix
	return nil
}

func (m *Manager) updateFunction(ctx context.Context, entry registry.Entry) error {
	// todo: implement and fix
	return nil
}

// Delete handles removal of temporal components
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.KindClient:
		return m.deleteClient(ctx, entry)
	case api.KindTaskQueue:
		return m.deleteTaskQueue(ctx, entry)
	case api.KindWorkflow:
		return m.deleteWorkflow(ctx, entry)
	case api.KindFunction:
		return m.deleteFunction(ctx, entry)
	default:
		m.log.Debug("ignoring entry kind", zap.String("kind", string(entry.Kind)))
		return nil
	}
}

func (m *Manager) GetClient(id registry.ID) (*client.Client, error) {
	return m.clients.GetClient(id)
}

func (m *Manager) deleteClient(ctx context.Context, entry registry.Entry) error {
	activeQueues := m.taskQueues.GetActiveTaskQueues(entry.ID)
	if activeQueues > 0 {
		return fmt.Errorf("client %s still has %d active task queues", entry.ID, activeQueues)
	}

	if err := m.clients.Delete(entry.ID); err != nil {
		return err
	}

	m.unregisterFromSupervisor(ctx, entry.ID)
	return nil
}

func (m *Manager) deleteTaskQueue(ctx context.Context, entry registry.Entry) error {
	if err := m.taskQueues.Delete(entry.ID); err != nil {
		return err
	}

	m.unregisterFromSupervisor(ctx, entry.ID)
	return nil
}

func (m *Manager) deleteWorkflow(ctx context.Context, entry registry.Entry) error {
	if err := m.workflows.Delete(entry.ID); err != nil {
		return fmt.Errorf("failed to delete workflow: %w", err)
	}

	// todo: update task queue

	m.log.Info("workflow deleted successfully", zap.String("id", string(entry.ID)))
	return nil
}

func (m *Manager) deleteFunction(ctx context.Context, entry registry.Entry) error {
	if err := m.activities.Delete(entry.ID); err != nil {
		return fmt.Errorf("failed to delete activity handler: %w", err)
	}

	// todo: update task queue

	m.log.Info("activity deleted successfully", zap.String("id", string(entry.ID)))
	return nil
}
