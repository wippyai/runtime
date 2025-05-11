package workflow

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/service/temporal"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.temporal.io/sdk/client"
	temporalsdk "go.temporal.io/sdk/temporal"
	"go.uber.org/zap"
)

// Function metadata keys
const (
	// MetaTemporalWorkflow is the metadata key for temporal workflow configuration
	MetaTemporalWorkflow  = "temporal_workflow"
	MetaWorkflowName      = "name"
	MetaWorkflowTaskQueue = "task_queue"
	MetaWorkflowWakeUp    = "wake_up_signals"

	// Workflow options keys
	MetaWorkflowExecutionTimeout = "execution_timeout"
	MetaWorkflowRunTimeout       = "run_timeout"
	MetaWorkflowTaskTimeout      = "task_timeout"

	// Retry policy keys
	MetaRetryPolicy             = "retry_policy"
	MetaRetryInitialInterval    = "initial_interval"
	MetaRetryBackoffCoefficient = "backoff_coefficient"
	MetaRetryMaximumInterval    = "maximum_interval"
	MetaRetryMaximumAttempts    = "maximum_attempts"
	MetaRetryNonRetryableErrors = "non_retryable_error_types"
)

// WorkflowListener listens for workflow registry entries and registers
// them as Temporal workflows when appropriate metadata is present
type WorkflowListener struct {
	log *zap.Logger
	bus event.Bus
}

// NewWorkflowListener creates a new listener for Temporal workflows and registers it with the event bus
func NewWorkflowListener(
	bus event.Bus,
	log *zap.Logger,
) eventbus.EventHandler {
	listener := &WorkflowListener{log: log, bus: bus}
	return listener
}

// Pattern returns the event matching criteria for this handler
func (l *WorkflowListener) Pattern() eventbus.Pattern {
	return eventbus.Pattern{System: registry.System, Kind: registry.Changes}
}

// Handle processes registry events
func (l *WorkflowListener) Handle(ctx context.Context, evt event.Event) error {
	// Only handle registry events
	if evt.System != registry.System {
		return nil
	}

	// Parse registry entry
	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		return nil // Not a registry entry event
	}

	// Process event based on kind
	switch evt.Kind {
	case registry.Create, registry.Update:
		return l.handleFunctionEntry(ctx, entry)
	case registry.Delete:
		return l.handleFunctionDelete(ctx, entry)
	default:
		return nil // Ignore other events
	}
}

// handleFunctionEntry registers a function as a Temporal workflow if it has appropriate metadata
func (l *WorkflowListener) handleFunctionEntry(ctx context.Context, entry registry.Entry) error {
	// Check if entry is a workflow type
	if !isWorkflowTarget(entry) {
		return nil
	}

	// Get temporal workflow metadata
	workflowMeta := l.getWorkflowMetadata(entry)
	if workflowMeta == nil {
		return nil // Not a temporal workflow
	}

	// Get task queue ID
	taskQueueID, err := l.getTaskQueueID(workflowMeta, entry.ID.NS)
	if err != nil {
		l.log.Warn("skipping workflow registration",
			zap.String("workflow", entry.ID.String()),
			zap.Error(err))
		return nil
	}

	// Get workflow name
	workflowName := l.getWorkflowName(workflowMeta, entry.ID.String())

	// Get workflow options
	options, err := l.getWorkflowOptions(workflowMeta)
	if err != nil {
		l.log.Warn("error parsing workflow options, using defaults",
			zap.String("workflow", entry.ID.String()),
			zap.Error(err))
		// Continue with default options
	}

	// Get wake-up signals
	wakeUpSignals := l.getWakeUpSignals(workflowMeta)

	// Create a definition factory for this workflow entry
	factory := NewDefinitionFactory(entry.ID)

	// Create workflow registration
	workflowReg := &temporal.WorkflowRegistration{
		TaskQueue:     taskQueueID,
		Name:          workflowName,
		Handler:       factory, // Use definition factory as handler
		Options:       options,
		WakeUpSignals: wakeUpSignals,
	}

	// Send workflow registration event
	l.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.WorkflowRegister,
		Path:   entry.ID.String(),
		Data:   workflowReg,
	})

	l.log.Debug("registered workflow with temporal",
		zap.String("workflow", entry.ID.String()),
		zap.String("name", workflowName),
		zap.String("task_queue", taskQueueID.String()))

	return nil
}

