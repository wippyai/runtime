// SPDX-License-Identifier: MPL-2.0

package worker

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/entry"
	"go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
)

var _ registry.EntryListener = (*Manager)(nil)

// Manager handles Temporal worker configuration and lifecycle
type Manager struct {
	dtt                payload.Transcoder
	bus                event.Bus
	res                resource.Registry
	envReg             env.Registry
	factory            Factory
	log                *zap.Logger
	configs            map[registry.ID]*api.WorkerConfig
	services           map[registry.ID]*Worker
	workerInterceptors []interceptor.WorkerInterceptor
	mu                 sync.RWMutex
}

// ManagerOption configures a Manager instance
type ManagerOption func(*Manager)

// WithLogger sets the logger for the Manager
func WithLogger(logger *zap.Logger) ManagerOption {
	return func(m *Manager) {
		m.log = logger
	}
}

// WithTranscoder sets the payload transcoder for the Manager
func WithTranscoder(transcoder payload.Transcoder) ManagerOption {
	return func(m *Manager) {
		m.dtt = transcoder
	}
}

// WithEventBus sets the event bus for the Manager
func WithEventBus(bus event.Bus) ManagerOption {
	return func(m *Manager) {
		m.bus = bus
	}
}

// WithResourceRegistry sets the resource registry for the Manager
func WithResourceRegistry(reg resource.Registry) ManagerOption {
	return func(m *Manager) {
		m.res = reg
	}
}

// WithEnvRegistry sets the environment registry for the Manager
func WithEnvRegistry(reg env.Registry) ManagerOption {
	return func(m *Manager) {
		m.envReg = reg
	}
}

// WithInterceptors sets the worker interceptors for the Manager
func WithInterceptors(interceptors []interceptor.WorkerInterceptor) ManagerOption {
	return func(m *Manager) {
		m.workerInterceptors = interceptors
	}
}

// NewManager creates a new worker manager instance with functional options
func NewManager(opts ...ManagerOption) (*Manager, error) {
	m := &Manager{
		configs:  make(map[registry.ID]*api.WorkerConfig),
		services: make(map[registry.ID]*Worker),
	}

	for _, opt := range opts {
		opt(m)
	}

	if m.log == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if m.dtt == nil {
		return nil, fmt.Errorf("transcoder is required")
	}
	if m.bus == nil {
		return nil, fmt.Errorf("event bus is required")
	}
	if m.res == nil {
		return nil, fmt.Errorf("resource registry is required")
	}

	if m.factory == nil {
		m.factory = NewDefaultWorkerFactory(m.envReg, m.workerInterceptors, m.dtt)
	}

	return m, nil
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
	ensureClientDependency(cfg)

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

	// Send task queue registration event
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

	// Register worker as host for process.spawn
	m.bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostRegister,
		Path:   id.String(),
		Data:   service,
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
	ensureClientDependency(cfg)

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

// ensureClientDependency ensures the client is in the lifecycle dependencies.
func ensureClientDependency(cfg *api.WorkerConfig) {
	clientStr := cfg.Client.String()
	deps := cfg.Lifecycle.RequiredServices()
	for _, dep := range deps {
		if dep == clientStr {
			cfg.Lifecycle.Requires = deps
			return
		}
	}
	deps = append(deps, clientStr)
	cfg.Lifecycle.Requires = deps
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

	// Unregister host
	m.bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostDelete,
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
