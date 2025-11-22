package worker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/supervisor"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
	"go.uber.org/zap"
)

// Worker wraps a Temporal SDK worker with lifecycle management
type Worker struct {
	id           registry.ID
	config       *api.WorkerConfig
	log          *zap.Logger
	resourceReg  resource.Registry
	interceptors []interceptor.WorkerInterceptor

	mu             sync.RWMutex
	ctx            context.Context
	worker         worker.Worker
	clientResource resource.Resource[any]
	closed         atomic.Bool
	cancel         context.CancelFunc
	activities     map[string]*ActivityRegistration
	funcRegistry   function.Registry
}

// ActivityRegistration represents a registered activity
type ActivityRegistration struct {
	Name     string
	Function registry.ID
	Local    bool
}

// NewWorker creates a new Worker instance
func NewWorker(
	logger *zap.Logger,
	id registry.ID,
	config *api.WorkerConfig,
	resourceReg resource.Registry,
	interceptors []interceptor.WorkerInterceptor,
) *Worker {
	return &Worker{
		id:           id,
		config:       config,
		log:          logger,
		resourceReg:  resourceReg,
		interceptors: interceptors,
		activities:   make(map[string]*ActivityRegistration),
	}
}

// Start implements supervisor.Service
func (w *Worker) Start(ctx context.Context) (<-chan any, error) {
	if w.closed.Load() {
		return nil, fmt.Errorf("worker is closed")
	}

	// Create status channel
	statusCh := make(chan any, 1)

	// Acquire client resource
	clientRes, err := w.resourceReg.Acquire(ctx, w.config.Client, resource.ModeNormal)
	if err != nil {
		statusCh <- supervisor.Failed
		return statusCh, fmt.Errorf("failed to acquire client: %w", err)
	}
	w.clientResource = clientRes

	// Get temporal client resource
	clientAny, err := clientRes.Get()
	if err != nil {
		clientRes.Release()
		statusCh <- supervisor.Failed
		return statusCh, fmt.Errorf("failed to get client: %w", err)
	}

	temporalRes, ok := clientAny.(api.ClientResource)
	if !ok {
		clientRes.Release()
		statusCh <- supervisor.Failed
		return statusCh, fmt.Errorf("invalid client type: expected temporal.ClientResource, got %T", clientAny)
	}

	temporalClient := temporalRes.Client

	// Apply task queue prefix from client
	taskQueue := temporalRes.GetTaskQueueName(w.config.TaskQueue)
	if temporalRes.TQPrefix != "" {
		w.log.Debug("applied task queue prefix",
			zap.String("original", w.config.TaskQueue),
			zap.String("prefixed", taskQueue))
	}

	// Get function registry from context
	w.funcRegistry = function.GetRegistry(ctx)
	if w.funcRegistry == nil {
		clientRes.Release()
		statusCh <- supervisor.Failed
		return statusCh, fmt.Errorf("function registry not found in context")
	}

	// Store application context for activity execution
	w.ctx = ctx

	// Create worker options
	options := worker.Options{
		MaxConcurrentActivityExecutionSize:      w.config.WorkerOptions.MaxConcurrentActivityExecutionSize,
		MaxConcurrentWorkflowTaskExecutionSize:  w.config.WorkerOptions.MaxConcurrentWorkflowTaskExecutionSize,
		MaxConcurrentLocalActivityExecutionSize: w.config.WorkerOptions.MaxConcurrentLocalActivityExecutionSize,
		MaxConcurrentSessionExecutionSize:       w.config.WorkerOptions.MaxConcurrentSessionExecutionSize,
		MaxConcurrentEagerActivityExecutionSize: w.config.WorkerOptions.MaxConcurrentEagerActivityExecutionSize,
		MaxConcurrentActivityTaskPollers:        w.config.WorkerOptions.MaxConcurrentActivityTaskPollers,
		MaxConcurrentWorkflowTaskPollers:        w.config.WorkerOptions.MaxConcurrentWorkflowTaskPollers,
		Interceptors:                            w.interceptors,
	}

	// Apply optional settings
	if w.config.WorkerOptions.WorkerActivitiesPerSecond > 0 {
		options.WorkerActivitiesPerSecond = w.config.WorkerOptions.WorkerActivitiesPerSecond
	}
	if w.config.WorkerOptions.WorkerLocalActivitiesPerSecond > 0 {
		options.WorkerLocalActivitiesPerSecond = w.config.WorkerOptions.WorkerLocalActivitiesPerSecond
	}
	if w.config.WorkerOptions.TaskQueueActivitiesPerSecond > 0 {
		options.TaskQueueActivitiesPerSecond = w.config.WorkerOptions.TaskQueueActivitiesPerSecond
	}
	if w.config.WorkerOptions.StickyScheduleToStartTimeout > 0 {
		options.StickyScheduleToStartTimeout = w.config.WorkerOptions.StickyScheduleToStartTimeout
	}
	if w.config.WorkerOptions.WorkerStopTimeout > 0 {
		options.WorkerStopTimeout = w.config.WorkerOptions.WorkerStopTimeout
	}
	if w.config.WorkerOptions.DeadlockDetectionTimeout > 0 {
		options.DeadlockDetectionTimeout = w.config.WorkerOptions.DeadlockDetectionTimeout
	}
	if w.config.WorkerOptions.MaxHeartbeatThrottleInterval > 0 {
		options.MaxHeartbeatThrottleInterval = w.config.WorkerOptions.MaxHeartbeatThrottleInterval
	}
	if w.config.WorkerOptions.DefaultHeartbeatThrottleInterval > 0 {
		options.DefaultHeartbeatThrottleInterval = w.config.WorkerOptions.DefaultHeartbeatThrottleInterval
	}
	if w.config.WorkerOptions.Identity != "" {
		options.Identity = w.config.WorkerOptions.Identity
	}
	if w.config.WorkerOptions.BuildID != "" {
		options.BuildID = w.config.WorkerOptions.BuildID
	}
	if w.config.WorkerOptions.UseBuildIDForVersioning {
		options.UseBuildIDForVersioning = w.config.WorkerOptions.UseBuildIDForVersioning
	}

	options.EnableLoggingInReplay = w.config.WorkerOptions.EnableLoggingInReplay
	options.EnableSessionWorker = w.config.WorkerOptions.EnableSessionWorker
	options.DisableWorkflowWorker = w.config.WorkerOptions.DisableWorkflowWorker
	options.LocalActivityWorkerOnly = w.config.WorkerOptions.LocalActivityWorkerOnly
	options.DisableEagerActivities = w.config.WorkerOptions.DisableEagerActivities
	options.DisableRegistrationAliasing = w.config.WorkerOptions.DisableRegistrationAliasing

	// Create Temporal SDK worker
	w.worker = worker.New(temporalClient, taskQueue, options)

	// Re-register all existing activities
	w.mu.RLock()
	for _, act := range w.activities {
		if act.Local {
			w.registerLocalActivity(ctx, act)
		} else {
			w.registerActivity(ctx, act)
		}
	}
	w.mu.RUnlock()

	// Start worker
	if err := w.worker.Start(); err != nil {
		clientRes.Release()
		statusCh <- supervisor.Failed
		return statusCh, fmt.Errorf("failed to start worker: %w", err)
	}

	w.log.Info("worker started",
		zap.String("task_queue", w.config.TaskQueue),
		zap.String("client", w.config.Client.String()),
		zap.Int("activities", len(w.activities)))

	statusCh <- supervisor.Running
	return statusCh, nil
}

