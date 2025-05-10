package temporal

import (
	"context"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/service/temporal"
	"github.com/ponyruntime/pony/internal/config"
	"go.temporal.io/sdk/worker"
	"go.uber.org/zap"
)

// WorkerManager handles registry entries for Temporal workers (task queues)
type WorkerManager struct {
	log *zap.Logger
	dtt payload.Transcoder
	bus event.Bus
}

// NewWorkerManager creates a new manager for Temporal worker registry entries
func NewWorkerManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *WorkerManager {
	return &WorkerManager{
		log: log.With(zap.String("component", "temporal_worker_manager")),
		dtt: dtt,
		bus: bus,
	}
}

// Add implements registry.EntryListener
func (m *WorkerManager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != temporal.KindWorker {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	// Decode and initialize configuration
	cfg, err := config.DecodeAndInitConfig[temporal.WorkerConfig](m.dtt, entry)
	if err != nil {
		return fmt.Errorf("failed to decode worker config: %w", err)
	}

	// Create task queue registration from the config
	registration := m.createTaskQueueRegistration(entry.ID, cfg)

	// Send registration event to Temporal system
	m.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.TaskQueueRegister,
		Path:   entry.ID.String(),
		Data:   registration,
	})

	m.log.Info("temporal worker registered",
		zap.String("id", entry.ID.String()),
		zap.String("task_queue", cfg.TaskQueue),
		zap.String("client", cfg.Client.String()))

	return nil
}

// Update implements registry.EntryListener
func (m *WorkerManager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != temporal.KindWorker {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	// Decode and initialize configuration
	cfg, err := config.DecodeAndInitConfig[temporal.WorkerConfig](m.dtt, entry)
	if err != nil {
		return fmt.Errorf("failed to decode worker config: %w", err)
	}

	// Create task queue registration from the config
	registration := m.createTaskQueueRegistration(entry.ID, cfg)

	// Send update event to Temporal system
	m.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.TaskQueueUpdate,
		Path:   entry.ID.String(),
		Data:   registration,
	})

	m.log.Info("temporal worker updated",
		zap.String("id", entry.ID.String()),
		zap.String("task_queue", cfg.TaskQueue),
		zap.String("client", cfg.Client.String()))

	return nil
}

// Delete implements registry.EntryListener
func (m *WorkerManager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != temporal.KindWorker {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	// Create deletion request
	deletion := &temporal.TaskQueueDeletion{
		TaskQueue: entry.ID,
	}

	// Send delete event to Temporal system
	m.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.TaskQueueDelete,
		Path:   entry.ID.String(),
		Data:   deletion,
	})

	m.log.Info("temporal worker deleted",
		zap.String("id", entry.ID.String()))

	return nil
}

// createTaskQueueRegistration converts a WorkerConfig to a TaskQueueRegistration
func (m *WorkerManager) createTaskQueueRegistration(id registry.ID, cfg *temporal.WorkerConfig) *temporal.TaskQueueRegistration {
	options := worker.Options{
		MaxConcurrentActivityExecutionSize:      cfg.WorkerOptions.MaxConcurrentActivityExecutionSize,
		MaxConcurrentWorkflowTaskExecutionSize:  cfg.WorkerOptions.MaxConcurrentWorkflowTaskExecutionSize,
		MaxConcurrentLocalActivityExecutionSize: cfg.WorkerOptions.MaxConcurrentLocalActivityExecutionSize,
		MaxConcurrentSessionExecutionSize:       cfg.WorkerOptions.MaxConcurrentSessionExecutionSize,
		MaxConcurrentEagerActivityExecutionSize: cfg.WorkerOptions.MaxConcurrentEagerActivityExecutionSize,

		MaxConcurrentActivityTaskPollers: cfg.WorkerOptions.MaxConcurrentActivityTaskPollers,
		MaxConcurrentWorkflowTaskPollers: cfg.WorkerOptions.MaxConcurrentWorkflowTaskPollers,

		WorkerActivitiesPerSecond:      cfg.WorkerOptions.WorkerActivitiesPerSecond,
		WorkerLocalActivitiesPerSecond: cfg.WorkerOptions.WorkerLocalActivitiesPerSecond,
		TaskQueueActivitiesPerSecond:   cfg.WorkerOptions.TaskQueueActivitiesPerSecond,

		EnableLoggingInReplay:       cfg.WorkerOptions.EnableLoggingInReplay,
		EnableSessionWorker:         cfg.WorkerOptions.EnableSessionWorker,
		DisableWorkflowWorker:       cfg.WorkerOptions.DisableWorkflowWorker,
		LocalActivityWorkerOnly:     cfg.WorkerOptions.LocalActivityWorkerOnly,
		DisableEagerActivities:      cfg.WorkerOptions.DisableEagerActivities,
		DisableRegistrationAliasing: cfg.WorkerOptions.DisableRegistrationAliasing,

		Identity:                cfg.WorkerOptions.Identity,
		BuildID:                 cfg.WorkerOptions.BuildID,
		UseBuildIDForVersioning: cfg.WorkerOptions.UseBuildIDForVersioning,
	}

	// Parse duration fields from string to time.Duration
	if cfg.WorkerOptions.StickyScheduleToStartTimeout != "" {
		if d, err := time.ParseDuration(cfg.WorkerOptions.StickyScheduleToStartTimeout); err == nil {
			options.StickyScheduleToStartTimeout = d
		}
	}

	if cfg.WorkerOptions.WorkerStopTimeout != "" {
		if d, err := time.ParseDuration(cfg.WorkerOptions.WorkerStopTimeout); err == nil {
			options.WorkerStopTimeout = d
		}
	}

	if cfg.WorkerOptions.DeadlockDetectionTimeout != "" {
		if d, err := time.ParseDuration(cfg.WorkerOptions.DeadlockDetectionTimeout); err == nil {
			options.DeadlockDetectionTimeout = d
		}
	}

	if cfg.WorkerOptions.MaxHeartbeatThrottleInterval != "" {
		if d, err := time.ParseDuration(cfg.WorkerOptions.MaxHeartbeatThrottleInterval); err == nil {
			options.MaxHeartbeatThrottleInterval = d
		}
	}

	if cfg.WorkerOptions.DefaultHeartbeatThrottleInterval != "" {
		if d, err := time.ParseDuration(cfg.WorkerOptions.DefaultHeartbeatThrottleInterval); err == nil {
			options.DefaultHeartbeatThrottleInterval = d
		}
	}

	return &temporal.TaskQueueRegistration{
		ID:        id,
		Client:    cfg.Client,
		TaskQueue: cfg.TaskQueue,
		Options:   options,
		Lifecycle: cfg.Lifecycle,
	}
}
