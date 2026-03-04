// SPDX-License-Identifier: MPL-2.0

package worker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/uniqid"
	temporalerrors "github.com/wippyai/runtime/service/temporal/errors"
	temporalprop "github.com/wippyai/runtime/service/temporal/propagator"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
)

const (
	relaySendActivityName = "__wippy_relay_send"
)

var (
	_ supervisor.Service = (*Worker)(nil)
	_ relay.Receiver     = (*Worker)(nil)
	_ process.Host       = (*Worker)(nil)
)

// Worker wraps a Temporal SDK worker with lifecycle management
type Worker struct {
	pidGen         process.PIDGenerator
	temporalClient client.Client
	ctx            context.Context
	funcRegistry   function.Registry
	resourceReg    resource.Registry
	envReg         env.Registry
	worker         worker.Worker
	dtt            payload.Transcoder
	clientResource resource.Resource[any]
	cancel         context.CancelFunc
	log            *zap.Logger
	activities     map[string]*activityRegistration
	workflows      map[string]*workflowRegistration
	config         *api.WorkerConfig
	runtimeState   atomic.Pointer[workerRuntime]
	id             registry.ID
	clientNodeID   pid.NodeID
	workflowPrefix string
	taskQueue      string
	interceptors   []interceptor.WorkerInterceptor
	mu             sync.RWMutex
	closed         atomic.Bool
}

// workerRuntime is the published runtime snapshot used by hot paths.
// It is replaced atomically on Start/Stop boundaries.
type workerRuntime struct {
	worker         worker.Worker
	temporalClient client.Client
	funcRegistry   function.Registry
	clientResource resource.Resource[any]
	ctx            context.Context
	taskQueue      string
}

// activityRegistration represents a registered activity
type activityRegistration struct {
	name     string
	function registry.ID
	local    bool
}

// workflowRegistration represents a registered workflow
type workflowRegistration struct {
	handler any
	name    string
}

// newWorker creates a new Worker instance (use WorkerBuilder in production code).
func newWorker(
	logger *zap.Logger,
	id registry.ID,
	config *api.WorkerConfig,
	resourceReg resource.Registry,
	envReg env.Registry,
	interceptors []interceptor.WorkerInterceptor,
	dtt payload.Transcoder,
) *Worker {
	if logger == nil {
		logger = zap.NewNop()
	}
	clientNodeID := config.Client.String()
	pidGen := uniqid.NewPIDGenerator(uniqid.NewGenerator(), clientNodeID)
	return &Worker{
		id:             id,
		config:         config,
		log:            logger,
		resourceReg:    resourceReg,
		envReg:         envReg,
		interceptors:   interceptors,
		activities:     make(map[string]*activityRegistration),
		workflows:      make(map[string]*workflowRegistration),
		clientNodeID:   clientNodeID,
		pidGen:         pidGen,
		dtt:            dtt,
		workflowPrefix: uuid.NewString(),
	}
}

