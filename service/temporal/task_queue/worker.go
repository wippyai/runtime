package temporal

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/service/temporal"
	"go.temporal.io/sdk/activity"
	tmcli "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
)

type ContextDefinition interface {
	WithContext(ctx context.Context) any
}

// Worker is an interface that abstracts the Temporal worker functionality
type Worker interface {
	// Start the worker in a non-blocking fashion
	Start() error

	// Run the worker in a blocking fashion until signal received on channel
	Run(interruptCh <-chan interface{}) error

	// Stop the worker
	Stop()
}

// WorkerFactory is the interface for creating Temporal workers
type WorkerFactory interface {
	// CreateWorker creates a new Temporal worker for a task queue
	// It registers all workflows and activities during creation
	CreateWorker(
		ctx context.Context,
		config *temporal.TaskQueueRegistration,
		client tmcli.Client,
		taskQueue string,
		workflows map[string]*temporal.WorkflowRegistration,
		activities map[string]*temporal.ActivityRegistration,
		logger *zap.Logger,
	) (Worker, error)
}

// DefaultWorkerFactory is the standard implementation of WorkerFactory
type DefaultWorkerFactory struct{}

// temporalWorker wraps the Temporal worker implementation
type temporalWorker struct {
	ctx          context.Context
	worker       worker.Worker
	logger       *zap.Logger
	funcRegistry function.Registry
}

// CreateWorker implements WorkerFactory interface
func (f *DefaultWorkerFactory) CreateWorker(
	ctx context.Context,
	config *temporal.TaskQueueRegistration,
	client tmcli.Client,
	taskQueue string,
	workflows map[string]*temporal.WorkflowRegistration,
	activities map[string]*temporal.ActivityRegistration,
	logger *zap.Logger,
) (Worker, error) {
	// Get function registry from context
	funcRegistry := function.GetRegistry(ctx)
	if funcRegistry == nil {
		return nil, fmt.Errorf("function registry not found in context")
	}

	// Create the worker using options from the config
	w := worker.New(client, taskQueue, config.Options)

	// Create worker wrapper
	tw := &temporalWorker{
		ctx:          ctx,
		worker:       w,
		logger:       logger,
		funcRegistry: funcRegistry,
	}

	// Register all workflows
	for name, wf := range workflows {
		err := tw.registerWorkflow(wf, name)
		if err != nil {
			return nil, fmt.Errorf("failed to register workflow %s: %w", name, err)
		}
	}

	// Register all activities
	for name, act := range activities {
		err := tw.registerActivity(act, name)
		if err != nil {
			return nil, fmt.Errorf("failed to register activity %s: %w", name, err)
		}
	}

	logger.Info("created new worker",
		zap.String("task_queue", taskQueue),
		zap.Int("workflows", len(workflows)),
		zap.Int("activities", len(activities)))

	return tw, nil
}

// registerWorkflow registers a workflow with the worker
func (w *temporalWorker) registerWorkflow(reg *temporal.WorkflowRegistration, name string) error {
	// Assert that handler is never nil for workflows
	if reg.Handler == nil {
		return fmt.Errorf("workflow handler cannot be nil for %s", name)
	}

	handler := reg.Handler
	cwf, ok := handler.(ContextDefinition)
	if ok {
		handler = cwf.WithContext(w.ctx) // If the workflow handler is a ContextDefinition, set the context
	}

	// Register the workflow with the worker
	w.worker.RegisterWorkflowWithOptions(handler, workflow.RegisterOptions{
		Name: reg.Name,
	})

	w.logger.Debug(fmt.Sprintf("registered workflow %s", name))

	return nil
}

// registerActivity registers an activity with the worker
func (w *temporalWorker) registerActivity(reg *temporal.ActivityRegistration, name string) error {
	if reg.Handler == nil {
		return fmt.Errorf("activity handler cannot be nil for %s", name)
	}

	// Check if the handler is an id (for function registry)
	if idHandler, ok := reg.Handler.(registry.ID); ok {
		// Use function registry for executing this activity
		w.worker.RegisterActivityWithOptions(
			w.handleFunctionRegistryActivity(idHandler),
			activity.RegisterOptions{
				Name: reg.Name,
			},
		)

		w.logger.Debug("registered function as activity",
			zap.String("activity", reg.Name),
			zap.String("function", idHandler.String()),
			zap.String("task_queue", reg.TaskQueue.String()))

		return nil
	}

	// Use the handler directly
	w.worker.RegisterActivityWithOptions(reg.Handler, activity.RegisterOptions{
		Name: reg.Name,
	})

	w.logger.Debug("registered activity",
		zap.String("activity", reg.Name),
		zap.String("task_queue", reg.TaskQueue.String()))

	return nil
}

// handleFunctionRegistryActivity creates an activity handler that executes a function through the registry
// This is used for id-based function calls
func (w *temporalWorker) handleFunctionRegistryActivity(id registry.ID) func(ctx context.Context, input payload.Payloads) (payload.Payloads, error) {
	return func(ctx context.Context, input payload.Payloads) (payload.Payloads, error) {
		// todo: mix activity context as well
		resultCh, err := w.funcRegistry.Call(w.ctx, runtime.Task{ID: id, Payloads: input})
		if err != nil {
			return nil, err
		}

		// Wait for result
		select {
		case result := <-resultCh:
			if result == nil {
				return nil, fmt.Errorf("received nil result from function executor")
			}
			if result.Error != nil {
				return nil, result.Error
			}
			return payload.Payloads{result.Value}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Start implements Worker interface
func (w *temporalWorker) Start() error {
	return w.worker.Start()
}

// Run implements Worker interface
func (w *temporalWorker) Run(interruptCh <-chan interface{}) error {
	return w.worker.Run(interruptCh)
}

// Stop implements Worker interface
func (w *temporalWorker) Stop() {
	w.worker.Stop()
}
