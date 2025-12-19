package peer

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

// Receiver implements relay.Receiver for Temporal workflows.
// It handles topology events (monitor, link) and sends EXIT events
// when workflows complete.
type Receiver struct {
	nodeID pid.NodeID
	client client.Client
	router relay.Receiver
	log    *zap.Logger

	mu       sync.RWMutex
	watchers map[string]*workflowWatcher // workflowID -> watcher state
	ctx      context.Context
	cancel   context.CancelFunc
}

// workflowWatcher tracks watchers for a single workflow.
type workflowWatcher struct {
	workflowID string
	runID      string
	taskQueue  string
	monitors   map[string]pid.PID // callerKey -> callerPID (monitoring)
	links      map[string]pid.PID // callerKey -> callerPID (linked)
	cancel     context.CancelFunc
	watching   bool
}

// NewReceiver creates a new Temporal peer receiver.
func NewReceiver(ctx context.Context, nodeID pid.NodeID, temporalClient client.Client, router relay.Receiver, logger *zap.Logger) *Receiver {
	ctx, cancel := context.WithCancel(ctx)
	return &Receiver{
		nodeID:   nodeID,
		client:   temporalClient,
		router:   router,
		log:      logger,
		watchers: make(map[string]*workflowWatcher),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Send implements relay.Receiver.
// Handles topology events routed to this Temporal peer.
func (r *Receiver) Send(pkg *relay.Package) error {
	for _, msg := range pkg.Messages {
		for _, p := range msg.Payloads {
			switch event := p.Data().(type) {
			case *topology.MonitorRequestEvent:
				return r.handleMonitorRequest(event.Caller, event.Target)
			case *topology.MonitorReleaseEvent:
				return r.handleMonitorRelease(event.Caller, event.Target)
			case *topology.LinkRequestEvent:
				return r.handleLinkRequest(event.From, event.To)
			case *topology.UnlinkRequestEvent:
				return r.handleUnlinkRequest(event.From, event.To)
			case *topology.ExitEvent:
				return r.handleExitEvent(event.From, pkg.Target)
			}
		}
	}
	return nil
}

// handleExitEvent handles exit/linkdown from a linked process.
// When a local process dies while linked to a workflow, clean up the link.
func (r *Receiver) handleExitEvent(from, target pid.PID) error {
	r.log.Debug("exit event received",
		zap.String("from", from.String()),
		zap.String("target", target.String()))

	r.mu.Lock()
	defer r.mu.Unlock()

	watcher, exists := r.watchers[target.UniqID]
	if !exists {
		return nil
	}

	// Remove from links (process was linked to this workflow)
	delete(watcher.links, from.String())

	// Remove from monitors (process was monitoring this workflow)
	delete(watcher.monitors, from.String())

	r.cleanupWatcherIfEmpty(watcher)

	return nil
}

// handleMonitorRequest adds a monitor for a workflow.
func (r *Receiver) handleMonitorRequest(caller, target pid.PID) error {
	r.log.Debug("monitor request received",
		zap.String("caller", caller.String()),
		zap.String("target", target.String()))

	r.mu.Lock()
	defer r.mu.Unlock()

	watcher, exists := r.watchers[target.UniqID]
	if !exists {
		watcher = &workflowWatcher{
			workflowID: target.UniqID,
			taskQueue:  target.Host,
			monitors:   make(map[string]pid.PID),
			links:      make(map[string]pid.PID),
		}
		r.watchers[target.UniqID] = watcher
	}

	watcher.monitors[caller.String()] = caller

	// Start watching if not already
	if !watcher.watching {
		watcher.watching = true
		ctx, cancel := context.WithCancel(r.ctx)
		watcher.cancel = cancel
		go r.watchWorkflow(ctx, watcher)
	}

	return nil
}

// handleMonitorRelease removes a monitor for a workflow.
func (r *Receiver) handleMonitorRelease(caller, target pid.PID) error {
	r.log.Debug("monitor release received",
		zap.String("caller", caller.String()),
		zap.String("target", target.String()))

	r.mu.Lock()
	defer r.mu.Unlock()

	watcher, exists := r.watchers[target.UniqID]
	if !exists {
		return nil
	}

	delete(watcher.monitors, caller.String())
	r.cleanupWatcherIfEmpty(watcher)

	return nil
}

// handleLinkRequest adds a link for a workflow.
func (r *Receiver) handleLinkRequest(from, to pid.PID) error {
	r.log.Debug("link request received",
		zap.String("from", from.String()),
		zap.String("to", to.String()))

	r.mu.Lock()
	defer r.mu.Unlock()

	watcher, exists := r.watchers[to.UniqID]
	if !exists {
		watcher = &workflowWatcher{
			workflowID: to.UniqID,
			taskQueue:  to.Host,
			monitors:   make(map[string]pid.PID),
			links:      make(map[string]pid.PID),
		}
		r.watchers[to.UniqID] = watcher
	}

	watcher.links[from.String()] = from

	// Start watching if not already
	if !watcher.watching {
		watcher.watching = true
		ctx, cancel := context.WithCancel(r.ctx)
		watcher.cancel = cancel
		go r.watchWorkflow(ctx, watcher)
	}

	return nil
}

// handleUnlinkRequest removes a link for a workflow.
func (r *Receiver) handleUnlinkRequest(from, to pid.PID) error {
	r.log.Debug("unlink request received",
		zap.String("from", from.String()),
		zap.String("to", to.String()))

	r.mu.Lock()
	defer r.mu.Unlock()

	watcher, exists := r.watchers[to.UniqID]
	if !exists {
		return nil
	}

	delete(watcher.links, from.String())
	r.cleanupWatcherIfEmpty(watcher)

	return nil
}

// cleanupWatcherIfEmpty removes the watcher if no monitors or links remain.
func (r *Receiver) cleanupWatcherIfEmpty(watcher *workflowWatcher) {
	if len(watcher.monitors) == 0 && len(watcher.links) == 0 {
		if watcher.cancel != nil {
			watcher.cancel()
		}
		delete(r.watchers, watcher.workflowID)
	}
}

// watchWorkflow watches a workflow for completion and notifies watchers.
func (r *Receiver) watchWorkflow(ctx context.Context, watcher *workflowWatcher) {
	workflowID := watcher.workflowID

	r.log.Debug("starting workflow watch",
		zap.String("workflow_id", workflowID))

	// Get workflow run handle
	run := r.client.GetWorkflow(ctx, workflowID, watcher.runID)

	// Wait for completion
	var result any
	err := run.Get(ctx, &result)

	// Check if cancelled
	if ctx.Err() != nil {
		r.log.Debug("workflow watch cancelled",
			zap.String("workflow_id", workflowID))
		return
	}

	r.log.Debug("workflow completed",
		zap.String("workflow_id", workflowID),
		zap.Any("result", result),
		zap.Error(err))

	// Notify all watchers
	r.notifyCompletion(watcher, result, err)
}

// notifyCompletion sends EXIT events to all monitors and LINK_DOWN to linked processes.
func (r *Receiver) notifyCompletion(watcher *workflowWatcher, result any, err error) {
	r.mu.Lock()
	monitors := make([]pid.PID, 0, len(watcher.monitors))
	for _, p := range watcher.monitors {
		monitors = append(monitors, p)
	}
	links := make([]pid.PID, 0, len(watcher.links))
	for _, p := range watcher.links {
		links = append(links, p)
	}
	// Clear and remove watcher
	delete(r.watchers, watcher.workflowID)
	r.mu.Unlock()

	// Build workflow PID
	workflowPID := pid.PID{
		Node:   r.nodeID,
		Host:   watcher.taskQueue,
		UniqID: watcher.workflowID,
	}

	// Build result
	runtimeResult := &runtime.Result{}
	if err != nil {
		runtimeResult.Error = err
	} else if result != nil {
		runtimeResult.Value = payload.New(result)
	}

	// Send EXIT to monitors
	for _, monitorPID := range monitors {
		r.sendExitEvent(workflowPID, monitorPID, topology.Exit, runtimeResult)
	}

	// Send LINK_DOWN to linked processes (only if error)
	if err != nil {
		for _, linkedPID := range links {
			r.sendExitEvent(workflowPID, linkedPID, topology.LinkDown, runtimeResult)
		}
	} else {
		// Normal completion - still notify linked processes with EXIT
		for _, linkedPID := range links {
			r.sendExitEvent(workflowPID, linkedPID, topology.Exit, runtimeResult)
		}
	}
}

// sendExitEvent sends an exit event to a target PID.
func (r *Receiver) sendExitEvent(from, to pid.PID, kind topology.Kind, result *runtime.Result) {
	exitEvent := &topology.ExitEvent{
		Kind:   kind,
		From:   from,
		Result: result,
	}

	pkg := relay.NewPackage(from, to, topology.TopicEvents, payload.New(exitEvent))

	if err := r.router.Send(pkg); err != nil {
		r.log.Error("failed to send exit event",
			zap.String("from", from.String()),
			zap.String("to", to.String()),
			zap.String("kind", kind),
			zap.Error(err))
	} else {
		r.log.Debug("sent exit event",
			zap.String("from", from.String()),
			zap.String("to", to.String()),
			zap.String("kind", kind))
	}
}

// Stop gracefully shuts down the receiver.
func (r *Receiver) Stop() {
	r.cancel()

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, watcher := range r.watchers {
		if watcher.cancel != nil {
			watcher.cancel()
		}
	}
	r.watchers = make(map[string]*workflowWatcher)
}

var _ relay.Receiver = (*Receiver)(nil)
