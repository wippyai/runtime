package sandbox

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// upstreamHandler implements runtime.Upstream for workflow request queuing
type upstreamHandler struct {
	requests []runtime.Command
}

// newUpstreamHandler creates a new upstream handler
func newUpstreamHandler() *upstreamHandler {
	return &upstreamHandler{
		requests: make([]runtime.Command, 0),
	}
}

// SendRequest implements runtime.Upstream
func (h *upstreamHandler) SendRequest(cmd runtime.Command) error {
	h.requests = append(h.requests, cmd)
	return nil
}

// FlushRequests implements runtime.Upstream
func (h *upstreamHandler) FlushRequests() []runtime.Command {
	result := make([]runtime.Command, len(h.requests))
	copy(result, h.requests)

	h.requests = h.requests[:0]

	return result
}

type WorkflowInstance struct {
	workflow        process.Workflow
	registryID      registry.ID
	parentCtx       context.Context
	upstreamHandler runtime.Upstream
	started         bool
	done            bool
	result          *runtime.Result
}

// initContext creates and prepares frame context for workflow execution
// This is the internal host-like initialization
func (w *WorkflowInstance) initContext(parentCtx context.Context) (context.Context, relay.PID, error) {
	// Generate PID for this workflow instance
	pid := relay.PID{UniqID: uuid.New().String()}

	// Open frame context on parent (inherits Actor, Scope, etc. from parent)
	frameCtx, fc := ctxapi.OpenFrameContext(parentCtx)

	// Set frame metadata (like a host does)
	pairs := []ctxapi.Pair{
		{Key: runtime.FrameIDKey, Value: w.registryID},
		{Key: runtime.FramePIDKey, Value: pid},
	}

	if err := fc.SetMultiple(pairs...); err != nil {
		return nil, relay.PID{}, fmt.Errorf("failed to set frame metadata: %w", err)
	}

	// Inject upstream handler into frame context
	if err := runtime.WithUpstream(frameCtx, w.upstreamHandler); err != nil {
		return nil, relay.PID{}, fmt.Errorf("failed to set upstream handler: %w", err)
	}

	return frameCtx, pid, nil
}

// Start initializes workflow with input (acts as mini-host)
func (w *WorkflowInstance) Start(input ...interface{}) error {
	if w.started {
		return fmt.Errorf("already started")
	}

	// Prepare frame context with upstream handler (host pattern)
	frameCtx, pid, err := w.initContext(w.parentCtx)
	if err != nil {
		return err
	}

	// Set up OnComplete hook to capture workflow result
	onCompleteHook := func(_ context.Context, _ relay.PID, result *runtime.Result) {
		w.result = result
		w.done = true
	}
	if err := process.SetOnCompleteHooks(frameCtx, []process.OnComplete{onCompleteHook}); err != nil {
		return fmt.Errorf("failed to set completion hook: %w", err)
	}

	// Convert input to payloads
	payloads := make(payload.Payloads, len(input))
	for i, v := range input {
		payloads[i] = payload.NewPayload(v, payload.Lua)
	}

	// Start workflow with prepared context (like host.Launch)
	if err := w.workflow.Start(frameCtx, pid, payloads); err != nil {
		return err
	}

	w.started = true
	return nil
}

// Step advances workflow by one iteration
func (w *WorkflowInstance) Step() (process.StepResult, error) {
	if !w.started {
		return process.StepContinue, fmt.Errorf("not started - call start() first")
	}

	if w.done {
		return process.StepDone, nil
	}

	result, err := w.workflow.Step()

	if w.done {
		return process.StepDone, nil
	}

	if err != nil {
		return result, err
	}

	return result, nil
}

// Commands returns pending workflow commands from upstream handler
func (w *WorkflowInstance) Commands() []runtime.Command {
	if w.upstreamHandler == nil {
		return nil
	}

	return w.upstreamHandler.FlushRequests()
}

// IsDone checks if workflow has completed
func (w *WorkflowInstance) IsDone() bool {
	return w.done
}

// GetResult returns the final workflow result
func (w *WorkflowInstance) GetResult() *runtime.Result {
	return w.result
}

// Close terminates the workflow
func (w *WorkflowInstance) Close() {
	w.workflow.Terminate()
}