// Start implements supervisor.Service
func (w *Worker) Start(ctx context.Context) (<-chan any, error) {
	if w.closed.Load() {
		return nil, fmt.Errorf("worker is closed")
	}

	// Create status channel
	statusCh := make(chan any, 1)

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.runtimeState.Load() != nil {
		statusCh <- supervisor.StatusFailed
		return statusCh, fmt.Errorf("worker is already started")
	}

	// Acquire client resource
	clientRes, err := w.resourceReg.Acquire(ctx, w.config.Client, resource.ModeNormal)
	if err != nil {
		statusCh <- supervisor.StatusFailed
		return statusCh, fmt.Errorf("failed to acquire client: %w", err)
	}

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
	funcRegistry := function.GetRegistry(ctx)
	if funcRegistry == nil {
		clientRes.Release()
		statusCh <- supervisor.StatusFailed
		return statusCh, fmt.Errorf("function registry not found in context")
	}

	// Store application context with Temporal routing identity.
	// Client ID identifies the Temporal node; worker ID identifies host-level PID identity.
	appCtx := api.WithClientID(ctx, w.config.Client.String())
	appCtx = api.WithWorkerID(appCtx, w.id.String())

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
	sdkWorker := worker.New(temporalClient, taskQueue, options)
	runtimeState := &workerRuntime{
		worker:         sdkWorker,
		temporalClient: temporalClient,
		funcRegistry:   funcRegistry,
		clientResource: clientRes,
		ctx:            appCtx,
		taskQueue:      taskQueue,
	}

	// Keep legacy fields synchronized for tests that manually wire dependencies.
	w.clientResource = clientRes
	w.temporalClient = temporalClient
	w.taskQueue = taskQueue
	w.funcRegistry = funcRegistry
	w.ctx = appCtx
	w.worker = sdkWorker

	// Register system activities
	w.registerRelaySendActivity(runtimeState)

	// Re-register all existing activities and workflows
	for _, act := range w.activities {
		if act.local {
			w.registerLocalActivity(runtimeState, act)
		} else {
			w.registerActivity(runtimeState, act)
		}
	}
	for _, wf := range w.workflows {
		w.registerWorkflow(runtimeState, wf)
	}

	// Start worker
	if err := runtimeState.worker.Start(); err != nil {
		w.clientResource = nil
		w.temporalClient = nil
		w.taskQueue = ""
		w.funcRegistry = nil
		w.ctx = nil
		w.worker = nil
		clientRes.Release()
		statusCh <- supervisor.StatusFailed
		return statusCh, fmt.Errorf("failed to start worker: %w", err)
	}

	w.runtimeState.Store(runtimeState)

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

	w.mu.Lock()
	runtimeState := w.runtimeState.Load()
	w.runtimeState.Store(nil)

	cancel := w.cancel
	w.cancel = nil

	workerInstance := w.worker
	if runtimeState != nil && runtimeState.worker != nil {
		workerInstance = runtimeState.worker
	}

	clientRes := w.clientResource
	if runtimeState != nil && runtimeState.clientResource != nil {
		clientRes = runtimeState.clientResource
	}

	w.worker = nil
	w.clientResource = nil
	w.temporalClient = nil
	w.funcRegistry = nil
	w.ctx = nil
	w.taskQueue = ""
	w.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if workerInstance != nil {
		// Stop worker in goroutine to respect context timeout
		done := make(chan struct{})
		go func() {
			workerInstance.Stop()
			close(done)
		}()

		select {
		case <-done:
			w.log.Debug("temporal worker stopped gracefully")
		case <-ctx.Done():
			w.log.Warn("timeout waiting for temporal worker to stop")
		}
	}

	if clientRes != nil {
		clientRes.Release()
	}

	w.log.Info("temporal worker stopped", zap.String("id", w.id.String()))
	return nil
}

// RegisterActivity registers an activity with this worker
func (w *Worker) RegisterActivity(_ context.Context, name string, funcID registry.ID) error {
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
	if runtimeState := w.runtimeState.Load(); runtimeState != nil && runtimeState.worker != nil {
		w.registerActivity(runtimeState, reg)
	}

	w.log.Debug("activity registered",
		zap.String("name", name),
		zap.String("function", funcID.String()))

	return nil
}

