package temporal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/service/temporal"
	"github.com/ponyruntime/pony/api/topology"
	clientpkg "github.com/ponyruntime/pony/service/temporal/client"

	tmcli "go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

// Command types for host operations
const (
	CmdRegisterWF   = "register_workflow"
	CmdDeleteWF     = "delete_workflow_by_name"
	CmdRegisterAct  = "register_activity"
	CmdDeleteAct    = "delete_activity_by_name"
	CmdRebuild      = "rebuild"
	CmdWorkerFailed = "worker_failed"
)

// workerState holds all worker-related state, allowing atomic updates
type workerState struct {
	worker    Worker
	interrupt chan interface{}
	done      chan struct{}
}

// WorkerHost implements the WorkerHostAPI interface for Temporal task queues
type WorkerHost struct {
	id       registry.ID
	config   *temporal.TaskQueueRegistration
	log      *zap.Logger
	ctx      context.Context
	statusCh chan any
	mu       sync.RWMutex

	// Client is acquired from resources when needed
	clientResource resource.Resource[any]
	client         atomic.Pointer[tmcli.Client]
	clientPrefix   string

	// Command channel for worker operations
	cmdCh    chan command
	hostDone chan struct{}
	running  atomic.Bool

	// Worker handling - all worker state in a single atomic pointer
	workerState atomic.Pointer[workerState]

	// Workflow and Activity registry - using name as key
	workflows  map[string]*temporal.WorkflowRegistration
	activities map[string]*temporal.ActivityRegistration

	// Factory for creating workers
	workerFactory WorkerFactory
}

// command represents a command sent to the worker manager
type command struct {
	cmd   string
	data  any
	respC chan cmdResponse
}

// cmdResponse is the response from a command
type cmdResponse struct {
	err error
}

// NewTaskQueueHost creates a new WorkerHost instance
func NewTaskQueueHost(config *temporal.TaskQueueRegistration, logger *zap.Logger) *WorkerHost {
	host := &WorkerHost{
		id:            config.ID,
		config:        config,
		log:           logger.With(zap.String("task_queue", config.TaskQueue)),
		workflows:     make(map[string]*temporal.WorkflowRegistration),
		activities:    make(map[string]*temporal.ActivityRegistration),
		workerFactory: &DefaultWorkerFactory{},
	}

	// Initialize empty worker state
	emptyState := &workerState{
		worker:    nil,
		interrupt: nil,
		done:      nil,
	}
	host.workerState.Store(emptyState)

	// Explicitly initialize running to false
	host.running.Store(false)

	return host
}

// ID returns the registry ID of the task queue
func (h *WorkerHost) ID() registry.ID {
	return h.id
}

// Update updates the host with a new task queue configuration
func (h *WorkerHost) Update(config *temporal.TaskQueueRegistration) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.log.Info("updating task queue host configuration")
	h.config = config

	// If the worker is running, we'll need to rebuild it
	if h.running.Load() {
		// Start a goroutine to send a rebuild command
		go func() {
			// Create a dummy command to trigger a rebuild
			respC := make(chan cmdResponse, 1)

			// Use rebuild to trigger a rebuild without changing data
			cmd := command{
				cmd:   CmdRebuild,
				data:  nil,
				respC: respC,
			}

			select {
			case h.cmdCh <- cmd:
				select {
				case <-respC:
				case <-h.ctx.Done():
				}
			case <-h.ctx.Done():
			}
		}()
	}

	return nil
}

// Start initializes and starts the task queue host
func (h *WorkerHost) Start(ctx context.Context) (<-chan any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.log.Info("starting task queue host")

	// Store the parent context
	h.ctx = ctx
	h.cmdCh = make(chan command)
	h.hostDone = make(chan struct{})
	h.statusCh = make(chan any, 10)

	// Get client from resource system when starting
	temporalClient, err := h.acquireClient(h.ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire client: %w", err)
	}
	h.client.Store(&temporalClient)

	// Start the worker management goroutine
	go h.workerManager()

	h.log.Info("task queue host started successfully")
	h.statusCh <- "task queue host started"

	return h.statusCh, nil
}

