// SPDX-License-Identifier: MPL-2.0

// Package peer implements relay-to-Temporal signal bridging, allowing
// local processes to send messages to Temporal workflows via the relay system.
package peer

import (
	"context"
	"errors"
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
	systopology "github.com/wippyai/runtime/system/topology"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.uber.org/zap"
)

// Receiver implements relay.Receiver for Temporal workflows.
// It handles topology events (monitor, link) and sends EXIT events
// when workflows complete.
type Receiver struct {
	monitorConfig temporalapi.MonitorConfig
	observers     sync.WaitGroup
	calls         sync.WaitGroup
	stopped       bool
	client        client.Client
	router        relay.Receiver
	ctx           context.Context
	handoff       temporalapi.WorkflowRunHandoff
	log           *zap.Logger
	watchers      map[string]*workflowWatcher
	cancel        context.CancelFunc
	nodeID        pid.NodeID
	clientID      string
	mu            sync.RWMutex
}

// workflowWatcher tracks watchers for a single workflow.
type workflowObservation byte

type workflowWatcher struct {
	observation    *workflowObservation
	nativeRefs     *systopology.MonitorReferences
	nativeMonitors map[string]nativeObserver
	nativeResult   *runtime.Result
	monitors       map[string]pid.PID
	links          map[string]pid.PID
	cancel         context.CancelFunc
	workflowID     string
	runID          string
	taskQueue      string
	watching       bool
}

