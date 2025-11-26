package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/system/eventbus"
	tmcli "go.temporal.io/sdk/client"
	temporalsdk "go.temporal.io/sdk/temporal"
	"go.uber.org/zap"
)

// Metadata keys for workflow configuration
const (
	MetaTemporalWorkflow = "temporal"
	MetaWorkflowName     = "name"
	MetaWorkflowWorker   = "worker"

	// Workflow execution options
	MetaExecutionTimeout = "execution_timeout"
	MetaRunTimeout       = "run_timeout"
	MetaTaskTimeout      = "task_timeout"

	// Retry policy
	MetaRetryPolicy             = "retry_policy"
	MetaRetryInitialInterval    = "initial_interval"
	MetaRetryBackoffCoefficient = "backoff_coefficient"
	MetaRetryMaximumInterval    = "maximum_interval"
	MetaRetryMaximumAttempts    = "maximum_attempts"
	MetaRetryNonRetryableErrors = "non_retryable_error_types"

	// Other workflow options
	MetaWorkflowIDReusePolicy = "workflow_id_reuse_policy"
	MetaCronSchedule          = "cron_schedule"
	MetaMemo                  = "memo"
)

// WorkerManager interface for registering workflows
type WorkerManager interface {
	RegisterWorkflow(ctx context.Context, workerID registry.ID, workflowName string, handler any) error
}

// Listener listens for workflow registry entries and registers
// them as Temporal workflows when appropriate metadata is present
type Listener struct {
	log           *zap.Logger
	bus           event.Bus
	workerManager WorkerManager
}

// NewListener creates a new listener for Temporal workflows
func NewListener(
	log *zap.Logger,
	bus event.Bus,
	workerManager WorkerManager,
) *Listener {
	return &Listener{
		log:           log,
		bus:           bus,
		workerManager: workerManager,
	}
}

// Pattern returns the event matching criteria for this handler
func (l *Listener) Pattern() eventbus.Pattern {
	return eventbus.Pattern{
		System: registry.System,
		Kind:   registry.Changes,
	}
}

// Handle processes registry events
func (l *Listener) Handle(ctx context.Context, evt event.Event) error {
	if evt.System != registry.System {
		return nil
	}

	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		return nil
	}

	switch evt.Kind {
	case registry.Create, registry.Update:
		return l.handleEntry(ctx, entry)
	case registry.Delete:
		return l.handleDelete(ctx, entry)
	default:
		return nil
	}
}

// handleEntry registers a workflow if it has appropriate metadata
func (l *Listener) handleEntry(ctx context.Context, entry registry.Entry) error {
	if !isWorkflowTarget(entry) {
		return nil
	}

	workflowMeta := l.getWorkflowMetadata(entry)
	if workflowMeta == nil {
		return nil
	}

	l.log.Info("workflow listener processing entry",
		zap.String("workflow", entry.ID.String()),
		zap.String("kind", string(entry.Kind)))

	workerID, err := l.getWorkerID(workflowMeta, entry.ID.NS)
	if err != nil {
		l.log.Debug("skipping workflow registration",
			zap.String("workflow", entry.ID.String()),
			zap.Error(err))
		return nil
	}

	// Always use full ID as workflow name
	workflowName := entry.ID.String()

	options, err := l.parseWorkflowOptions(workflowMeta)
	if err != nil {
		l.log.Warn("error parsing workflow options, using defaults",
			zap.String("workflow", entry.ID.String()),
			zap.Error(err))
		options = &tmcli.StartWorkflowOptions{}
	}

	registration := &temporal.WorkflowRegistration{
		Source:    entry.ID,
		TaskQueue: workerID,
		Name:      workflowName,
		Options:   options,
	}

	l.log.Info("sending workflow registration event",
		zap.String("workflow", entry.ID.String()),
		zap.String("name", workflowName),
		zap.String("worker", workerID.String()))

	l.bus.Send(ctx, event.Event{
		System: temporal.SystemTemporalTaskQueue,
		Kind:   temporal.WorkflowRegister,
		Path:   entry.ID.String(),
		Data:   registration,
	})

	l.log.Debug("registered workflow",
		zap.String("workflow", entry.ID.String()),
		zap.String("name", workflowName),
		zap.String("worker", workerID.String()))

	factory := NewDefinitionFactory(entry.ID, workerID, l.log)
	if err := l.workerManager.RegisterWorkflow(ctx, workerID, workflowName, factory); err != nil {
		l.log.Error("failed to register workflow with worker",
			zap.String("workflow", entry.ID.String()),
			zap.String("name", workflowName),
			zap.String("worker", workerID.String()),
			zap.Error(err))
		return fmt.Errorf("failed to register workflow with worker: %w", err)
	}

	return nil
}

// handleDelete handles deletion of a workflow
func (l *Listener) handleDelete(ctx context.Context, entry registry.Entry) error {
	// Workflow deletion handling if needed
	return nil
}