// Stop gracefully stops the task queue host
func (h *WorkerHost) Stop(ctx context.Context) error {
	h.mu.Lock()
	h.log.Info("stopping task queue host")

	// Set running to false
	h.running.Store(false)

	// Close command channel to signal worker manager to stop
	close(h.cmdCh)
	h.mu.Unlock()

	// Wait for worker manager to exit
	select {
	case <-h.hostDone:
		// Worker manager exited cleanly
	case <-ctx.Done():
		return ctx.Err()
	}

	h.log.Info("task queue host stopped successfully")
	return nil
}

// workerManager is the main goroutine that manages worker lifecycle
func (h *WorkerHost) workerManager() {
	defer close(h.hostDone)
	defer close(h.statusCh)

	// Initial worker creation
	if err := h.rebuildWorker(); err != nil {
		h.log.Error("failed to build initial worker", zap.Error(err))
		if h.statusCh != nil {
			h.statusCh <- err.Error()
		}
		h.running.Store(false)
		return
	}

	for {
		select {
		case <-h.ctx.Done():
			h.log.Debug("worker manager stopping due to context cancellation")
			h.stopWorker(h.ctx)
			h.releaseClient()
			h.running.Store(false)
			return

		case cmd, ok := <-h.cmdCh:
			if !ok {
				// Command channel closed, stop the worker manager
				h.log.Debug("worker manager stopping due to command channel close")
				h.stopWorker(h.ctx)
				h.releaseClient()
				h.running.Store(false)
				return
			}

			resp := cmdResponse{}
			needsRebuild := false

			// Process command
			switch cmd.cmd {
			case CmdRegisterWF:
				reg := cmd.data.(*temporal.WorkflowRegistration)
				// Store in internal registry using name as key
				h.mu.Lock()
				h.workflows[reg.Name] = reg
				h.mu.Unlock()
				needsRebuild = true

			case CmdDeleteWF:
				name := cmd.data.(string)
				// Remove from internal registry
				h.mu.Lock()
				delete(h.workflows, name)
				h.mu.Unlock()
				needsRebuild = true

			case CmdRegisterAct:
				reg := cmd.data.(*temporal.ActivityRegistration)
				// Store in internal registry using name as key
				h.mu.Lock()
				h.activities[reg.Name] = reg
				h.mu.Unlock()
				needsRebuild = true

			case CmdDeleteAct:
				name := cmd.data.(string)
				// Remove from internal registry
				h.mu.Lock()
				delete(h.activities, name)
				h.mu.Unlock()
				needsRebuild = true

			case CmdRebuild:
				// Just trigger a rebuild
				needsRebuild = true

			case CmdWorkerFailed:
				// Worker failed, handle the error
				var errObj error
				if errData, ok := cmd.data.(error); ok {
					errObj = errData
				} else if errStr, ok := cmd.data.(string); ok {
					errObj = fmt.Errorf("worker failed: %s", errStr)
				} else {
					errObj = fmt.Errorf("worker failed with unknown error")
				}

				h.log.Error("worker failed", zap.Error(errObj))
				if h.statusCh != nil {
					h.statusCh <- errObj.Error()
				}

				// Clean up and exit the worker manager
				h.stopWorker(h.ctx)
				h.releaseClient()
				h.running.Store(false)
				return
			}

			// Send response
			select {
			case cmd.respC <- resp:
			default:
				h.log.Warn("failed to send command response", zap.String("cmd", cmd.cmd))
			}

			// Rebuild worker if needed
			if needsRebuild {
				if err := h.rebuildWorker(); err != nil {
					h.log.Error("failed to rebuild worker", zap.Error(err))
				}
			}
		}
	}
}

// stopWorker gracefully stops the current worker if it exists and waits for it to exit
func (h *WorkerHost) stopWorker(ctx context.Context) {
	// Get current worker state
	state := h.workerState.Load()
	if state == nil || state.worker == nil || state.interrupt == nil || state.done == nil {
		return
	}

	h.log.Info("stopping worker")

	// Create a cleanup context with timeout
	cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Create a notification channel for this stop operation
	stopComplete := make(chan struct{})

	// Close the interrupt channel to signal worker to stop
	close(state.interrupt)

	// Wait for worker to exit or timeout
	go func() {
		// Wait for worker goroutine to signal completion
		select {
		case <-state.done:
			close(stopComplete)
		case <-cleanupCtx.Done():
			h.log.Warn("timed out waiting for worker to stop")
			close(stopComplete)
		}
	}()

	// Wait for either worker to stop or timeout
	select {
	case <-stopComplete:
		h.log.Info("worker stopped successfully")
	case <-ctx.Done():
		h.log.Warn("context canceled while waiting for worker to stop")
	}

	// Store an empty worker state with atomic swap
	emptyState := &workerState{
		worker:    nil,
		interrupt: nil,
		done:      nil,
	}
	h.workerState.Swap(emptyState)
}