// Stop implements supervisor.Service
func (w *Worker) Stop(ctx context.Context) error {
	if w.closed.Swap(true) {
		return nil
	}

	if w.cancel != nil {
		w.cancel()
	}

	if w.worker != nil {
		w.worker.Stop()
	}

	if w.clientResource != nil {
		w.clientResource.Release()
	}

	return nil
}

// RegisterActivity registers an activity with this worker
func (w *Worker) RegisterActivity(ctx context.Context, name string, funcID registry.ID) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.activities[name]; exists {
		return fmt.Errorf("activity %s already registered", name)
	}

	reg := &ActivityRegistration{
		Name:     name,
		Function: funcID,
	}

	w.activities[name] = reg

	// If worker is already running, register immediately
	if w.worker != nil {
		w.registerActivity(ctx, reg)
	}

	w.log.Debug("activity registered",
		zap.String("name", name),
		zap.String("function", funcID.String()))

	return nil
}

// RegisterLocalActivity registers a local activity with this worker
func (w *Worker) RegisterLocalActivity(ctx context.Context, name string, funcID registry.ID) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.activities[name]; exists {
		return fmt.Errorf("activity %s already registered", name)
	}

	reg := &ActivityRegistration{
		Name:     name,
		Function: funcID,
		Local:    true,
	}

	w.activities[name] = reg

	// If worker is already running, register immediately
	if w.worker != nil {
		w.registerLocalActivity(ctx, reg)
	}

	w.log.Debug("local activity registered",
		zap.String("name", name),
		zap.String("function", funcID.String()))

	return nil
}

