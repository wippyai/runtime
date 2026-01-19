package worker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/supervisor"
	temporalerrors "github.com/wippyai/runtime/service/temporal/errors"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
)

const (
	relaySendActivityName = "__wippy_relay_send"
)

var _ supervisor.Service = (*Worker)(nil)

// Worker wraps a Temporal SDK worker with lifecycle management
type Worker struct {
	id           registry.ID
	config       *api.WorkerConfig
	log          *zap.Logger
	resourceReg  resource.Registry
	envReg       env.Registry
	interceptors []interceptor.WorkerInterceptor

	mu             sync.RWMutex
	ctx            context.Context
	worker         worker.Worker
	clientResource resource.Resource[any]
	closed         atomic.Bool
	cancel         context.CancelFunc
	activities     map[string]*activityRegistration
	workflows      map[string]*workflowRegistration
	funcRegistry   function.Registry
}

// activityRegistration represents a registered activity
type activityRegistration struct {
	name     string
	function registry.ID
	local    bool
}

// workflowRegistration represents a registered workflow
type workflowRegistration struct {
	name    string
	handler any
}

// NewWorker creates a new Worker instance
func NewWorker(
	logger *zap.Logger,
	id registry.ID,
	config *api.WorkerConfig,
	resourceReg resource.Registry,
	envReg env.Registry,
	interceptors []interceptor.WorkerInterceptor,
) *Worker {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Worker{
		id:           id,
		config:       config,
		log:          logger,
		resourceReg:  resourceReg,
		envReg:       envReg,
		interceptors: interceptors,
		activities:   make(map[string]*activityRegistration),
		workflows:    make(map[string]*workflowRegistration),
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
		statusCh <- supervisor.StatusFailed
		return statusCh, fmt.Errorf("failed to acquire client: %w", err)
	}
	w.clientResource = clientRes

	// Get temporal client resource
	clientAny, err := clientRes.Get()
	if err != nil {
		clientRes.Release()
		statusCh <- supervisor.StatusFailed
		return statusCh, fmt.Errorf("failed to get client: %w", err)
	}

	temporalRes, ok := clientAny.(api.ClientResource)
	if !ok {
		clientRes.Release()
		statusCh <- supervisor.StatusFailed
		return statusCh, fmt.Errorf("invalid client type: expected temporal.ClientResource, got %T", clientAny)
	}

	temporalClient := temporalRes.Client
	if temporalClient == nil {
		clientRes.Release()
		statusCh <- supervisor.StatusFailed
		return statusCh, fmt.Errorf("temporal client is nil")
	}

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
		statusCh <- supervisor.StatusFailed
		return statusCh, fmt.Errorf("function registry not found in context")
	}

	// Store application context with client ID for workflow peer routing
	w.ctx = api.WithClientID(ctx, w.config.Client.String())

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
	if w.config.WorkerOptions.UseVersioning {
		buildID := w.config.WorkerOptions.BuildID
		if buildID == "" && w.config.WorkerOptions.BuildIDEnv != "" && w.envReg != nil {
			if val, err := w.envReg.Get(ctx, w.config.WorkerOptions.BuildIDEnv); err == nil {
				buildID = val
			}
		}
		options.DeploymentOptions = worker.DeploymentOptions{
			UseVersioning: true,
			Version: worker.WorkerDeploymentVersion{
				DeploymentName: w.config.WorkerOptions.DeploymentName,
				BuildID:        buildID,
			},
			DefaultVersioningBehavior: mapVersioningBehavior(w.config.WorkerOptions.DefaultVersioningBehavior),
		}
	}

	options.EnableLoggingInReplay = w.config.WorkerOptions.EnableLoggingInReplay
	options.EnableSessionWorker = w.config.WorkerOptions.EnableSessionWorker
	options.DisableWorkflowWorker = w.config.WorkerOptions.DisableWorkflowWorker
	options.LocalActivityWorkerOnly = w.config.WorkerOptions.LocalActivityWorkerOnly
	options.DisableEagerActivities = w.config.WorkerOptions.DisableEagerActivities
	options.DisableRegistrationAliasing = w.config.WorkerOptions.DisableRegistrationAliasing

	// Create Temporal SDK worker
	w.worker = worker.New(temporalClient, taskQueue, options)

	// Register system activities
	w.registerRelaySendActivity(ctx)

	// Re-register all existing activities and workflows
	w.mu.RLock()
	for _, act := range w.activities {
		if act.local {
			w.registerLocalActivity(ctx, act)
		} else {
			w.registerActivity(ctx, act)
		}
	}
	for _, wf := range w.workflows {
		w.registerWorkflow(ctx, wf)
	}
	w.mu.RUnlock()

	// Start worker
	if err := w.worker.Start(); err != nil {
		clientRes.Release()
		statusCh <- supervisor.StatusFailed
		return statusCh, fmt.Errorf("failed to start worker: %w", err)
	}

	w.log.Info("worker started",
		zap.String("task_queue", w.config.TaskQueue),
		zap.String("client", w.config.Client.String()),
		zap.Int("activities", len(w.activities)),
		zap.Int("workflows", len(w.workflows)))

	statusCh <- supervisor.StatusRunning
	return statusCh, nil
}

// Stop implements supervisor.Service
func (w *Worker) Stop(ctx context.Context) error {
	if w.closed.Swap(true) {
		return nil
	}

	w.log.Info("stopping temporal worker", zap.String("id", w.id.String()))

	if w.cancel != nil {
		w.cancel()
	}

	if w.worker != nil {
		// Stop worker in goroutine to respect context timeout
		done := make(chan struct{})
		go func() {
			w.worker.Stop()
			close(done)
		}()

		select {
		case <-done:
			w.log.Debug("temporal worker stopped gracefully")
		case <-ctx.Done():
			w.log.Warn("timeout waiting for temporal worker to stop")
		}
	}

	if w.clientResource != nil {
		w.clientResource.Release()
	}

	w.log.Info("temporal worker stopped", zap.String("id", w.id.String()))
	return nil
}