// NewReceiver creates a new Temporal peer receiver.
func NewReceiver(ctx context.Context, nodeID pid.NodeID, temporalClient client.Client, router relay.Receiver, logger *zap.Logger, opts ...ReceiverOption) *Receiver {
	ctx, cancel := context.WithCancel(ctx)
	if logger == nil {
		logger = zap.NewNop()
	}
	r := &Receiver{
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
	r.monitorConfig.InitDefaults()
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Send implements relay.Receiver.
// Handles topology events routed to this Temporal peer.
func (r *Receiver) Send(pkg *relay.Package) error {
	return r.SendContext(context.Background(), pkg)
}

// SendContext bounds synchronous Temporal calls by both the caller and receiver
// lifetime. Workflow observation itself remains owned by the receiver.
func (r *Receiver) SendContext(ctx context.Context, pkg *relay.Package) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.ctx.Err(); err != nil {
		return err
	}
	if pkg == nil {
		return fmt.Errorf("nil Temporal relay package")
	}
	for _, message := range pkg.Messages {
		if message == nil {
			return fmt.Errorf("nil Temporal relay message")
		}
		if message.Topic != topology.TopicEvents {
			continue
		}
		for _, body := range message.Payloads {
			if fields, ok := body.Data().(map[string]any); ok {
				if fields["kind"] == topology.MonitorRequest || fields["kind"] == topology.MonitorRelease {
					return fmt.Errorf("Temporal peer does not yet support acknowledged monitor control")
				}
			}
		}
	}
	r.mu.Lock()
	if r.stopped || r.ctx.Err() != nil {
		r.mu.Unlock()
		return context.Canceled
	}
	r.calls.Add(1)
	r.mu.Unlock()
	defer r.calls.Done()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	defer cancel()
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
				if err := r.handleCancelRequestContext(ctx, event, pkg.Target); err != nil && firstErr == nil {
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
		if err := r.signalWorkflowContext(ctx, pkg.Source, pkg.Target, msg); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if firstErr == nil {
		relay.ReleasePackage(pkg)
	}
	return firstErr
}

func (r *Receiver) handleCancelRequest(event *topology.CancelEvent, target pid.PID) error {
	return r.handleCancelRequestContext(r.ctx, event, target)
}
func (r *Receiver) handleCancelRequestContext(ctx context.Context, event *topology.CancelEvent, target pid.PID) error {
	if r.client == nil {
		return fmt.Errorf("temporal client not available")
	}

	workflowID := target.UniqID
	if workflowID == "" {
		return fmt.Errorf("workflow ID is empty")
	}

	return r.client.CancelWorkflow(ctx, workflowID, event.Reason)
}

func (r *Receiver) signalWorkflow(from, target pid.PID, msg *relay.Message) error {
	return r.signalWorkflowContext(r.ctx, from, target, msg)
}
func (r *Receiver) signalWorkflowContext(ctx context.Context, from, target pid.PID, msg *relay.Message) error {
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

	ctx = temporalprop.WithRelaySignal(ctx, target.UniqID, msg.Topic)
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
	if r.stopped || r.ctx.Err() != nil {
		return context.Canceled
	}

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
		r.startWorkflowWatchLocked(watcher)
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
	if r.stopped || r.ctx.Err() != nil {
		return context.Canceled
	}

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
		r.startWorkflowWatchLocked(watcher)
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
	activeNative := false
	for _, observer := range watcher.nativeMonitors {
		if observer.result == nil {
			activeNative = true
			break
		}
	}
	if len(watcher.monitors) == 0 && len(watcher.links) == 0 && !activeNative {
		if watcher.cancel != nil {
			watcher.cancel()
		}
		if watcher.nativeRefs == nil {
			delete(r.watchers, watcher.workflowID)
		}
		watcher.watching = false
	}
}

func (r *Receiver) assignRunIDIfAvailable(watcher *workflowWatcher) {
	if watcher == nil || watcher.watching || watcher.runID != "" || watcher.workflowID == "" {
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
	r.mu.Lock()
	if watcher.observation == nil {
		watcher.observation = new(workflowObservation)
	}
	attempt := watcher.observation
	r.mu.Unlock()
	r.watchWorkflowAttempt(ctx, watcher, attempt)
}
func (r *Receiver) watchWorkflowAttempt(ctx context.Context, watcher *workflowWatcher, attempt *workflowObservation) {
	if err := ctx.Err(); err != nil {
		r.suspendWorkflowObservationAttempt(watcher, attempt, err)
		return
	}
	r.mu.RLock()
	workflowID, runID := watcher.workflowID, watcher.runID
	r.mu.RUnlock()

	r.log.Debug("starting workflow watch",
		zap.String("workflow_id", workflowID),
		zap.String("run_id", runID))

	if runID == "" {
		r.log.Warn("watching workflow without run id; result may resolve to non-target execution",
			zap.String("workflow_id", workflowID))
	}

	// Get workflow run handle
	run := r.client.GetWorkflow(ctx, workflowID, runID)

	// Wait for completion
	var payloads payload.Payloads
	err := run.Get(ctx, &payloads)

	// Check if canceled
	if ctx.Err() != nil {
		r.suspendWorkflowObservationAttempt(watcher, attempt, ctx.Err())
		return
	}
	// SDK v1.48 Get wraps failed/canceled/terminated/timed-out workflow close
	// events in WorkflowExecutionError. History transport and payload decode
	// errors are returned directly and do not establish a terminal result.
	var terminal *temporal.WorkflowExecutionError
	if err != nil && !errors.As(err, &terminal) {
		r.suspendWorkflowObservationAttempt(watcher, attempt, err)
		return
	}

	r.log.Debug("workflow completed",
		zap.String("workflow_id", workflowID),
		zap.Int("payloads", len(payloads)),
		zap.Error(err))

	// Notify all watchers
	r.notifyCompletionAttempt(watcher, payloads, err, attempt)
}

// suspendWorkflowObservation retains ownership after an inconclusive lookup.
// A later explicit observation can restart it. No timer or connection failure
// proves workflow death, and an old observer cannot modify a replacement.
func (r *Receiver) suspendWorkflowObservation(watcher *workflowWatcher, cause error) {
	r.suspendWorkflowObservationAttempt(watcher, nil, cause)
}
func (r *Receiver) suspendWorkflowObservationAttempt(watcher *workflowWatcher, attempt *workflowObservation, cause error) {
	r.mu.Lock()
	current, exists := r.watchers[watcher.workflowID]
	if !exists || current != watcher || (attempt != nil && current.observation != attempt) {
		r.mu.Unlock()
		return
	}
	if watcher.cancel != nil {
		watcher.cancel()
		watcher.cancel = nil
	}
	watcher.watching = false
	r.mu.Unlock()
	r.log.Warn("workflow observation unresolved", zap.String("workflow_id", watcher.workflowID), zap.Error(cause))
}

// notifyCompletion sends EXIT events to all monitors and LINK_DOWN to linked processes.
func (r *Receiver) notifyCompletion(watcher *workflowWatcher, payloads payload.Payloads, err error) {
	r.notifyCompletionAttempt(watcher, payloads, err, nil)
}
func (r *Receiver) notifyCompletionAttempt(watcher *workflowWatcher, payloads payload.Payloads, err error, attempt *workflowObservation) {
	runtimeResult := &runtime.Result{}
	if err != nil {
		runtimeResult.Error = temporalerrors.FromTemporalError(err)
	} else if len(payloads) > 0 {
		runtimeResult.Value = payloads[0]
	}
	r.mu.Lock()
	current, ok := r.watchers[watcher.workflowID]
	if !ok || current != watcher || (attempt != nil && current.observation != attempt) {
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
	// Native completions retain bounded result/reference state for explicit retry.
	var native []nativeObserver
	if current.nativeRefs != nil {
		current.nativeResult = &runtime.Result{} // terminal marker; pending records own result bytes
		current.watching = false
		for key, observer := range current.nativeMonitors {
			if observer.result != nil {
				continue
			} // belongs to an older completed run
			if releaseErr := current.nativeRefs.Release(observer.request.Caller, observer.request.Reference); releaseErr != nil {
				r.log.Error("native monitor reference completion failed", zap.Error(releaseErr))
				continue
			}
			observer.result = runtimeResult
			current.nativeMonitors[key] = observer
			native = append(native, observer)
		}
		clear(current.monitors)
		clear(current.links)
	} else {
		delete(r.watchers, current.workflowID)
	}
	r.mu.Unlock()

	// Build workflow PID
	workflowPID := pid.PID{
		Node:   r.nodeID,
		Host:   current.taskQueue,
		UniqID: current.workflowID,
	}

	for _, observer := range native {
		r.deliverNativeResult(r.ctx, current, observer, runtimeResult)
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
	r.stopped = true

	for _, watcher := range r.watchers {
		if watcher.cancel != nil {
			watcher.cancel()
		}
	}
	r.watchers = make(map[string]*workflowWatcher)
	r.mu.Unlock()
	r.calls.Wait()
	r.observers.Wait()
}

var _ relay.Receiver = (*Receiver)(nil)
var _ relay.ContextSender = (*Receiver)(nil)