// UnregisterActivity removes an activity from this worker
func (w *Worker) UnregisterActivity(name string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.activities[name]; !exists {
		return fmt.Errorf("activity %s not found", name)
	}

	delete(w.activities, name)

	w.log.Debug("activity unregistered", zap.String("name", name))
	return nil
}

// registerActivity registers an activity with the Temporal SDK worker
func (w *Worker) registerActivity(ctx context.Context, reg *ActivityRegistration) {
	handler := w.createActivityHandler(ctx, reg.Function)
	w.worker.RegisterActivityWithOptions(handler, activity.RegisterOptions{
		Name: reg.Name,
	})
}

// registerLocalActivity registers a local activity with the Temporal SDK worker
func (w *Worker) registerLocalActivity(ctx context.Context, reg *ActivityRegistration) {
	handler := w.createActivityHandler(ctx, reg.Function)
	w.worker.RegisterActivityWithOptions(handler, activity.RegisterOptions{
		Name:                          reg.Name,
		DisableAlreadyRegisteredCheck: false,
		SkipInvalidStructFunctions:    false,
	})
}

// createActivityHandler creates an activity handler that executes a function through the registry
func (w *Worker) createActivityHandler(ctx context.Context, funcID registry.ID) func(context.Context, []payload.Payload) ([]payload.Payload, error) {
	return func(activityCtx context.Context, input []payload.Payload) ([]payload.Payload, error) {
		info := activity.GetInfo(activityCtx)
		w.log.Debug("executing",
			zap.String("type", info.ActivityType.Name),
			zap.String("id", info.ActivityID),
			zap.String("workflow_id", info.WorkflowExecution.ID),
			zap.Int32("attempt", info.Attempt),
		)

		// Use application context (has wippy components) but respect activity cancellation
		execCtx := w.ctx
		if activityCtx.Done() != nil {
			var cancel context.CancelFunc
			execCtx, cancel = context.WithCancel(w.ctx)
			defer cancel()

			go func() {
				<-activityCtx.Done()
				cancel()
			}()
		}

		result, err := w.funcRegistry.Call(execCtx, runtime.Task{
			ID:       funcID,
			Payloads: payload.Payloads(input),
			Context: []ctxapi.Pair{
				{Key: api.ActivityContextKey(), Value: activityCtx},
			},
		})
		if err != nil {
			w.log.Error("execution failed",
				zap.String("type", info.ActivityType.Name),
				zap.Error(err),
			)
			return nil, err
		}

		if result == nil {
			return nil, fmt.Errorf("received nil result from function executor")
		}
		if result.Error != nil {
			w.log.Error("execution failed",
				zap.String("type", info.ActivityType.Name),
				zap.Error(result.Error),
			)
			return nil, result.Error
		}

		w.log.Debug("completed",
			zap.String("type", info.ActivityType.Name),
		)

		return []payload.Payload{result.Value}, nil
	}
}