// isWorkflowTarget checks if an entry is a workflow type
func isWorkflowTarget(entry registry.Entry) bool {
	return strings.HasPrefix(string(entry.Kind), "workflow.")
}

// getWorkflowMetadata extracts the temporal workflow metadata map
func (l *Listener) getWorkflowMetadata(entry registry.Entry) registry.Metadata {
	if entry.Meta == nil {
		return nil
	}

	temporal, ok := entry.Meta.GetBag(MetaTemporalWorkflow)
	if !ok {
		return nil
	}

	workflow, ok := temporal.GetBag("workflow")
	if !ok {
		return nil
	}

	return workflow
}

// getWorkerID extracts and validates the worker ID from metadata
func (l *Listener) getWorkerID(workflowMeta registry.Metadata, defaultNS string) (registry.ID, error) {
	workerStr := workflowMeta.GetString(MetaWorkflowWorker, "")
	if workerStr == "" {
		return registry.ID{}, fmt.Errorf("missing required worker in meta.temporal.workflow.worker")
	}

	workerID := registry.ParseID(workerStr)
	if workerID.NS == "" {
		workerID = workerID.WithDefaultNS(defaultNS)
	}

	return workerID, nil
}

// getWorkflowName gets the workflow name from metadata or defaults to workflow ID string
func (l *Listener) getWorkflowName(workflowMeta registry.Metadata, defaultName string) string {
	name := workflowMeta.GetString(MetaWorkflowName, "")
	if name == "" {
		return defaultName
	}
	return name
}

// parseWorkflowOptions parses workflow execution options from metadata
func (l *Listener) parseWorkflowOptions(workflowMeta registry.Metadata) (*tmcli.StartWorkflowOptions, error) {
	options := &tmcli.StartWorkflowOptions{}

	// Parse timeouts as time.Duration (stored as duration in metadata, not strings)
	if execTimeout := workflowMeta.GetDuration(MetaExecutionTimeout, 0); execTimeout > 0 {
		options.WorkflowExecutionTimeout = execTimeout
	}

	if runTimeout := workflowMeta.GetDuration(MetaRunTimeout, 0); runTimeout > 0 {
		options.WorkflowRunTimeout = runTimeout
	}

	if taskTimeout := workflowMeta.GetDuration(MetaTaskTimeout, 0); taskTimeout > 0 {
		options.WorkflowTaskTimeout = taskTimeout
	}

	// Parse retry policy
	if retryPolicyMeta, ok := workflowMeta.GetBag(MetaRetryPolicy); ok {
		retryPolicy, err := l.parseRetryPolicy(retryPolicyMeta)
		if err != nil {
			return nil, fmt.Errorf("invalid retry policy: %w", err)
		}
		if retryPolicy != nil {
			options.RetryPolicy = retryPolicy
		}
	}

	// Parse cron schedule
	if cronSchedule := workflowMeta.GetString(MetaCronSchedule, ""); cronSchedule != "" {
		options.CronSchedule = cronSchedule
	}

	// Parse memo
	if memoMeta, ok := workflowMeta.GetBag(MetaMemo); ok {
		memo := make(map[string]interface{})
		for k, v := range memoMeta {
			memo[k] = v
		}
		if len(memo) > 0 {
			options.Memo = memo
		}
	}

	return options, nil
}

// parseRetryPolicy parses retry policy from metadata
func (l *Listener) parseRetryPolicy(retryMeta registry.Metadata) (*temporalsdk.RetryPolicy, error) {
	retryPolicy := &temporalsdk.RetryPolicy{}
	hasFields := false

	// Parse initial interval (stored as time.Duration)
	if initialInterval := retryMeta.GetDuration(MetaRetryInitialInterval, 0); initialInterval > 0 {
		retryPolicy.InitialInterval = initialInterval
		hasFields = true
	}

	// Parse backoff coefficient (stored as float64)
	if backoffCoef := retryMeta.GetFloat(MetaRetryBackoffCoefficient, 0); backoffCoef > 0 {
		retryPolicy.BackoffCoefficient = backoffCoef
		hasFields = true
	}

	// Parse maximum interval (stored as time.Duration)
	if maxInterval := retryMeta.GetDuration(MetaRetryMaximumInterval, 0); maxInterval > 0 {
		retryPolicy.MaximumInterval = maxInterval
		hasFields = true
	}

	// Parse maximum attempts (stored as int)
	if maxAttempts := retryMeta.GetInt(MetaRetryMaximumAttempts, 0); maxAttempts > 0 {
		retryPolicy.MaximumAttempts = int32(maxAttempts)
		hasFields = true
	}

	// Parse non-retryable error types (stored as []string)
	if nonRetryableErrors := retryMeta.GetSlice(MetaRetryNonRetryableErrors); len(nonRetryableErrors) > 0 {
		retryPolicy.NonRetryableErrorTypes = nonRetryableErrors
		hasFields = true
	}

	if !hasFields {
		return nil, nil
	}

	return retryPolicy, nil
}

var _ eventbus.EventHandler = (*Listener)(nil)