// RegisterLocalActivity registers a local activity with this worker
func (w *Worker) RegisterLocalActivity(_ context.Context, name string, funcID registry.ID) error {
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
	if runtimeState := w.runtimeState.Load(); runtimeState != nil && runtimeState.worker != nil {
		w.registerLocalActivity(runtimeState, reg)
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
func (w *Worker) RegisterWorkflow(_ context.Context, name string, handler any) error {
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
	if runtimeState := w.runtimeState.Load(); runtimeState != nil && runtimeState.worker != nil {
		w.registerWorkflow(runtimeState, reg)
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
func (w *Worker) registerActivity(runtimeState *workerRuntime, reg *activityRegistration) {
	handler := w.createActivityHandler(runtimeState, reg.function)
	runtimeState.worker.RegisterActivityWithOptions(handler, activity.RegisterOptions{
		Name: reg.name,
	})
}

// registerLocalActivity registers a local activity with the Temporal SDK worker
func (w *Worker) registerLocalActivity(runtimeState *workerRuntime, reg *activityRegistration) {
	handler := w.createActivityHandler(runtimeState, reg.function)
	runtimeState.worker.RegisterActivityWithOptions(handler, activity.RegisterOptions{
		Name: reg.name,
	})
}

// registerWorkflow registers a workflow with the Temporal SDK worker
func (w *Worker) registerWorkflow(runtimeState *workerRuntime, reg *workflowRegistration) {
	// Support DefinitionFactory with WithContext method
	handler := reg.handler
	if cwf, ok := handler.(interface{ WithContext(context.Context) any }); ok {
		handler = cwf.WithContext(runtimeState.ctx)
	}

	// Register directly with Temporal SDK
	runtimeState.worker.RegisterWorkflowWithOptions(handler, workflow.RegisterOptions{
		Name: reg.name,
	})
}

// registerRelaySendActivity registers the system activity for routing messages to local processes.
func (w *Worker) registerRelaySendActivity(runtimeState *workerRuntime) {
	handler := func(_ context.Context, pkg *relay.Package) error {
		// Get router from application context
		router := relay.GetRouter(runtimeState.ctx)
		if router == nil {
			return fmt.Errorf("relay router not available")
		}

		w.log.Debug("routing message to local process",
			zap.String("from", pkg.Source.String()),
			zap.String("to", pkg.Target.String()),
			zap.Int("messages", len(pkg.Messages)))

		return router.Send(pkg)
	}

	runtimeState.worker.RegisterActivityWithOptions(handler, activity.RegisterOptions{
		Name: relaySendActivityName,
	})
}

// createActivityHandler creates an activity handler that executes a function through the registry
func (w *Worker) createActivityHandler(runtimeState *workerRuntime, funcID registry.ID) func(context.Context, []payload.Payload) ([]payload.Payload, error) {
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
		execCtx := runtimeState.ctx
		if activityCtx.Done() != nil {
			var cancel context.CancelFunc
			execCtx, cancel = context.WithCancel(runtimeState.ctx)
			defer cancel()

			go func() {
				<-activityCtx.Done()
				cancel()
			}()
		}

		execCtx, release, err := temporalprop.MergeActivityContext(execCtx, activityCtx)
		if err != nil {
			w.log.Warn("failed to merge activity context", zap.Error(err))
		} else {
			defer release()
		}

		result, err := runtimeState.funcRegistry.Call(execCtx, runtime.Task{
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

// loadRuntime returns the currently running snapshot.
// It falls back to legacy fields for tests that manually configure a worker.
func (w *Worker) loadRuntime() *workerRuntime {
	if runtimeState := w.runtimeState.Load(); runtimeState != nil {
		return runtimeState
	}

	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.temporalClient == nil {
		return nil
	}

	ctx := w.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	taskQueue := w.taskQueue
	if taskQueue == "" && w.config != nil {
		taskQueue = w.config.TaskQueue
	}

	return &workerRuntime{
		worker:         w.worker,
		temporalClient: w.temporalClient,
		funcRegistry:   w.funcRegistry,
		clientResource: w.clientResource,
		ctx:            ctx,
		taskQueue:      taskQueue,
	}
}

// Send implements relay.Receiver by translating relay packages to Temporal signals.
func (w *Worker) Send(pkg *relay.Package) error {
	if w.closed.Load() {
		return fmt.Errorf("worker is closed")
	}

	runtimeState := w.loadRuntime()
	if runtimeState == nil || runtimeState.temporalClient == nil {
		return fmt.Errorf("temporal client not available")
	}

	workflowID := pkg.Target.UniqID
	if workflowID == "" {
		return fmt.Errorf("target workflow ID is empty")
	}

	for _, msg := range pkg.Messages {
		if msg.Topic == "" {
			continue
		}

		// Pass payloads directly - client's DataConverter handles format conversion
		var signalArg any
		if len(msg.Payloads) == 1 {
			signalArg = msg.Payloads[0]
		} else if len(msg.Payloads) > 1 {
			signalArg = msg.Payloads
		}

		w.log.Debug("sending signal to workflow",
			zap.String("workflow_id", workflowID),
			zap.String("signal", msg.Topic),
			zap.Int("payloads", len(msg.Payloads)))

		ctx := runtimeState.ctx
		var frame ctxapi.FrameContext
		if pkg.Source.Node != "" || pkg.Source.Host != "" || pkg.Source.UniqID != "" {
			ctx, frame = ctxapi.ForkFrameContext(ctx)
			values, err := ctxapi.GetOrCreateValues(ctx)
			if err == nil {
				values.Set(temporalprop.SignalFromValueKey, pkg.Source.String())
			}
			ctx = withContextValuesFallback(ctx)
		}

		err := runtimeState.temporalClient.SignalWorkflow(ctx, workflowID, "", msg.Topic, signalArg)
		if frame != nil {
			ctxapi.ReleaseFrameContext(frame)
		}
		if err != nil {
			w.log.Error("failed to signal workflow",
				zap.String("workflow_id", workflowID),
				zap.String("signal", msg.Topic),
				zap.Error(err))
			return err
		}
	}

	return nil
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
