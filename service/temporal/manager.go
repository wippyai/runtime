package temporal

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	api "github.com/ponyruntime/pony/api/service/temporal"
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
		workflows:  workflow.NewWorkflowManager(logger),
	}
}

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

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required")
	}

	switch entry.Kind {
	case api.KindClient:
		cfg := new(api.ClientConfig)
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		if err := m.clients.Add(entry.ID, cfg); err != nil {
			return err
		}

		// Create and register service with supervisor
		service, err := m.clients.GetClient(entry.ID, m.dc)
		if err != nil {
			return err
		}

		m.bus.Send(ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Register,
			Path:   events.Path(entry.ID),
			Data: &supervisor.Entry{
				Service: service,
				Config:  supervisor.LifecycleConfig{AutoStart: false}, // supervisor will start client when needed
			},
		})

		return nil

	case api.KindTaskQueue:
		cfg := new(api.TaskQueueConfig)
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		// Check if referenced client exists
		if !m.clients.Has(cfg.Client) {
			return fmt.Errorf("client %s not found", cfg.Client)
		}

		if err := m.taskQueues.Add(entry.ID, cfg); err != nil {
			return err
		}

		// Get client for the task queue
		c, err := m.clients.GetClient(cfg.Client, m.dc) // todo: split this funcs
		if err != nil {
			return fmt.Errorf("failed to get client connection: %w", err)
		}

		// Get task queue service instance
		service, err := m.taskQueues.GetTaskQueue(entry.ID, c)
		if err != nil {
			return fmt.Errorf("failed to create task queue service: %w", err)
		}

		// Register task queue with supervisor
		m.bus.Send(ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Register,
			Path:   events.Path(entry.ID),
			Data: &supervisor.Entry{
				Service: service,
				Config:  service.GetLifecycleConfig(),
			},
		})

		return nil

	case api.KindWorkflow:
		m.log.Info("workflow registration not implemented", zap.String("id", string(entry.ID)))
		return nil

	case api.KindFunction:
		cfg := new(api.FunctionActivity) // We'll need to define this
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		// Check if referenced client exists
		if !m.taskQueues.Has(cfg.TaskQueue) {
			return fmt.Errorf("task queue %s not found", cfg.TaskQueue)
		}

		// We except task queue to be already defined
		taskQueue, err := m.taskQueues.GetTaskQueue(cfg.TaskQueue, nil)
		if err != nil {
			return fmt.Errorf("failed to get task queue: %w", err)
		}

		// Register activity with activity manager
		handler, err := m.activities.Register(entry.ID, cfg, taskQueue.GetClient())
		if err != nil {
			return fmt.Errorf("failed to cretate activity handler: %w", err)
		}

		// Register activity handler with task queue
		if err := taskQueue.RegisterActivity(string(entry.ID), handler); err != nil {
			return fmt.Errorf("failed to register activity with task queue: %w", err)
		}

		m.log.Info("activity registered successfully",
			zap.String("id", string(entry.ID)),
			zap.String("task_queue", string(cfg.TaskQueue)),
		)

		return nil

	default:
		m.log.Debug("ignoring entry kind", zap.String("kind", string(entry.Kind)))
		return nil
	}
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required")
	}

	switch entry.Kind {
	case api.KindClient:
		return fmt.Errorf("temporal clients cannot be updated at runtime, flush and re-add")

	case api.KindTaskQueue:
		cfg := new(api.TaskQueueConfig)
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		// Get old config to check if client reference changed
		oldCfg, exists := m.taskQueues.GetConfig(entry.ID)
		if !exists {
			return fmt.Errorf("task queue %s not found", entry.ID)
		}

		// Cannot change client reference
		if oldCfg.Client != cfg.Client {
			return fmt.Errorf("cannot change task queue client reference")
		}

		if err := m.taskQueues.Update(entry.ID, cfg); err != nil {
			return err
		}

		// Update supervisor with new lifecycle config
		m.bus.Send(ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Update,
			Path:   events.Path(entry.ID),
			Data: &supervisor.Entry{
				Config: cfg.Lifecycle,
			},
		})

		return nil

	case api.KindWorkflow:
		m.log.Info("workflow update not implemented", zap.String("id", string(entry.ID)))
		return nil

	case api.KindFunction:
		cfg := new(api.FunctionActivity) // We'll need to define this
		if err := m.unmarshalAndValidate(entry.Data, cfg); err != nil {
			return err
		}

		// Check if referenced client exists
		if !m.taskQueues.Has(cfg.TaskQueue) {
			return fmt.Errorf("task queue %s not found", cfg.TaskQueue)
		}

		// We except task queue to be already defined
		taskQueue, err := m.taskQueues.GetTaskQueue(cfg.TaskQueue, nil)
		if err != nil {
			return fmt.Errorf("failed to get task queue: %w", err)
		}

		// Register activity with activity manager
		handler, err := m.activities.Register(entry.ID, cfg, taskQueue.GetClient())
		if err != nil {
			return fmt.Errorf("failed to cretate activity handler: %w", err)
		}

		// Register activity handler with task queue
		if err := taskQueue.RegisterActivity(string(entry.ID), handler); err != nil {
			return fmt.Errorf("failed to register activity with task queue: %w", err)
		}

		m.log.Info("activity updated successfully",
			zap.String("id", string(entry.ID)),
			zap.String("task_queue", string(cfg.TaskQueue)),
		)

		return nil

	default:
		m.log.Debug("ignoring entry kind", zap.String("kind", string(entry.Kind)))
		return nil
	}
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.KindClient:
		// Check if any task queues still reference this client
		activeQueues := m.taskQueues.GetActiveTaskQueues(entry.ID)
		if activeQueues > 0 {
			return fmt.Errorf("client %s still has %d active task queues", entry.ID, activeQueues)
		}

		if err := m.clients.Delete(entry.ID); err != nil {
			return err
		}

		// Unregister from supervisor
		m.bus.Send(ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Remove,
			Path:   events.Path(entry.ID),
		})

		return nil

	case api.KindTaskQueue:
		if err := m.taskQueues.Delete(entry.ID); err != nil {
			return err
		}

		// Unregister from supervisor
		m.bus.Send(ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Remove,
			Path:   events.Path(entry.ID),
		})

		return nil

	case api.KindWorkflow:
		m.log.Info("workflow deletion not implemented", zap.String("id", string(entry.ID)))
		return nil

	case api.KindFunction:
		m.log.Info("activity deletion not implemented", zap.String("id", string(entry.ID)))
		return nil

	default:
		m.log.Debug("ignoring entry kind", zap.String("kind", string(entry.Kind)))
		return nil
	}
}