// handleFunctionDelete handles deletion of a workflow
func (l *WorkflowListener) handleFunctionDelete(ctx context.Context, entry registry.Entry) error {
	// Check if entry is a workflow type
	if !isWorkflowTarget(entry) {
		return nil
	}

	// We don't need to check the metadata here - just send a delete event
	// if there was no workflow registration, the Temporal system will just ignore it

	// Create workflow deletion
	deletion := &temporal.WorkflowDeletion{
		TaskQueue:    registry.ID{}, // This will be resolved by the Temporal system
		WorkflowName: entry.ID.Name, // Use workflow name as workflow name
	}

	// Send workflow deletion event
	l.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.WorkflowDelete,
		Path:   entry.ID.String(),
		Data:   deletion,
	})

	l.log.Debug("sent deletion event for workflow",
		zap.String("workflow", entry.ID.String()))

	return nil
}

// Helper methods

// isWorkflowTarget checks if an entry is a workflow type
func isWorkflowTarget(entry registry.Entry) bool {
	return strings.HasPrefix(string(entry.Kind), "workflow.")
}

// getWorkflowMetadata extracts the temporal workflow metadata map
func (l *WorkflowListener) getWorkflowMetadata(entry registry.Entry) registry.Metadata {
	if entry.Meta == nil {
		return nil
	}

	// Get the temporal_workflow map from metadata
	return entry.Meta.MapValue(MetaTemporalWorkflow)
}

// getTaskQueueID extracts and validates the task queue ID
func (l *WorkflowListener) getTaskQueueID(workflowMeta registry.Metadata, defaultNS string) (registry.ID, error) {
	taskQueueStr := workflowMeta.StringValue(MetaWorkflowTaskQueue)
	if taskQueueStr == "" {
		return registry.ID{}, fmt.Errorf("missing required task_queue in temporal_workflow metadata")
	}

	// Parse the task queue ID, inheriting namespace from function if not specified
	taskQueueID := registry.ParseID(taskQueueStr)
	if taskQueueID.NS == "" {
		taskQueueID = taskQueueID.WithDefaultNS(defaultNS)
	}

	return taskQueueID, nil
}

// getWorkflowName gets the workflow name from metadata or defaults to function name
func (l *WorkflowListener) getWorkflowName(workflowMeta registry.Metadata, defaultName string) string {
	name := workflowMeta.StringValue(MetaWorkflowName)
	if name == "" {
		return defaultName
	}

	return name
}

// parseDuration attempts to parse a duration from either a string or an integer (milliseconds)
func (l *WorkflowListener) parseDuration(value interface{}) (time.Duration, error) {
	switch v := value.(type) {
	case string:
		return time.ParseDuration(v)
	case int:
		return time.Duration(v) * time.Millisecond, nil
	case int64:
		return time.Duration(v) * time.Millisecond, nil
	case float64:
		return time.Duration(v) * time.Millisecond, nil
	default:
		return 0, fmt.Errorf("unsupported duration format: %T", value)
	}
}

// getMetaDuration extracts a duration value from metadata, supporting both string and numeric formats
func (l *WorkflowListener) getMetaDuration(meta registry.Metadata, key string) (time.Duration, error) {
	// First try to get as string
	if strVal := meta.StringValue(key); strVal != "" {
		return time.ParseDuration(strVal)
	}

	// Then try as integer (milliseconds)
	if intVal := meta.IntValue(key); intVal > 0 {
		return time.Duration(intVal) * time.Millisecond, nil
	}

	// Not found or zero
	return 0, nil
}

