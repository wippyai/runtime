package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// Metadata keys for workflow configuration
const (
	MetaTemporalWorkflow = "temporal"
	MetaWorkflowWorker   = "worker"
	MetaWorkflowName     = "name"
)

// WorkerRegistry provides access to workers for workflow registration
type WorkerRegistry interface {
	RegisterWorkflow(ctx context.Context, workerID registry.ID, workflowName string, handler any) error
	UnregisterWorkflow(ctx context.Context, workerID registry.ID, workflowName string) error
}

// Listener listens for workflow registry entries and registers
// them as Temporal workflows when appropriate metadata is present.
// Implements registry.EntryListener for use with RegisterObserver.
type Listener struct {
	log     *zap.Logger
	workers WorkerRegistry
}

// NewListener creates a new listener for Temporal workflows
func NewListener(
	log *zap.Logger,
	workers WorkerRegistry,
) *Listener {
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

// handleEntry registers a workflow with Temporal if it has appropriate metadata
func (l *Listener) handleEntry(ctx context.Context, entry registry.Entry) error {
	if !isWorkflowTarget(entry) {
		return nil
	}

	workflowMeta := l.getWorkflowMetadata(entry)
	if workflowMeta == nil {
		l.log.Debug("no temporal.workflow metadata",
			zap.String("entry", entry.ID.String()),
			zap.String("kind", entry.Kind))
		return nil
	}

	l.log.Info("found temporal workflow metadata",
		zap.String("entry", entry.ID.String()))

	workerID, err := l.getWorkerID(workflowMeta, entry.ID.NS)
	if err != nil {
		l.log.Debug("skipping workflow registration",
			zap.String("workflow", entry.ID.String()),
			zap.Error(err))
		return nil
	}

	// Create workflow definition factory
	factory := &DefinitionFactory{
		ID:  entry.ID,
		log: l.log.Named("workflow"),
	}

	// Use custom name from metadata or default to full ID
	workflowName := workflowMeta.GetString(MetaWorkflowName, entry.ID.String())

	err = l.workers.RegisterWorkflow(ctx, workerID, workflowName, factory)
	if err != nil {
		l.log.Error("failed to register workflow",
			zap.String("workflow", entry.ID.String()),
			zap.String("name", workflowName),
			zap.String("worker", workerID.String()),
			zap.Error(err))
		return err
	}

	l.log.Debug("registered workflow with temporal worker",
		zap.String("workflow", entry.ID.String()),
		zap.String("name", workflowName),
		zap.String("worker", workerID.String()))

	return nil
}

// handleDelete handles deletion of a workflow
func (l *Listener) handleDelete(ctx context.Context, entry registry.Entry) error {
	if !isWorkflowTarget(entry) {
		return nil
	}

	workflowMeta := l.getWorkflowMetadata(entry)
	if workflowMeta == nil {
		return nil
	}

	workerID, err := l.getWorkerID(workflowMeta, entry.ID.NS)
	if err != nil {
		return nil
	}

	// Use custom name from metadata or default to full ID
	workflowName := workflowMeta.GetString(MetaWorkflowName, entry.ID.String())

	if err := l.workers.UnregisterWorkflow(ctx, workerID, workflowName); err != nil {
		l.log.Error("failed to unregister workflow",
			zap.String("workflow", entry.ID.String()),
			zap.String("worker", workerID.String()),
			zap.Error(err))
	}

	l.log.Debug("unregistered temporal workflow",
		zap.String("workflow", entry.ID.String()))

	return nil
}

// isWorkflowTarget checks if an entry is a workflow type
func isWorkflowTarget(entry registry.Entry) bool {
	return strings.HasPrefix(entry.Kind, "workflow.")
}

// getWorkflowMetadata extracts the temporal workflow metadata map
func (l *Listener) getWorkflowMetadata(entry registry.Entry) attrs.Bag {
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
func (l *Listener) getWorkerID(workflowMeta attrs.Bag, defaultNS string) (registry.ID, error) {
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

var _ registry.EntryListener = (*Listener)(nil)
