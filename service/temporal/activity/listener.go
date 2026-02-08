// Package activity provides a registry listener that auto-registers
// functions and processes as Temporal activities based on entry metadata.
package activity

import (
	"context"
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// Metadata keys for activity configuration
const (
	MetaTemporalActivity = "temporal"
	MetaActivityBag      = "activity"
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
// them as Temporal activities when appropriate metadata is present.
// Implements registry.EntryListener for use with RegisterObserver.
type Listener struct {
	log     *zap.Logger
	workers WorkerRegistry
}

// NewListener creates a new listener for Temporal activities
func NewListener(
	log *zap.Logger,
	workers WorkerRegistry,
) *Listener {
	if log == nil {
		log = zap.NewNop()
	}
	return &Listener{
		log:     log,
		workers: workers,
	}
}

// Add implements registry.EntryListener
func (l *Listener) Add(ctx context.Context, entry registry.Entry) error {
	return l.handleEntry(ctx, entry)
}

// Update implements registry.EntryListener
func (l *Listener) Update(ctx context.Context, entry registry.Entry) error {
	return l.handleEntry(ctx, entry)
}

// Delete implements registry.EntryListener
func (l *Listener) Delete(ctx context.Context, entry registry.Entry) error {
	return l.handleDelete(ctx, entry)
}

// handleEntry registers a function as a Temporal activity if it has appropriate metadata
func (l *Listener) handleEntry(ctx context.Context, entry registry.Entry) error {
	if !isActivityTarget(entry) {
		return nil
	}

	activityMeta := l.getActivityMetadata(entry)
	if activityMeta == nil {
		l.log.Debug("no temporal.activity metadata",
			zap.String("entry", entry.ID.String()),
			zap.String("kind", entry.Kind))
		return nil
	}

	l.log.Debug("found temporal activity metadata",
		zap.String("entry", entry.ID.String()))

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
		l.log.Debug("skipping activity unregistration",
			zap.String("function", entry.ID.String()),
			zap.Error(err))
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
	return strings.HasPrefix(entry.Kind, "function.") ||
		strings.HasPrefix(entry.Kind, "process.")
}

// getActivityMetadata extracts the temporal activity metadata map
func (l *Listener) getActivityMetadata(entry registry.Entry) attrs.Bag {
	if entry.Meta == nil {
		return nil
	}

	temporal, ok := entry.Meta.GetBag(MetaTemporalActivity)
	if !ok {
		return nil
	}

	activity, ok := temporal.GetBag(MetaActivityBag)
	if !ok {
		return nil
	}

	return activity
}

// getWorkerID extracts and validates the worker ID from metadata
func (l *Listener) getWorkerID(activityMeta attrs.Bag, defaultNS string) (registry.ID, error) {
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

var _ registry.EntryListener = (*Listener)(nil)