// getWorkflowOptions parses workflow options from metadata
func (l *WorkflowListener) getWorkflowOptions(workflowMeta registry.Metadata) (*client.StartWorkflowOptions, error) {
	// Create default options
	options := &client.StartWorkflowOptions{}

	// Parse execution timeout if present
	if execTimeout, err := l.getMetaDuration(workflowMeta, MetaWorkflowExecutionTimeout); err != nil {
		return nil, fmt.Errorf("invalid execution timeout: %w", err)
	} else if execTimeout > 0 {
		options.WorkflowExecutionTimeout = execTimeout
	}

	// Parse run timeout if present
	if runTimeout, err := l.getMetaDuration(workflowMeta, MetaWorkflowRunTimeout); err != nil {
		return nil, fmt.Errorf("invalid run timeout: %w", err)
	} else if runTimeout > 0 {
		options.WorkflowRunTimeout = runTimeout
	}

	// Parse task timeout if present
	if taskTimeout, err := l.getMetaDuration(workflowMeta, MetaWorkflowTaskTimeout); err != nil {
		return nil, fmt.Errorf("invalid task timeout: %w", err)
	} else if taskTimeout > 0 {
		options.WorkflowTaskTimeout = taskTimeout
	}

	// Parse retry policy if present
	retryPolicyMeta := workflowMeta.MapValue(MetaRetryPolicy)
	if retryPolicyMeta != nil {
		retryPolicy := &temporalsdk.RetryPolicy{}

		// Parse initial interval
		if initialInterval, err := l.getMetaDuration(retryPolicyMeta, MetaRetryInitialInterval); err != nil {
			return nil, fmt.Errorf("invalid retry initial interval: %w", err)
		} else if initialInterval > 0 {
			retryPolicy.InitialInterval = initialInterval
		}

		// Parse backoff coefficient
		if backoffCoef := float64(retryPolicyMeta.IntValue(MetaRetryBackoffCoefficient)); backoffCoef > 0 {
			retryPolicy.BackoffCoefficient = backoffCoef
		} else if backoffStr := retryPolicyMeta.StringValue(MetaRetryBackoffCoefficient); backoffStr != "" {
			if coef, err := strconv.ParseFloat(backoffStr, 64); err == nil && coef > 0 {
				retryPolicy.BackoffCoefficient = coef
			}
		}

		// Parse maximum interval
		if maxInterval, err := l.getMetaDuration(retryPolicyMeta, MetaRetryMaximumInterval); err != nil {
			return nil, fmt.Errorf("invalid retry maximum interval: %w", err)
		} else if maxInterval > 0 {
			retryPolicy.MaximumInterval = maxInterval
		}

		// Parse maximum attempts
		if maxAttempts := retryPolicyMeta.IntValue(MetaRetryMaximumAttempts); maxAttempts > 0 {
			retryPolicy.MaximumAttempts = int32(maxAttempts)
		} else if attemptsStr := retryPolicyMeta.StringValue(MetaRetryMaximumAttempts); attemptsStr != "" {
			if attempts, err := strconv.ParseInt(attemptsStr, 10, 32); err == nil && attempts > 0 {
				retryPolicy.MaximumAttempts = int32(attempts)
			}
		}

		// Parse non-retryable error types
		if nonRetryableErrors := retryPolicyMeta.TagValue(MetaRetryNonRetryableErrors); nonRetryableErrors != nil {
			retryPolicy.NonRetryableErrorTypes = nonRetryableErrors
		}

		// Only set retry policy if it has at least one field set
		if retryPolicy.InitialInterval > 0 ||
			retryPolicy.BackoffCoefficient > 0 ||
			retryPolicy.MaximumInterval > 0 ||
			retryPolicy.MaximumAttempts > 0 ||
			len(retryPolicy.NonRetryableErrorTypes) > 0 {
			options.RetryPolicy = retryPolicy
		}
	}

	return options, nil
}

// getWakeUpSignals extracts wake-up signals from metadata
func (l *WorkflowListener) getWakeUpSignals(workflowMeta registry.Metadata) []string {
	return workflowMeta.TagValue(MetaWorkflowWakeUp)
}

// Ensure WorkflowListener implements eventbus.EventHandler interface
var _ eventbus.EventHandler = (*WorkflowListener)(nil)