// RegisterActivity registers an activity with this worker
func (w *Worker) RegisterActivity(ctx context.Context, name string, funcID registry.ID) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.activities[name]; exists {
		return fmt.Errorf("activity %s already registered", name)
	}

	reg := &activityRegistration{
		name:     name,
		function: funcID,
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

	reg := &activityRegistration{
		name:     name,
		function: funcID,
		local:    true,
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

// RegisterWorkflow registers a workflow with this worker
func (w *Worker) RegisterWorkflow(ctx context.Context, name string, handler any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.workflows[name]; exists {
		return fmt.Errorf("workflow %s already registered", name)
	}

	reg := &workflowRegistration{
		name:    name,
		handler: handler,
	}

	w.workflows[name] = reg

	// If worker is already running, register immediately
	if w.worker != nil {
		w.registerWorkflow(ctx, reg)
	}

	w.log.Debug("workflow registered",
		zap.String("name", name))

	return nil
}

// UnregisterWorkflow removes a workflow from this worker
func (w *Worker) UnregisterWorkflow(name string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.workflows[name]; !exists {
		return fmt.Errorf("workflow %s not found", name)
	}

	delete(w.workflows, name)

	w.log.Debug("workflow unregistered", zap.String("name", name))
	return nil
}

// registerActivity registers an activity with the Temporal SDK worker
func (w *Worker) registerActivity(ctx context.Context, reg *activityRegistration) {
	handler := w.createActivityHandler(ctx, reg.function)
	w.worker.RegisterActivityWithOptions(handler, activity.RegisterOptions{
		Name: reg.name,
	})
}

// registerLocalActivity registers a local activity with the Temporal SDK worker
func (w *Worker) registerLocalActivity(ctx context.Context, reg *activityRegistration) {
	handler := w.createActivityHandler(ctx, reg.function)
	w.worker.RegisterActivityWithOptions(handler, activity.RegisterOptions{
		Name: reg.name,
	})
}

// registerWorkflow registers a workflow with the Temporal SDK worker
func (w *Worker) registerWorkflow(_ context.Context, reg *workflowRegistration) {
	// Support DefinitionFactory with WithContext method
	handler := reg.handler
	if cwf, ok := handler.(interface{ WithContext(context.Context) any }); ok {
		handler = cwf.WithContext(w.ctx)
	}

	// Register directly with Temporal SDK
	w.worker.RegisterWorkflowWithOptions(handler, workflow.RegisterOptions{
		Name: reg.name,
	})
}

// registerRelaySendActivity registers the system activity for routing messages to local processes.
func (w *Worker) registerRelaySendActivity(_ context.Context) {
	handler := func(activityCtx context.Context, pkg *relay.Package) error {
		// Get router from application context
		router := relay.GetRouter(w.ctx)
		if router == nil {
			return fmt.Errorf("relay router not available")
		}

		w.log.Debug("routing message to local process",
			zap.String("from", pkg.Source.String()),
			zap.String("to", pkg.Target.String()),
			zap.Int("messages", len(pkg.Messages)))

		return router.Send(pkg)
	}

	w.worker.RegisterActivityWithOptions(handler, activity.RegisterOptions{
		Name: relaySendActivityName,
	})
}

// createActivityHandler creates an activity handler that executes a function through the registry
func (w *Worker) createActivityHandler(_ context.Context, funcID registry.ID) func(context.Context, []payload.Payload) ([]payload.Payload, error) {
	return func(activityCtx context.Context, input []payload.Payload) ([]payload.Payload, error) {
		info := activity.GetInfo(activityCtx)
		w.log.Debug("executing activity",
			zap.String("type", info.ActivityType.Name),
			zap.String("function", funcID.String()),
			zap.String("workflow_id", info.WorkflowExecution.ID),
			zap.Int32("attempt", info.Attempt),
			zap.Int("payloads", len(input)),
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
			Payloads: input,
			Context: []ctxapi.Pair{
				{Key: api.ActivityContextKey(), Value: activityCtx},
			},
		})
		if err != nil {
			w.log.Error("activity call failed",
				zap.String("type", info.ActivityType.Name),
				zap.String("function", funcID.String()),
				zap.Error(err),
			)
			// Convert to Temporal ApplicationError preserving error kind and retryability
			return nil, temporalerrors.ToApplicationError(err)
		}

		if result == nil {
			return nil, fmt.Errorf("received nil result from function executor")
		}
		if result.Error != nil {
			w.log.Error("activity execution failed",
				zap.String("type", info.ActivityType.Name),
				zap.String("function", funcID.String()),
				zap.Error(result.Error),
			)
			// Convert to Temporal ApplicationError preserving error kind and retryability
			return nil, temporalerrors.ToApplicationError(result.Error)
		}

		w.log.Debug("activity completed",
			zap.String("type", info.ActivityType.Name),
			zap.String("function", funcID.String()),
		)

		return []payload.Payload{result.Value}, nil
	}
}

// mapVersioningBehavior converts API versioning behavior to SDK type
func mapVersioningBehavior(behavior api.VersioningBehavior) workflow.VersioningBehavior {
	switch behavior {
	case api.VersioningBehaviorPinned:
		return workflow.VersioningBehaviorPinned
	case api.VersioningBehaviorAutoUpgrade:
		return workflow.VersioningBehaviorAutoUpgrade
	default:
		return workflow.VersioningBehaviorUnspecified
	}
}
