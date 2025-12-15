package worker

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/entry"
	"go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
)

// Manager handles Temporal worker configuration and lifecycle
type Manager struct {
	log                *zap.Logger
	dtt                payload.Transcoder
	bus                event.Bus
	res                resource.Registry
	factory            Factory
	workerInterceptors []interceptor.WorkerInterceptor

	mu       sync.RWMutex
	configs  map[registry.ID]*api.WorkerConfig
	services map[registry.ID]*Worker
}

// NewManager creates a new worker manager instance
func NewManager(
	logger *zap.Logger,
	transcoder payload.Transcoder,
	bus event.Bus,
	resourceReg resource.Registry,
	workerInterceptors []interceptor.WorkerInterceptor,
) (*Manager, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if transcoder == nil {
		return nil, fmt.Errorf("transcoder is required")
	}
	if bus == nil {
		return nil, fmt.Errorf("event bus is required")
	}
	if resourceReg == nil {
		return nil, fmt.Errorf("resource registry is required")
	}

	factory := NewDefaultWorkerFactory(workerInterceptors)

	return &Manager{
		log:                logger,
		dtt:                transcoder,
		bus:                bus,
		res:                resourceReg,
		factory:            factory,
		workerInterceptors: workerInterceptors,
		configs:            make(map[registry.ID]*api.WorkerConfig),
		services:           make(map[registry.ID]*Worker),
	}, nil
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, ent registry.Entry) error {
	if ent.Kind != api.Worker {
		return fmt.Errorf("unexpected entry kind: %s", ent.Kind)
	}

	m.log.Debug("processing temporal worker entry",
		zap.String("id", ent.ID.String()),
		zap.String("kind", ent.Kind))

	cfg, err := entry.DecodeEntryConfig[api.WorkerConfig](ctx, m.dtt, ent)
	if err != nil {
		return fmt.Errorf("failed to decode worker config: %w", err)
	}

	return m.AddWorker(ctx, ent.ID, cfg)
}

// AddWorker initializes a new worker instance with the given configuration
func (m *Manager) AddWorker(ctx context.Context, id registry.ID, cfg *api.WorkerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if worker already exists
	if _, exists := m.services[id]; exists {
		return fmt.Errorf("worker %s already initialized", id)
	}

	if _, exists := m.configs[id]; exists {
		return fmt.Errorf("worker config %s already exists", id)
	}

	// Initialize defaults
	cfg.InitDefaults()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid worker config: %w", err)
	}

	// Add client dependency to lifecycle config
	if cfg.Lifecycle.DependsOn == nil {
		cfg.Lifecycle.DependsOn = []string{cfg.Client.String()}
	} else {
		found := false
		for _, dep := range cfg.Lifecycle.DependsOn {
			if dep == cfg.Client.String() {
				found = true
				break
			}
		}
		if !found {
			cfg.Lifecycle.DependsOn = append(cfg.Lifecycle.DependsOn, cfg.Client.String())
		}
	}

	// Store configuration
	m.configs[id] = cfg

	// Create new service
	service, err := m.factory.CreateWorker(ctx, m.log, id, cfg, m.res)
	if err != nil {
		delete(m.configs, id)
		return fmt.Errorf("failed to create worker: %w", err)
	}

	m.services[id] = service

	// Register with supervisor - workers depend on their client (set in lifecycle config above)
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   id.String(),
		Data: &supervisor.Entry{
			Service: service,
			Config:  cfg.Lifecycle,
		},
	})

	// Send task queue registration event to host manager
	m.bus.Send(ctx, event.Event{
		System: api.SystemTemporalTaskQueue,
		Kind:   api.TaskQueueRegister,
		Path:   id.String(),
		Data: &api.TaskQueueRegistration{
			ID:        id,
			Client:    cfg.Client,
			TaskQueue: cfg.TaskQueue,
		},
	})

	m.log.Info("initialized temporal worker",
		zap.String("id", id.String()),
		zap.String("client", cfg.Client.String()),
		zap.String("task_queue", cfg.TaskQueue),
		zap.Int("max_concurrent_activities", cfg.WorkerOptions.MaxConcurrentActivityExecutionSize))

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, ent registry.Entry) error {
	if ent.Kind != api.Worker {
		return fmt.Errorf("unexpected entry kind: %s", ent.Kind)
	}

	m.log.Debug("updating temporal worker entry",
		zap.String("id", ent.ID.String()),
		zap.String("kind", ent.Kind))

	cfg, err := entry.DecodeEntryConfig[api.WorkerConfig](ctx, m.dtt, ent)
	if err != nil {
		return fmt.Errorf("failed to decode worker config: %w", err)
	}

	return m.UpdateWorker(ctx, ent.ID, cfg)
}