// releaseClient releases the client resource
func (h *WorkerHost) releaseClient() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.clientResource != nil {
		h.log.Debug("releasing client resource")
		h.clientResource.Release()
		h.clientResource = nil
		h.client.Store(nil) // Reset the client pointer
	}
}

// rebuildWorker creates a new worker instance and registers all workflows and activities
func (h *WorkerHost) rebuildWorker() error {
	h.log.Info("building worker")

	// Get client
	clientPtr := h.client.Load()
	if clientPtr == nil {
		return fmt.Errorf("client not available")
	}
	temporalClient := *clientPtr

	// Get current worker state and prepare to replace it
	oldState := h.workerState.Load()

	// Create a new interrupt channel for this worker
	interruptCh := make(chan interface{})

	// Create a new done channel for this worker
	doneCh := make(chan struct{})

	// Create worker with workflows and activities
	h.mu.RLock()
	worker, err := h.workerFactory.CreateWorker(
		pubsub.WithHost(h.ctx, h),
		h.config,
		temporalClient,
		h.getQueueNameWithPrefix(h.config.TaskQueue),
		h.workflows,
		h.activities,
		h.log,
	)
	h.mu.RUnlock()

	if err != nil {
		return fmt.Errorf("failed to create worker: %w", err)
	}

	// Create the new worker state
	newState := &workerState{
		worker:    worker,
		interrupt: interruptCh,
		done:      doneCh,
	}

	// Start the worker in a separate goroutine
	go func() {
		h.log.Info("starting worker")
		if err := worker.Run(interruptCh); err != nil {
			h.log.Error("worker failed", zap.Error(err))

			// Send failure notification to main loop
			respC := make(chan cmdResponse, 1)
			select {
			case h.cmdCh <- command{
				cmd:   CmdWorkerFailed,
				data:  fmt.Errorf("worker failed: %w", err),
				respC: respC,
			}:
				select {
				case <-respC:
				case <-h.ctx.Done():
				}
			case <-h.ctx.Done():
				// Context canceled, don't need to report
			}
		}

		h.log.Info("worker stopped")
		// Signal that this worker has stopped
		close(doneCh)
	}()

	// Atomically swap in the new worker state
	h.workerState.Swap(newState)

	// Stop old worker after new one is started
	if oldState != nil && oldState.worker != nil && oldState.interrupt != nil && oldState.done != nil {
		h.log.Info("stopping old worker")
		close(oldState.interrupt)

		select {
		case <-oldState.done:
			h.log.Info("old worker stopped successfully")
		case <-h.ctx.Done():
			h.log.Warn("timed out waiting for old worker to stop")
		}
	}

	h.running.Store(true)
	h.log.Info("worker built successfully")
	return nil
}

// getQueueNameWithPrefix applies client prefix if available
func (h *WorkerHost) getQueueNameWithPrefix(queueName string) string {
	if h.clientPrefix == "" {
		return queueName
	}
	return h.clientPrefix + queueName
}

