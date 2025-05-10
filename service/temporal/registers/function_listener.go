package temporal

import (
	"context"
	"fmt"
	"strings"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/service/temporal"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// Function metadata keys
const (
	// MetaTemporalActivity is the metadata key for temporal activity configuration
	MetaTemporalActivity = "temporal_activity"

	// Activity config keys within the temporal_activity map
	MetaActivityName      = "name"
	MetaActivityTaskQueue = "task_queue"
)

// FunctionListener listens for function registry entries and registers
// them as Temporal activities when appropriate metadata is present
type FunctionListener struct {
	log *zap.Logger
	bus event.Bus
}

// NewFunctionListener creates a new listener for Temporal activities
func NewFunctionListener(
	bus event.Bus,
	log *zap.Logger,
) *FunctionListener {
	return &FunctionListener{log: log, bus: bus}
}

// Pattern returns the event matching criteria for this handler
func (l *FunctionListener) Pattern() eventbus.Pattern {
	return eventbus.Pattern{System: registry.System, Kind: registry.Changes}
}

// Handle processes registry events
func (l *FunctionListener) Handle(ctx context.Context, evt event.Event) error {
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

// handleFunctionEntry registers a function as a Temporal activity if it has appropriate metadata
func (l *FunctionListener) handleFunctionEntry(ctx context.Context, entry registry.Entry) error {
	// Check if entry is a function type
	if !isFunctionEntry(entry) {
		return nil
	}

	// Get temporal activity metadata
	activityMeta := l.getActivityMetadata(entry)
	if activityMeta == nil {
		return nil // Not a temporal activity
	}

	// Get task queue ID
	taskQueueID, err := l.getTaskQueueID(activityMeta, entry.ID.NS)
	if err != nil {
		l.log.Warn("skipping function registration as activity",
			zap.String("function", entry.ID.String()),
			zap.Error(err))
		return nil
	}

	// Get activity name
	activityName := l.getActivityName(activityMeta, entry.ID.String())

	// Create activity registration
	activityReg := &temporal.ActivityRegistration{
		TaskQueue: taskQueueID,
		Name:      activityName,
		Handler:   entry.ID, // Use function ID as handler
	}

	// Send activity registration event
	l.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.ActivityRegister,
		Path:   entry.ID.String(),
		Data:   activityReg,
	})

	l.log.Debug("registered function as temporal activity",
		zap.String("function", entry.ID.String()),
		zap.String("activity", activityName),
		zap.String("task_queue", taskQueueID.String()))

	return nil
}

// handleFunctionDelete handles deletion of a function
func (l *FunctionListener) handleFunctionDelete(ctx context.Context, entry registry.Entry) error {
	// Check if entry is a function type
	if !isFunctionEntry(entry) {
		return nil
	}

	// We don't need to check the metadata here - just send a delete event
	// if there was no activity registration, the Temporal system will just ignore it

	// Create activity deletion
	deletion := &temporal.ActivityDeletion{
		TaskQueue:    registry.ID{}, // This will be resolved by the Temporal system
		ActivityName: entry.ID.Name, // Use function name as activity name
	}

	// Send activity deletion event
	l.bus.Send(ctx, event.Event{
		System: temporal.System,
		Kind:   temporal.ActivityDelete,
		Path:   entry.ID.String(),
		Data:   deletion,
	})

	l.log.Debug("sent deletion event for function",
		zap.String("function", entry.ID.String()))

	return nil
}

// Helper methods

// isFunctionEntry checks if an entry is a function type
func isFunctionEntry(entry registry.Entry) bool {
	return strings.HasPrefix(string(entry.Kind), "function.")
}

// getActivityMetadata extracts the temporal activity metadata map
func (l *FunctionListener) getActivityMetadata(entry registry.Entry) registry.Metadata {
	if entry.Meta == nil {
		return nil
	}

	// Get the temporal_activity map from metadata
	return entry.Meta.MapValue(MetaTemporalActivity)
}

// getTaskQueueID extracts and validates the task queue ID
func (l *FunctionListener) getTaskQueueID(activityMeta registry.Metadata, defaultNS string) (registry.ID, error) {
	taskQueueStr := activityMeta.StringValue(MetaActivityTaskQueue)
	if taskQueueStr == "" {
		return registry.ID{}, fmt.Errorf("missing required task_queue in temporal_activity metadata")
	}

	// Parse the task queue ID, inheriting namespace from function if not specified
	taskQueueID := registry.ParseID(taskQueueStr)
	if taskQueueID.NS == "" {
		taskQueueID = taskQueueID.WithDefaultNS(defaultNS)
	}

	return taskQueueID, nil
}

// getActivityName gets the activity name from metadata or defaults to function name
func (l *FunctionListener) getActivityName(activityMeta registry.Metadata, defaultName string) string {
	name := activityMeta.StringValue(MetaActivityName)
	if name == "" {
		return defaultName
	}

	return name
}

// Ensure FunctionListener implements eventbus.EventHandler interface
var _ eventbus.EventHandler = (*FunctionListener)(nil)