// UpdateWorker updates an existing worker configuration
func (m *Manager) UpdateWorker(ctx context.Context, id registry.ID, cfg *api.WorkerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("worker config %s not found", id)
	}

	// Initialize defaults
	cfg.InitDefaults()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid worker config: %w", err)
	}

	// Add client dependency to lifecycle config
	if cfg.Lifecycle.DependsOn == nil {
		cfg.Lifecycle.DependsOn = []string{cfg.Client.String()}
	} else {
		found := false
		for _, dep := range cfg.Lifecycle.DependsOn {
			if dep == cfg.Client.String() {
				found = true
				break
			}
		}
		if !found {
			cfg.Lifecycle.DependsOn = append(cfg.Lifecycle.DependsOn, cfg.Client.String())
		}
	}

	// Store updated configuration
	m.configs[id] = cfg

	// Update supervisor configuration
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceUpdate,
		Path:   id.String(),
		Data: &supervisor.Entry{
			Config: cfg.Lifecycle,
		},
	})

	m.log.Info("updated temporal worker config",
		zap.String("id", id.String()),
		zap.String("task_queue", cfg.TaskQueue))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, ent registry.Entry) error {
	if ent.Kind != api.Worker {
		return fmt.Errorf("unexpected entry kind: %s", ent.Kind)
	}

	m.log.Debug("deleting temporal worker entry",
		zap.String("id", ent.ID.String()),
		zap.String("kind", ent.Kind))

	return m.DeleteWorker(ctx, ent.ID)
}

// DeleteWorker removes a worker configuration and service if it exists
func (m *Manager) DeleteWorker(ctx context.Context, id registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("worker config %s not found", id)
	}

	// Unregister from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   id.String(),
	})

	delete(m.configs, id)
	delete(m.services, id)

	m.log.Info("deleted temporal worker", zap.String("id", id.String()))
	return nil
}

// GetWorker retrieves an existing worker by id
func (m *Manager) GetWorker(id registry.ID) (*Worker, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	service, exists := m.services[id]
	if !exists {
		return nil, fmt.Errorf("worker %s not initialized", id)
	}
	return service, nil
}

// GetConfig retrieves a worker config by id
func (m *Manager) GetConfig(id registry.ID) (*api.WorkerConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg, exists := m.configs[id]
	return cfg, exists
}

// Has checks if a worker config exists
func (m *Manager) Has(id registry.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.configs[id]
	return exists
}

// RegisterActivity registers an activity with a worker
func (m *Manager) RegisterActivity(ctx context.Context, workerID registry.ID, activityName string, funcID registry.ID) error {
	worker, err := m.GetWorker(workerID)
	if err != nil {
		return fmt.Errorf("worker %s not found: %w", workerID, err)
	}

	return worker.RegisterActivity(ctx, activityName, funcID)
}

// RegisterLocalActivity registers a local activity with a worker
func (m *Manager) RegisterLocalActivity(ctx context.Context, workerID registry.ID, activityName string, funcID registry.ID) error {
	worker, err := m.GetWorker(workerID)
	if err != nil {
		return fmt.Errorf("worker %s not found: %w", workerID, err)
	}

	return worker.RegisterLocalActivity(ctx, activityName, funcID)
}

// UnregisterActivity removes an activity from a worker
func (m *Manager) UnregisterActivity(_ context.Context, workerID registry.ID, activityName string) error {
	worker, err := m.GetWorker(workerID)
	if err != nil {
		return fmt.Errorf("worker %s not found: %w", workerID, err)
	}

	return worker.UnregisterActivity(activityName)
}

// RegisterWorkflow registers a workflow with a worker
func (m *Manager) RegisterWorkflow(ctx context.Context, workerID registry.ID, workflowName string, handler any) error {
	worker, err := m.GetWorker(workerID)
	if err != nil {
		return fmt.Errorf("worker %s not found: %w", workerID, err)
	}

	return worker.RegisterWorkflow(ctx, workflowName, handler)
}

// UnregisterWorkflow removes a workflow from a worker
func (m *Manager) UnregisterWorkflow(_ context.Context, workerID registry.ID, workflowName string) error {
	worker, err := m.GetWorker(workerID)
	if err != nil {
		return fmt.Errorf("worker %s not found: %w", workerID, err)
	}

	return worker.UnregisterWorkflow(workflowName)
}