// sendCommand sends a command to the worker manager and waits for a response
func (h *WorkerHost) sendCommand(ctx context.Context, cmd string, data any) error {
	h.mu.RLock()
	cmdCh := h.cmdCh
	h.mu.RUnlock()

	if cmdCh == nil {
		return fmt.Errorf("task queue host not started")
	}

	respC := make(chan cmdResponse, 1)

	// Send command
	select {
	case cmdCh <- command{cmd: cmd, data: data, respC: respC}:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Wait for response
	select {
	case resp := <-respC:
		return resp.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// acquireClient gets the client from the resource registry
func (h *WorkerHost) acquireClient(ctx context.Context) (tmcli.Client, error) {
	// Get resource registry from context
	reg := resource.GetResources(ctx)
	if reg == nil {
		return nil, fmt.Errorf("resource registry not found in context")
	}

	// Acquire client resource
	res, err := reg.Acquire(ctx, h.config.Client, resource.ModeNormal)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire client resource: %w", err)
	}

	// Store resource for later release
	h.clientResource = res

	// Get client from resource
	clientRes, err := res.Get()
	if err != nil {
		h.clientResource.Release()
		h.clientResource = nil
		return nil, fmt.Errorf("failed to get client from resource: %w", err)
	}

	// Convert to client.Resource
	cr, ok := clientRes.(clientpkg.Resource)
	if !ok {
		h.clientResource.Release()
		h.clientResource = nil
		return nil, fmt.Errorf("unexpected resource type: %T", clientRes)
	}

	// Store the client prefix
	h.clientPrefix = cr.Prefix
	return cr.Client, nil
}

// RegisterWorkflow registers a workflow with the task queue
func (h *WorkerHost) RegisterWorkflow(ctx context.Context, registration *temporal.WorkflowRegistration) error {
	// Check if the service is running
	if !h.running.Load() {
		// Service not running, directly add to the registry
		h.mu.Lock()
		h.workflows[registration.Name] = registration
		h.mu.Unlock()
		return nil
	}

	// Service is running, send a command to the worker manager
	return h.sendCommand(ctx, CmdRegisterWF, registration)
}

// DeleteWorkflowByName removes a workflow from the task queue by name
func (h *WorkerHost) DeleteWorkflowByName(ctx context.Context, workflowName string) error {
	// Check if the service is running
	if !h.running.Load() {
		// Service not running, directly remove from the registry
		h.mu.Lock()
		delete(h.workflows, workflowName)
		h.mu.Unlock()
		return nil
	}

	// Service is running, send a command to the worker manager
	return h.sendCommand(ctx, CmdDeleteWF, workflowName)
}

// RegisterActivity registers an activity with the task queue
func (h *WorkerHost) RegisterActivity(ctx context.Context, registration *temporal.ActivityRegistration) error {
	// Check if the service is running
	if !h.running.Load() {
		// Service not running, directly add to the registry
		h.mu.Lock()
		h.activities[registration.Name] = registration
		h.mu.Unlock()
		return nil
	}

	// Service is running, send a command to the worker manager
	return h.sendCommand(ctx, CmdRegisterAct, registration)
}

// DeleteActivityByName removes an activity from the task queue by name
func (h *WorkerHost) DeleteActivityByName(ctx context.Context, activityName string) error {
	// Check if the service is running
	if !h.running.Load() {
		// Service not running, directly remove from the registry
		h.mu.Lock()
		delete(h.activities, activityName)
		h.mu.Unlock()
		return nil
	}

	// Service is running, send a command to the worker manager
	return h.sendCommand(ctx, CmdDeleteAct, activityName)
}

// Launch implements process.Delegated by starting a workflow based on the PID
func (h *WorkerHost) Launch(ctx context.Context, pid pubsub.PID, _ process.Lifecycle, input payload.Payloads) (pubsub.PID, error) {
	clientPtr := h.client.Load()
	if clientPtr == nil {
		return pubsub.PID{}, fmt.Errorf("task queue host not started")
	}
	temporalClient := *clientPtr

	// Generate a UUID v4 for the PID if not provided
	if pid.UniqID == "" {
		u, err := uuid.NewRandom()
		if err != nil {
			return pubsub.PID{}, fmt.Errorf("failed to generate UUID: %w", err)
		}
		pid.UniqID = u.String()
	}

	h.log.Info("launch workflow request received",
		zap.String("pid", pid.String()),
		zap.Int("payloads", len(input)))

	// Find the workflow with matching name
	h.mu.RLock()
	wfName := pid.ID.Name
	wf, exists := h.workflows[wfName]
	queueName := h.getQueueNameWithPrefix(h.config.TaskQueue)
	h.mu.RUnlock()

	if !exists {
		return pubsub.PID{}, fmt.Errorf("workflow %s not found", wfName)
	}

	// Check options
	if wf.Options == nil {
		return pubsub.PID{}, fmt.Errorf("workflow %s is not startable (no options)", wfName)
	}

	options := *wf.Options
	options.ID = pid.UniqID // Always use the provided id
	options.TaskQueue = queueName

	// Use the workflow name
	workflowName := wf.Name

	// Execute the workflow
	run, err := temporalClient.ExecuteWorkflow(ctx, options, workflowName, input)
	if err != nil {
		return pubsub.PID{}, fmt.Errorf("failed to launch workflow: %w", err)
	}

	h.log.Info("workflow launched",
		zap.String("pid", pid.String()),
		zap.String("run_id", run.GetRunID()))

	return pid, nil
}

// Terminate implements process.Delegated by terminating the workflow
func (h *WorkerHost) Terminate(ctx context.Context, pid pubsub.PID) error {
	clientPtr := h.client.Load()
	if clientPtr == nil {
		return fmt.Errorf("task queue host not started")
	}
	temporalClient := *clientPtr

	h.log.Info("terminate workflow request received", zap.String("pid", pid.String()))

	// Terminate the workflow via Temporal
	err := temporalClient.TerminateWorkflow(ctx, pid.UniqID, "", "Terminated by host")
	if err != nil {
		return fmt.Errorf("failed to terminate workflow: %w", err)
	}

	return nil
}

// Send implements pubsub.Host
func (h *WorkerHost) Send(pkg *pubsub.Package) error {
	// Get client
	clientPtr := h.client.Load()
	if clientPtr == nil {
		return fmt.Errorf("task queue host not started")
	}
	temporalClient := *clientPtr

	// Look for workflow by target id (using name)
	h.mu.RLock()
	wfName := pkg.Target.ID.Name
	workflow, exists := h.workflows[wfName]
	h.mu.RUnlock()

	if pkg.Target.UniqID == "" {
		h.log.Warn("cannot send signal to workflow without instance id")
		return fmt.Errorf("cannot send signal to workflow without instance id")
	}

	// Only process the first message, as the previous loop always returned on the first iteration
	if len(pkg.Messages) > 0 {
		msg := pkg.Messages[0]
		// Handle cancel message
		if msg.Topic == topology.KindCancel {
			if pkg.Target.UniqID == "" {
				return fmt.Errorf("cancel requires workflow instance id")
			}

			err := temporalClient.CancelWorkflow(h.ctx, pkg.Target.UniqID, "")
			if err != nil {
				h.log.Warn("failed to cancel workflow",
					zap.String("workflow_id", pkg.Target.UniqID),
					zap.Error(err))
				return fmt.Errorf("failed to cancel workflow: %w", err)
			}

			h.log.Info("workflow canceled", zap.String("workflow_id", pkg.Target.UniqID))
			return nil
		}

		// Check for wake-up signals
		wakeUp := false
		if exists {
			for _, trigger := range workflow.WakeUpSignals {
				if trigger == msg.Topic {
					wakeUp = true
					break
				}
			}
		}

		if wakeUp && exists {
			// Start workflow with signal using service context
			run, err := temporalClient.SignalWithStartWorkflow(
				h.ctx,
				pkg.Target.UniqID,
				msg.Topic,
				msg.Payloads,
				*workflow.Options,
				workflow.Name,
			)

			if err != nil {
				h.log.Error("failed to start workflow with signal",
					zap.String("workflow", wfName),
					zap.String("signal", msg.Topic),
					zap.Error(err))
				return fmt.Errorf("failed to start workflow with signal: %w", err)
			}

			h.log.Info("started workflow with signal",
				zap.String("workflow", wfName),
				zap.String("workflow_id", run.GetID()),
				zap.String("run_id", run.GetRunID()),
				zap.String("signal", msg.Topic))

			return nil
		}

		err := temporalClient.SignalWorkflow(h.ctx, pkg.Target.UniqID, "", msg.Topic, msg.Payloads)
		if err != nil {
			h.log.Warn("failed to send signal",
				zap.String("workflow_id", pkg.Target.UniqID),
				zap.String("signal", msg.Topic),
				zap.Error(err))
			return fmt.Errorf("failed to send signal: %w", err)
		}

		h.log.Debug("signal sent",
			zap.String("workflow_id", pkg.Target.UniqID),
			zap.String("signal", msg.Topic))
		return nil
	}

	return nil
}

// Attach implements pubsub.Host
func (h *WorkerHost) Attach(_ pubsub.PID, _ chan *pubsub.Package) (context.CancelFunc, error) {
	h.log.Warn("temporal does not accept external attachments")
	return nil, fmt.Errorf("direct channel attachment not supported for temporal workflows")
}

// Detach implements pubsub.Host
func (h *WorkerHost) Detach(_ pubsub.PID) {
	h.log.Warn("temporal does not accept external attachments")
}

// Ensure WorkerHost implements all required interfaces
var (
	_ WorkerHostAPI     = (*WorkerHost)(nil)
	_ process.Delegated = (*WorkerHost)(nil)
	_ pubsub.Host       = (*WorkerHost)(nil)
)
