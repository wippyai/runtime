package activity

import (
	"context"
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// Metadata keys for activity configuration
const (
	MetaTemporalActivity = "temporal"
	MetaActivityName     = "name"
	MetaActivityWorker   = "worker"
	MetaActivityLocal    = "local"
)

// WorkerRegistry provides access to workers for activity registration
type WorkerRegistry interface {
	RegisterActivity(ctx context.Context, workerID registry.ID, activityName string, funcID registry.ID) error
	RegisterLocalActivity(ctx context.Context, workerID registry.ID, activityName string, funcID registry.ID) error
	UnregisterActivity(ctx context.Context, workerID registry.ID, activityName string) error
}

// Listener listens for function registry entries and registers
// them as Temporal activities when appropriate metadata is present
type Listener struct {
	log     *zap.Logger
	workers WorkerRegistry
}

// NewListener creates a new listener for Temporal activities
func NewListener(
	log *zap.Logger,
	workers WorkerRegistry,
) *Listener {
	return &Listener{
		log:     log,
		workers: workers,
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

// handleEntry registers a function as a Temporal activity if it has appropriate metadata
func (l *Listener) handleEntry(ctx context.Context, entry registry.Entry) error {
	if !isActivityTarget(entry) {
		return nil
	}

	activityMeta := l.getActivityMetadata(entry)
	if activityMeta == nil {
		return nil
	}

	workerID, err := l.getWorkerID(activityMeta, entry.ID.NS)
	if err != nil {
		l.log.Debug("skipping function registration as activity",
			zap.String("function", entry.ID.String()),
			zap.Error(err))
		return nil
	}

	// Always use full ID as activity name
	activityName := entry.ID.String()
	isLocal := activityMeta.GetBool(MetaActivityLocal, false)

	if isLocal {
		err = l.workers.RegisterLocalActivity(ctx, workerID, activityName, entry.ID)
	} else {
		err = l.workers.RegisterActivity(ctx, workerID, activityName, entry.ID)
	}

	if err != nil {
		l.log.Error("failed to register activity",
			zap.String("function", entry.ID.String()),
			zap.String("activity", activityName),
			zap.String("worker", workerID.String()),
			zap.Bool("local", isLocal),
			zap.Error(err))
		return err
	}

	l.log.Debug("registered function as temporal activity",
		zap.String("function", entry.ID.String()),
		zap.String("activity", activityName),
		zap.String("worker", workerID.String()),
		zap.Bool("local", isLocal))

	return nil
}

// handleDelete handles deletion of a function
func (l *Listener) handleDelete(ctx context.Context, entry registry.Entry) error {
	if !isActivityTarget(entry) {
		return nil
	}

	activityMeta := l.getActivityMetadata(entry)
	if activityMeta == nil {
		return nil
	}

	workerID, err := l.getWorkerID(activityMeta, entry.ID.NS)
	if err != nil {
		return nil
	}

	// Always use full ID as activity name
	activityName := entry.ID.String()

	if err := l.workers.UnregisterActivity(ctx, workerID, activityName); err != nil {
		l.log.Error("failed to unregister activity",
			zap.String("function", entry.ID.String()),
			zap.String("activity", activityName),
			zap.String("worker", workerID.String()),
			zap.Error(err))
	}

	l.log.Debug("unregistered temporal activity",
		zap.String("function", entry.ID.String()),
		zap.String("activity", activityName))

	return nil
}

// isActivityTarget checks if an entry is a function or process type
func isActivityTarget(entry registry.Entry) bool {
	return strings.HasPrefix(string(entry.Kind), "function.") ||
		strings.HasPrefix(string(entry.Kind), "process.")
}

// getActivityMetadata extracts the temporal activity metadata map
func (l *Listener) getActivityMetadata(entry registry.Entry) registry.Metadata {
	if entry.Meta == nil {
		return nil
	}

	temporal, ok := entry.Meta.GetBag(MetaTemporalActivity)
	if !ok {
		return nil
	}

	activity, ok := temporal.GetBag("activity")
	if !ok {
		return nil
	}

	return activity
}

// getWorkerID extracts and validates the worker ID from metadata
func (l *Listener) getWorkerID(activityMeta registry.Metadata, defaultNS string) (registry.ID, error) {
	workerStr := activityMeta.GetString(MetaActivityWorker, "")
	if workerStr == "" {
		return registry.ID{}, fmt.Errorf("missing required worker in meta.temporal.activity.worker")
	}

	workerID := registry.ParseID(workerStr)
	if workerID.NS == "" {
		workerID = workerID.WithDefaultNS(defaultNS)
	}

	return workerID, nil
}

// getActivityName gets the activity name from metadata or defaults to function ID string
func (l *Listener) getActivityName(activityMeta registry.Metadata, defaultName string) string {
	name := activityMeta.GetString(MetaActivityName, "")
	if name == "" {
		return defaultName
	}
	return name
}

var _ eventbus.EventHandler = (*Listener)(nil)
