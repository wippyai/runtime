// SPDX-License-Identifier: MPL-2.0

// Package peer implements relay-to-Temporal signal bridging, allowing
// local processes to send messages to Temporal workflows via the relay system.
package peer

import (
	"context"
	"fmt"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/topology"
	temporalerrors "github.com/wippyai/runtime/service/temporal/errors"
	temporalprop "github.com/wippyai/runtime/service/temporal/propagator"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

// Receiver implements relay.Receiver for Temporal workflows.
// It handles topology events (monitor, link) and sends EXIT events
// when workflows complete.
type Receiver struct {
	client   client.Client
	router   relay.Receiver
	ctx      context.Context
	handoff  temporalapi.WorkflowRunHandoff
	log      *zap.Logger
	watchers map[string]*workflowWatcher
	cancel   context.CancelFunc
	nodeID   pid.NodeID
	clientID string
	mu       sync.RWMutex
}

// workflowWatcher tracks watchers for a single workflow.
type workflowWatcher struct {
	monitors   map[string]pid.PID
	links      map[string]pid.PID
	cancel     context.CancelFunc
	workflowID string
	runID      string
	taskQueue  string
	watching   bool
}

// NewReceiver creates a new Temporal peer receiver.
func NewReceiver(ctx context.Context, nodeID pid.NodeID, temporalClient client.Client, router relay.Receiver, logger *zap.Logger) *Receiver {
	ctx, cancel := context.WithCancel(ctx)
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Receiver{
		nodeID:   nodeID,
		clientID: nodeID,
		client:   temporalClient,
		router:   router,
		log:      logger,
		watchers: make(map[string]*workflowWatcher),
		ctx:      ctx,
		cancel:   cancel,
		handoff:  temporalapi.GetWorkflowRunHandoff(ctx),
	}
}

// Send implements relay.Receiver.
// Handles topology events routed to this Temporal peer.
func (r *Receiver) Send(pkg *relay.Package) error {
	var firstErr error

	for _, msg := range pkg.Messages {
		for _, p := range msg.Payloads {
			switch event := p.Data().(type) {
			case *topology.MonitorRequestEvent:
				if err := r.handleMonitorRequest(event.Caller, event.Target); err != nil && firstErr == nil {
					firstErr = err
				}
			case *topology.MonitorReleaseEvent:
				if err := r.handleMonitorRelease(event.Caller, event.Target); err != nil && firstErr == nil {
					firstErr = err
				}
			case *topology.LinkRequestEvent:
				if err := r.handleLinkRequest(event.From, event.To); err != nil && firstErr == nil {
					firstErr = err
				}
			case *topology.UnlinkRequestEvent:
				if err := r.handleUnlinkRequest(event.From, event.To); err != nil && firstErr == nil {
					firstErr = err
				}
			case *topology.CancelEvent:
				if err := r.handleCancelRequest(event, pkg.Target); err != nil && firstErr == nil {
					firstErr = err
				}
			case *topology.ExitEvent:
				if err := r.handleExitEvent(event.From, pkg.Target); err != nil && firstErr == nil {
					firstErr = err
				}
			}
		}
	}

	for _, msg := range pkg.Messages {
		if msg.Topic == topology.TopicEvents {
			continue
		}
		if r.client == nil {
			continue
		}
		if err := r.signalWorkflow(pkg.Source, pkg.Target, msg); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (r *Receiver) handleCancelRequest(event *topology.CancelEvent, target pid.PID) error {
	if r.client == nil {
		return fmt.Errorf("temporal client not available")
	}

	workflowID := target.UniqID
	if workflowID == "" {
		return fmt.Errorf("workflow ID is empty")
	}

	return r.client.CancelWorkflow(r.ctx, workflowID, event.Reason)
}

func (r *Receiver) signalWorkflow(from, target pid.PID, msg *relay.Message) error {
	if r.client == nil {
		return fmt.Errorf("temporal client not available")
	}
	if msg == nil || msg.Topic == "" {
		return nil
	}
	if target.UniqID == "" {
		return fmt.Errorf("workflow ID is empty")
	}
	var signalArg any
	switch len(msg.Payloads) {
	case 0:
		signalArg = nil
	case 1:
		signalArg = msg.Payloads[0]
	default:
		signalArg = msg.Payloads
	}

	ctx := r.ctx
	var fc ctxapi.FrameContext
	if from.Node != "" || from.Host != "" || from.UniqID != "" {
		ctx, fc = ctxapi.ForkFrameContext(ctx)
		values, err := ctxapi.GetOrCreateValues(ctx)
		if err == nil {
			values.Set(temporalprop.SignalFromValueKey, from.String())
		}
	}
	if fc != nil {
		defer ctxapi.ReleaseFrameContext(fc)
	}
	return r.client.SignalWorkflow(ctx, target.UniqID, "", msg.Topic, signalArg)
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

	r.assignRunIDIfAvailable(watcher)
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

	r.assignRunIDIfAvailable(watcher)
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

func (r *Receiver) assignRunIDIfAvailable(watcher *workflowWatcher) {
	if watcher == nil || watcher.runID != "" || watcher.workflowID == "" {
		return
	}
	if r.handoff == nil || r.clientID == "" {
		return
	}

	runID, ok := r.handoff.Consume(r.clientID, watcher.workflowID)
	if !ok || runID == "" {
		return
	}

	watcher.runID = runID
}

// watchWorkflow watches a workflow for completion and notifies watchers.
func (r *Receiver) watchWorkflow(ctx context.Context, watcher *workflowWatcher) {
	workflowID := watcher.workflowID

	r.log.Debug("starting workflow watch",
		zap.String("workflow_id", workflowID),
		zap.String("run_id", watcher.runID))

	if watcher.runID == "" {
		r.log.Warn("watching workflow without run id; result may resolve to non-target execution",
			zap.String("workflow_id", workflowID))
	}

	// Get workflow run handle
	run := r.client.GetWorkflow(ctx, workflowID, watcher.runID)

	// Wait for completion
	var payloads payload.Payloads
	err := run.Get(ctx, &payloads)

	// Check if canceled
	if ctx.Err() != nil {
		r.log.Debug("workflow watch canceled",
			zap.String("workflow_id", workflowID))
		return
	}

	r.log.Debug("workflow completed",
		zap.String("workflow_id", workflowID),
		zap.Int("payloads", len(payloads)),
		zap.Error(err))

	// Notify all watchers
	r.notifyCompletion(watcher, payloads, err)
}

// notifyCompletion sends EXIT events to all monitors and LINK_DOWN to linked processes.
func (r *Receiver) notifyCompletion(watcher *workflowWatcher, payloads payload.Payloads, err error) {
	r.mu.Lock()
	current, ok := r.watchers[watcher.workflowID]
	if !ok || current != watcher {
		r.mu.Unlock()
		return
	}
	monitors := make([]pid.PID, 0, len(current.monitors))
	for _, p := range current.monitors {
		monitors = append(monitors, p)
	}
	links := make([]pid.PID, 0, len(current.links))
	for _, p := range current.links {
		links = append(links, p)
	}
	if current.cancel != nil {
		current.cancel()
	}
	// Clear and remove watcher
	delete(r.watchers, current.workflowID)
	r.mu.Unlock()

	// Build workflow PID
	workflowPID := pid.PID{
		Node:   r.nodeID,
		Host:   current.taskQueue,
		UniqID: current.workflowID,
	}

	// Build result
	runtimeResult := &runtime.Result{}
	if err != nil {
		runtimeResult.Error = temporalerrors.FromTemporalError(err)
	} else if len(payloads) > 0 {
		runtimeResult.Value = payloads[0]
		if runtimeResult.Value != nil {
			r.log.Debug("temporal completion payload",
				zap.String("workflow_id", current.workflowID),
				zap.String("format", runtimeResult.Value.Format()),
				zap.String("data_type", fmt.Sprintf("%T", runtimeResult.Value.Data())))
		}
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
		fields := []zap.Field{
			zap.String("from", from.String()),
			zap.String("to", to.String()),
			zap.String("kind", kind),
		}
		if result != nil && result.Value != nil {
			fields = append(fields,
				zap.String("result_format", result.Value.Format()),
				zap.String("result_data_type", fmt.Sprintf("%T", result.Value.Data())))
		}
		r.log.Debug("sent exit event", fields...)
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
