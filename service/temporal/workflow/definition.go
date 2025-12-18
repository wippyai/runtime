package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	workflowapi "github.com/wippyai/runtime/api/runtime/workflow"
	"github.com/wippyai/runtime/api/topology"
	temporalsvc "github.com/wippyai/runtime/service/temporal"
	"github.com/wippyai/runtime/service/temporal/propagator"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/converter"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
)

// workflowTimeRef implements clock.TimeReference using Temporal's deterministic time.
type workflowTimeRef struct {
	env       bindings.WorkflowEnvironment
	startTime time.Time
}

func (r *workflowTimeRef) Now() time.Time       { return r.env.Now() }
func (r *workflowTimeRef) StartTime() time.Time { return r.startTime }

// DefinitionFactory creates workflow definition instances.
// This is registered with the Temporal worker and creates new Definition
// instances for each workflow execution.
type DefinitionFactory struct {
	ID  registry.ID
	log *zap.Logger
	ctx context.Context
}

// WithContext returns a new factory with the given context.
// This is called by the worker when registering the workflow.
func (f *DefinitionFactory) WithContext(ctx context.Context) any {
	return &DefinitionFactory{
		ID:  f.ID,
		log: f.log,
		ctx: ctx,
	}
}

// NewWorkflowDefinition creates a new workflow definition instance.
// Called by Temporal SDK for each workflow execution.
func (f *DefinitionFactory) NewWorkflowDefinition() bindings.WorkflowDefinition {
	return &Definition{
		id:  f.ID,
		log: f.log,
		ctx: f.ctx,
	}
}

// incomingSignal represents a queued signal to be delivered to the workflow.
type incomingSignal struct {
	Name     string
	Payloads payload.Payloads
}

// childExitEvent represents a child workflow completion to be delivered as EXIT event.
type childExitEvent struct {
	ChildPID pid.PID
	Result   payload.Payload
	Error    error
}

// updateState tracks the lifecycle of an update.
type updateState int

const (
	updatePending  updateState = iota // Waiting for ack/nak from Lua
	updateAccepted                    // Lua sent ack, waiting for ok/error
	updateRejected                    // Lua sent nak, update rejected
	updateComplete                    // Lua sent ok/error, update completed
)

// pendingUpdate represents an update in progress.
type pendingUpdate struct {
	Name      string
	ID        string
	Payloads  payload.Payloads
	State     updateState
	Callbacks bindings.UpdateCallbacks
}

// workflowTimer tracks an active timer in workflow context.
type workflowTimer struct {
	ID       uint64
	PID      pid.PID
	Topic    string
	Duration time.Duration
	Canceled bool
}

// workflowTicker tracks an active ticker in workflow context.
type workflowTicker struct {
	ID       uint64
	PID      pid.PID
	Topic    string
	Duration time.Duration
	Stopped  bool
}

// Definition implements Temporal's WorkflowDefinition interface.
// It bridges the Process model with Temporal's workflow execution.
type Definition struct {
	id         registry.ID
	log        *zap.Logger
	replayLog  *propagator.ReplayLogger // replay-safe logger for workflow code
	ctx        context.Context
	execCtx    context.Context
	env        bindings.WorkflowEnvironment
	dc         converter.DataConverter
	proc       process.Process
	result     *runtime.Result
	output     process.StepOutput // reusable output buffer
	signals    []incomingSignal   // queued signals
	childExits []childExitEvent   // queued child workflow exit events
	canceled   bool               // true if workflow was cancelled
	queryState map[string]any     // queryable state exposed by workflow

	// Update handling: updates are queued, then delivered to Lua.
	// Lua responds with ack/nak (Accept/Reject), then ok/error (Complete).
	pendingUpdates []*pendingUpdate          // updates waiting to be delivered
	activeUpdates  map[string]*pendingUpdate // updates being processed (by ID)

	// Timer tracking for time.after/time.timer
	timerCounter uint64
	activeTimers map[uint64]*workflowTimer

	// Ticker tracking for time.ticker
	tickerCounter uint64
	activeTickers map[uint64]*workflowTicker
}

// Execute implements WorkflowDefinition.Execute.
// Called by Temporal SDK to start workflow execution.
func (d *Definition) Execute(env bindings.WorkflowEnvironment, header *commonpb.Header, input *commonpb.Payloads) {
	d.env = env
	d.dc = env.GetDataConverter()

	// Create replay-safe logger
	d.replayLog = propagator.NewReplayLogger(d.log, env.IsReplaying)

	factory := process.GetFactory(d.ctx)
	if factory == nil {
		d.env.Complete(nil, fmt.Errorf("no process factory found"))
		return
	}

	proc, meta, err := factory.Create(d.id)
	if err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to create workflow process: %w", err))
		return
	}
	d.proc = proc

	var payloads payload.Payloads
	if err := d.dc.FromPayloads(input, &payloads); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to decode input payloads: %w", err))
		return
	}

	processPID := pid.PID{
		Node:   "",
		Host:   "temporal",
		UniqID: env.WorkflowInfo().WorkflowExecution.ID,
	}

	execCtx, fc := ctxapi.OpenFrameContextOn(d.ctx, d.ctx)
	pairs := []ctxapi.Pair{
		{Key: runtime.FrameIDKey, Value: d.id},
		{Key: runtime.FramePIDKey, Value: processPID},
	}

	if err := fc.SetMultiple(pairs...); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to set frame context: %w", err))
		return
	}

	// Extract context values from header and set in frame context
	if ctxValues, err := propagator.ExtractFromHeader(header); err != nil {
		d.replayLog.Warn("failed to extract context from header", zap.Error(err))
	} else if len(ctxValues) > 0 {
		values, err := ctxapi.GetOrCreateValues(execCtx)
		if err == nil {
			for k, v := range ctxValues {
				values.Set(k, v)
			}
			d.replayLog.Debug("extracted context values from header",
				zap.Int("count", len(ctxValues)))
		}
	}

	// Extract and apply security context from header
	if secPayload, err := propagator.ExtractSecurityFromHeader(header); err != nil {
		d.replayLog.Warn("failed to extract security from header", zap.Error(err))
	} else if secPayload != nil {
		if err := propagator.ApplySecurityPayload(execCtx, secPayload); err != nil {
			d.replayLog.Warn("failed to apply security context", zap.Error(err))
		}
	}

	// Set deterministic time reference for workflow execution
	timeRef := &workflowTimeRef{
		env:       env,
		startTime: env.Now(),
	}
	if err := clockapi.WithTimeReference(execCtx, timeRef); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to set time reference: %w", err))
		return
	}

	// Mark context as deterministic for workflow-safe modules (uuid, etc.)
	if err := workflowapi.SetDeterministic(execCtx); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to set deterministic mode: %w", err))
		return
	}

	// Set workflow info provider for workflow.info() calls
	if err := workflowapi.SetInfoProvider(execCtx, d); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to set info provider: %w", err))
		return
	}

	d.execCtx = execCtx

	// Register cancel handler
	env.RegisterCancelHandler(d.handleCancel)

	// Register signal handler
	env.RegisterSignalHandler(d.handleSignal)

	// Register query handler
	env.RegisterQueryHandler(d.handleQuery)

	// Register update handler
	env.RegisterUpdateHandler(d.handleUpdate)

	// Initialize query state and update tracking
	d.queryState = make(map[string]any)
	d.activeUpdates = make(map[string]*pendingUpdate)
	d.activeTimers = make(map[uint64]*workflowTimer)
	d.activeTickers = make(map[uint64]*workflowTicker)

	method := "main"
	if meta != nil && meta.Method != "" {
		method = meta.Method
	}

	if err := d.proc.Init(execCtx, method, payloads); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to start workflow: %w", err))
		return
	}
}

// handleCancel is called when the workflow receives a cancellation request.
// Instead of completing immediately, we send a CANCEL event to the process
// so Lua code can handle cleanup gracefully.
func (d *Definition) handleCancel() {
	d.canceled = true
	// Queue cancel event for delivery to process via @pid/events topic
	// Use JSON format so it transcodes properly to Lua table
	cancelEvent := map[string]any{
		"at":   d.env.Now().Format(time.RFC3339),
		"kind": topology.Cancel,
		"from": topology.SystemPID.String(),
	}
	jsonBytes, _ := json.Marshal(cancelEvent)
	d.signals = append(d.signals, incomingSignal{
		Name:     topology.TopicEvents,
		Payloads: payload.Payloads{payload.NewPayload(jsonBytes, payload.JSON)},
	})
}

// handleSignal queues incoming signals for delivery to the process.
func (d *Definition) handleSignal(name string, input *commonpb.Payloads, _ *commonpb.Header) error {
	var payloads payload.Payloads
	if input != nil {
		if err := d.dc.FromPayloads(input, &payloads); err != nil {
			return fmt.Errorf("failed to decode signal payloads: %w", err)
		}
	}
	d.signals = append(d.signals, incomingSignal{Name: name, Payloads: payloads})
	return nil
}

// handleQuery handles incoming queries by returning the queryable state.
func (d *Definition) handleQuery(queryType string, _ *commonpb.Payloads, _ *commonpb.Header) (*commonpb.Payloads, error) {
	switch queryType {
	case "state":
		// Return the current query state
		result := d.queryState
		if result == nil {
			result = make(map[string]any)
		}
		return d.dc.ToPayloads(result)
	case "pid":
		// Return the workflow PID
		return d.dc.ToPayloads(d.env.WorkflowInfo().WorkflowExecution.ID)
	default:
		// Check if query type exists in state
		if val, ok := d.queryState[queryType]; ok {
			return d.dc.ToPayloads(val)
		}
		return nil, fmt.Errorf("unknown query type: %s", queryType)
	}
}

// handleUpdate queues incoming updates for delivery to the workflow.
// Updates are delivered as messages with topic "update.<name>" from pseudo-PID "{update|<id>}".
// Lua responds with: ack (Accept), nak (Reject), ok (Complete success), error (Complete failure).
// Note: Accept/Reject cannot be called here - SDK state is still "New".
func (d *Definition) handleUpdate(name string, id string, input *commonpb.Payloads, _ *commonpb.Header, callbacks bindings.UpdateCallbacks) {
	d.replayLog.Debug("handleUpdate", zap.String("name", name), zap.String("id", id))

	upd := &pendingUpdate{
		Name:      name,
		ID:        id,
		State:     updatePending,
		Callbacks: callbacks,
	}

	if input != nil {
		if err := d.dc.FromPayloads(input, &upd.Payloads); err != nil {
			d.replayLog.Error("handleUpdate decode failed", zap.Error(err))
			// Mark for rejection on next workflow task
			upd.Name = "__reject__"
			upd.Payloads = payload.Payloads{payload.NewString(err.Error())}
		}
	}

	d.pendingUpdates = append(d.pendingUpdates, upd)
}

// OnWorkflowTaskStarted implements WorkflowDefinition.OnWorkflowTaskStarted.
// Called by Temporal SDK when a workflow task is ready to execute.
func (d *Definition) OnWorkflowTaskStarted(_ time.Duration) {
	for {
		// Drain signals and convert to events
		var events []process.Event
		if len(d.signals) > 0 {
			for _, sig := range d.signals {
				events = append(events, process.Event{
					Type: process.EventMessage,
					Data: &relay.Package{
						Messages: []*relay.Message{{
							Topic:    sig.Name,
							Payloads: sig.Payloads,
						}},
					},
				})
			}
			d.signals = d.signals[:0]
		}

		// Drain child exit events and convert to EXIT events
		if len(d.childExits) > 0 {
			for _, exit := range d.childExits {
				exitEvent := &topology.ExitEvent{
					At:   d.env.Now(),
					Kind: topology.Exit,
					From: exit.ChildPID,
					Result: &runtime.Result{
						Value: exit.Result,
						Error: exit.Error,
					},
				}
				events = append(events, process.Event{
					Type: process.EventMessage,
					Data: &relay.Package{
						Messages: []*relay.Message{{
							Topic:    topology.TopicEvents,
							Payloads: payload.Payloads{payload.New(exitEvent)},
						}},
					},
				})
			}
			d.childExits = d.childExits[:0]
		}

		// Process pending updates - deliver as messages, track for responses
		// State is now RequestInitiated, so Accept/Reject can be called
		if len(d.pendingUpdates) > 0 {
			d.replayLog.Debug("processing pending updates", zap.Int("count", len(d.pendingUpdates)))
			for _, upd := range d.pendingUpdates {
				// Handle decode errors - reject immediately
				if upd.Name == "__reject__" {
					var errMsg string
					if len(upd.Payloads) > 0 {
						if s, ok := upd.Payloads[0].Data().(string); ok {
							errMsg = s
						}
					}
					if errMsg == "" {
						errMsg = "update decode error"
					}
					upd.State = updateRejected
					upd.Callbacks.Reject(fmt.Errorf("%s", errMsg))
					continue
				}

				// Track update for response handling
				d.activeUpdates[upd.ID] = upd

				// Deliver as normal message - topic is update name, payload is update args
				updatePID := pid.PID{Host: "update", UniqID: upd.ID}
				events = append(events, process.Event{
					Type: process.EventMessage,
					Data: &relay.Package{
						Source: updatePID,
						Messages: []*relay.Message{{
							Topic:    upd.Name,
							Payloads: upd.Payloads,
						}},
					},
				})
			}
			d.pendingUpdates = d.pendingUpdates[:0]
		}

		d.output.Reset()
		err := d.proc.Step(events, &d.output)
		d.replayLog.Debug("proc.Step", zap.Int("status", int(d.output.Status())), zap.Error(err))

		if d.result != nil {
			d.completeWithResult()
			return
		}

		if err != nil {
			d.env.Complete(nil, fmt.Errorf("workflow step failed: %w", err))
			return
		}

		switch d.output.Status() {
		case process.StepYield:
			d.output.ForEachYield(func(y process.Yield) {
				if err := d.executeCommand(y.Cmd, y.Tag); err != nil {
					d.env.Complete(nil, fmt.Errorf("failed to execute command: %w", err))
				}
			})
			return

		case process.StepDone:
			if result := d.output.Result(); result != nil {
				res, err := d.dc.ToPayloads(payload.Payloads{result})
				if err != nil {
					d.env.Complete(nil, fmt.Errorf("failed to encode result: %w", err))
					return
				}
				d.env.Complete(res, nil)
			} else {
				d.env.Complete(nil, nil)
			}
			return

		case process.StepIdle:
			return

		case process.StepUpgrade:
			d.executeContinueAsNew()
			return

		case process.StepContinue:
			d.output.ForEachYield(func(y process.Yield) {
				if err := d.executeCommand(y.Cmd, y.Tag); err != nil {
					d.env.Complete(nil, fmt.Errorf("failed to execute command: %w", err))
				}
			})
		}
	}
}

// executeContinueAsNew handles process.upgrade() by triggering Temporal's ContinueAsNew.
func (d *Definition) executeContinueAsNew() {
	req := d.output.Upgrade()
	if req == nil {
		d.env.Complete(nil, fmt.Errorf("upgrade: no request"))
		return
	}

	// Determine workflow type (empty = same workflow)
	workflowType := d.id.String()
	if req.Source.Name != "" {
		workflowType = req.Source.String()
	}

	// Convert input payloads
	var input *commonpb.Payloads
	if len(req.Input) > 0 {
		var err error
		input, err = d.dc.ToPayloads(req.Input)
		if err != nil {
			d.env.Complete(nil, fmt.Errorf("upgrade: failed to encode input: %w", err))
			return
		}
	}

	// Create ContinueAsNewError
	continueErr := &bindings.ContinueAsNewError{
		WorkflowType:  &bindings.WorkflowType{Name: workflowType},
		Input:         input,
		Header:        d.getContextHeader(),
		TaskQueueName: d.env.WorkflowInfo().TaskQueueName,
	}

	d.env.Complete(nil, continueErr)
}

func (d *Definition) completeWithResult() {
	if d.result.Error != nil {
		// Convert to Temporal ApplicationError preserving error kind and retryability
		d.env.Complete(nil, temporalsvc.ToApplicationError(d.result.Error))
		return
	}

	if d.result.Value == nil {
		d.env.Complete(nil, nil)
		return
	}

	res, err := d.dc.ToPayloads(payload.Payloads{d.result.Value})
	if err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to encode result: %w", err))
		return
	}

	d.env.Complete(res, nil)
}

func (d *Definition) executeCommand(cmd dispatcher.Command, tag uint64) error {
	switch c := cmd.(type) {
	case *ActivityCmd:
		return d.executeActivity(c, tag)
	case *LocalActivityCmd:
		return d.executeLocalActivity(c, tag)
	case clockapi.SleepCmd:
		return d.executeSleep(c, tag)
	case clockapi.TimerStartCmd:
		return d.executeTimerStart(c, tag)
	case clockapi.TimerStopCmd:
		return d.executeTimerStop(c, tag)
	case clockapi.TimerResetCmd:
		return d.executeTimerReset(c, tag)
	case clockapi.TickerStartCmd:
		return d.executeTickerStart(c, tag)
	case clockapi.TickerStopCmd:
		return d.executeTickerStop(c, tag)
	case *ChildWorkflowCmd:
		return d.executeChildWorkflow(c, tag)
	case *SignalCmd:
		return d.executeSignal(c, tag)
	case *function.CallCmd:
		return d.executeFunctionCall(c, tag)
	case *process.SendCmd:
		return d.executeProcessSend(c, tag)
	case *process.SpawnCmd:
		return d.executeProcessSpawn(c, tag)
	case *process.TerminateCmd:
		return d.executeProcessTerminate(c, tag)
	case *process.CancelCmd:
		return d.executeProcessCancel(c, tag)
	case *process.MonitorCmd:
		return d.executeProcessMonitor(c, tag)
	case *process.UnmonitorCmd:
		return d.executeProcessUnmonitor(c, tag)
	case *process.LinkCmd:
		return d.executeProcessLink(c, tag)
	case *process.UnlinkCmd:
		return d.executeProcessUnlink(c, tag)
	case *process.CallCmd:
		return d.executeProcessCall(c, tag)
	case *workflowapi.SideEffectCmd:
		return d.executeSideEffect(c, tag)
	case *workflowapi.CallCmd:
		return d.executeWorkflowCall(c, tag)
	case *workflowapi.VersionCmd:
		return d.executeVersion(c, tag)
	case *workflowapi.UpsertAttrsCmd:
		return d.executeUpsertAttrs(c, tag)
	default:
		return fmt.Errorf("unknown command type: %T", cmd)
	}
}

func (d *Definition) executeActivity(cmd *ActivityCmd, tag uint64) error {
	opts, err := cmd.Options.ToExecuteActivityOptions()
	if err != nil {
		return fmt.Errorf("failed to convert activity options: %w", err)
	}

	args, err := d.dc.ToPayloads(cmd.Args)
	if err != nil {
		return fmt.Errorf("failed to convert activity arguments: %w", err)
	}

	d.env.ExecuteActivity(bindings.ExecuteActivityParams{
		ExecuteActivityOptions: opts,
		ActivityType:           bindings.ActivityType{Name: cmd.Name},
		Input:                  args,
		Header:                 d.getContextHeader(),
	}, func(result *commonpb.Payloads, err error) {
		d.handleActivityResult(tag, result, err)
	})

	return nil
}

func (d *Definition) executeLocalActivity(cmd *LocalActivityCmd, tag uint64) error {
	opts, err := cmd.Options.ToLocalActivityOptions()
	if err != nil {
		return fmt.Errorf("failed to convert local activity options: %w", err)
	}

	// Convert payloads to []interface{} as required by local activities
	inputArgs := make([]interface{}, 0, len(cmd.Args))
	for _, p := range cmd.Args {
		inputArgs = append(inputArgs, p)
	}

	d.env.ExecuteLocalActivity(bindings.ExecuteLocalActivityParams{
		ExecuteLocalActivityOptions: opts,
		ActivityType:                cmd.Name,
		InputArgs:                   inputArgs,
		WorkflowInfo:                d.env.WorkflowInfo(),
		DataConverter:               d.dc,
	}, func(lar *bindings.LocalActivityResultWrapper) {
		if lar.Err != nil {
			// Convert Temporal error to apierror
			d.resumeProcess(tag, nil, temporalsvc.FromTemporalError(lar.Err))
			return
		}
		var values payload.Payloads
		if err := d.dc.FromPayloads(lar.Result, &values); err != nil {
			d.resumeProcess(tag, nil, temporalsvc.FromTemporalError(err))
			return
		}
		if len(values) > 0 {
			d.resumeProcess(tag, values[0], nil)
		} else {
			d.resumeProcess(tag, nil, nil)
		}
	})

	return nil
}

func (d *Definition) handleActivityResult(tag uint64, result *commonpb.Payloads, err error) {
	if err != nil {
		// Convert Temporal error to apierror
		d.resumeProcess(tag, nil, temporalsvc.FromTemporalError(err))
		return
	}

	var values payload.Payloads
	if err := d.dc.FromPayloads(result, &values); err != nil {
		d.resumeProcess(tag, nil, temporalsvc.FromTemporalError(err))
		return
	}

	if len(values) > 0 {
		d.resumeProcess(tag, values[0], nil)
	} else {
		d.resumeProcess(tag, nil, nil)
	}
}

func (d *Definition) executeSleep(cmd clockapi.SleepCmd, tag uint64) error {
	d.env.NewTimer(cmd.Duration, workflow.TimerOptions{}, func(_ *commonpb.Payloads, err error) {
		if err != nil {
			d.resumeProcess(tag, nil, temporalsvc.FromTemporalError(err))
			return
		}
		d.resumeProcess(tag, payload.NewPayload(true, payload.Golang), nil)
	})
	return nil
}

func (d *Definition) executeTimerStart(cmd clockapi.TimerStartCmd, tag uint64) error {
	if cmd.Duration <= 0 {
		d.resumeProcess(tag, clockapi.TimerStartResult{ID: 0}, nil)
		return nil
	}

	d.timerCounter++
	timerID := d.timerCounter

	timer := &workflowTimer{
		ID:       timerID,
		PID:      cmd.PID,
		Topic:    cmd.Topic,
		Duration: cmd.Duration,
	}
	d.activeTimers[timerID] = timer

	d.env.NewTimer(cmd.Duration, workflow.TimerOptions{}, func(_ *commonpb.Payloads, err error) {
		t, ok := d.activeTimers[timerID]
		if !ok || t.Canceled {
			return
		}
		delete(d.activeTimers, timerID)

		if err != nil {
			return
		}

		// Deliver timer fire as a signal to the topic
		fireTime := d.env.Now().UnixNano()
		d.signals = append(d.signals, incomingSignal{
			Name:     t.Topic,
			Payloads: payload.Payloads{payload.NewPayload(fireTime, payload.Golang)},
		})
	})

	d.resumeProcess(tag, clockapi.TimerStartResult{
		ID: timerID,
		Stop: func() {
			if t, ok := d.activeTimers[timerID]; ok {
				t.Canceled = true
				delete(d.activeTimers, timerID)
			}
		},
	}, nil)

	return nil
}

func (d *Definition) executeTimerStop(cmd clockapi.TimerStopCmd, tag uint64) error {
	timer, ok := d.activeTimers[cmd.TimerID]
	if !ok {
		d.resumeProcess(tag, false, nil)
		return nil
	}

	timer.Canceled = true
	delete(d.activeTimers, cmd.TimerID)
	d.resumeProcess(tag, true, nil)
	return nil
}

func (d *Definition) executeTimerReset(cmd clockapi.TimerResetCmd, tag uint64) error {
	timer, ok := d.activeTimers[cmd.TimerID]
	if !ok {
		d.resumeProcess(tag, false, nil)
		return nil
	}

	// Cancel old timer
	timer.Canceled = true
	delete(d.activeTimers, cmd.TimerID)

	// Create new timer with same ID
	newTimer := &workflowTimer{
		ID:       cmd.TimerID,
		PID:      timer.PID,
		Topic:    timer.Topic,
		Duration: cmd.Duration,
	}
	d.activeTimers[cmd.TimerID] = newTimer

	d.env.NewTimer(cmd.Duration, workflow.TimerOptions{}, func(_ *commonpb.Payloads, err error) {
		t, ok := d.activeTimers[cmd.TimerID]
		if !ok || t.Canceled {
			return
		}
		delete(d.activeTimers, cmd.TimerID)

		if err != nil {
			return
		}

		fireTime := d.env.Now().UnixNano()
		d.signals = append(d.signals, incomingSignal{
			Name:     t.Topic,
			Payloads: payload.Payloads{payload.NewPayload(fireTime, payload.Golang)},
		})
	})

	d.resumeProcess(tag, true, nil)
	return nil
}

func (d *Definition) executeTickerStart(cmd clockapi.TickerStartCmd, tag uint64) error {
	if cmd.Duration <= 0 {
		d.resumeProcess(tag, clockapi.TickerStartResult{ID: 0}, nil)
		return nil
	}

	d.tickerCounter++
	tickerID := d.tickerCounter

	ticker := &workflowTicker{
		ID:       tickerID,
		PID:      cmd.PID,
		Topic:    cmd.Topic,
		Duration: cmd.Duration,
	}
	d.activeTickers[tickerID] = ticker

	// Schedule first tick
	d.scheduleNextTick(tickerID)

	d.resumeProcess(tag, clockapi.TickerStartResult{
		ID: tickerID,
		Stop: func() {
			if t, ok := d.activeTickers[tickerID]; ok {
				t.Stopped = true
				delete(d.activeTickers, tickerID)
			}
		},
	}, nil)

	return nil
}

func (d *Definition) scheduleNextTick(tickerID uint64) {
	ticker, ok := d.activeTickers[tickerID]
	if !ok || ticker.Stopped {
		return
	}

	d.env.NewTimer(ticker.Duration, workflow.TimerOptions{}, func(_ *commonpb.Payloads, err error) {
		t, ok := d.activeTickers[tickerID]
		if !ok || t.Stopped {
			return
		}

		if err != nil {
			return
		}

		// Deliver tick as signal
		tickTime := d.env.Now().UnixNano()
		d.signals = append(d.signals, incomingSignal{
			Name:     t.Topic,
			Payloads: payload.Payloads{payload.NewPayload(tickTime, payload.Golang)},
		})

		// Schedule next tick
		d.scheduleNextTick(tickerID)
	})
}

func (d *Definition) executeTickerStop(cmd clockapi.TickerStopCmd, tag uint64) error {
	ticker, ok := d.activeTickers[cmd.TickerID]
	if !ok {
		d.resumeProcess(tag, nil, nil)
		return nil
	}

	ticker.Stopped = true
	delete(d.activeTickers, cmd.TickerID)
	d.resumeProcess(tag, nil, nil)
	return nil
}

func (d *Definition) executeChildWorkflow(cmd *ChildWorkflowCmd, tag uint64) error {
	args, err := d.dc.ToPayloads(cmd.Args)
	if err != nil {
		return fmt.Errorf("failed to convert child workflow arguments: %w", err)
	}

	params := bindings.ExecuteWorkflowParams{
		WorkflowType: &bindings.WorkflowType{Name: cmd.Name},
		Input:        args,
		Header:       d.getContextHeader(),
	}

	if cmd.Options != nil {
		if cmd.Options.WorkflowID != "" {
			params.WorkflowOptions.WorkflowID = cmd.Options.WorkflowID
		}
		if cmd.Options.TaskQueue != "" {
			params.WorkflowOptions.TaskQueueName = cmd.Options.TaskQueue
		}
		if cmd.Options.ExecutionTimeout != "" {
			dur, err := time.ParseDuration(cmd.Options.ExecutionTimeout)
			if err == nil {
				params.WorkflowOptions.WorkflowExecutionTimeout = dur
			}
		}
		if cmd.Options.RunTimeout != "" {
			dur, err := time.ParseDuration(cmd.Options.RunTimeout)
			if err == nil {
				params.WorkflowOptions.WorkflowRunTimeout = dur
			}
		}
		if cmd.Options.TaskTimeout != "" {
			dur, err := time.ParseDuration(cmd.Options.TaskTimeout)
			if err == nil {
				params.WorkflowOptions.WorkflowTaskTimeout = dur
			}
		}
		params.WorkflowOptions.WaitForCancellation = cmd.Options.WaitForCancellation
		if cmd.Options.RetryPolicy != nil {
			rp, err := cmd.Options.RetryPolicy.ToCommonRetryPolicy()
			if err == nil {
				params.WorkflowOptions.RetryPolicy = rp
			}
		}
		if cmd.Options.ParentClosePolicy != "" {
			params.WorkflowOptions.ParentClosePolicy = parseParentClosePolicy(cmd.Options.ParentClosePolicy)
		}
	}

	d.env.ExecuteChildWorkflow(params, func(result *commonpb.Payloads, err error) {
		d.handleActivityResult(tag, result, err)
	}, func(_ bindings.WorkflowExecution, _ error) {
		// Child workflow started callback
	})

	return nil
}

func (d *Definition) executeSignal(cmd *SignalCmd, tag uint64) error {
	var arg *commonpb.Payloads
	if cmd.Arg != nil {
		var err error
		arg, err = d.dc.ToPayloads(payload.Payloads{cmd.Arg})
		if err != nil {
			return fmt.Errorf("failed to convert signal argument: %w", err)
		}
	}

	d.env.SignalExternalWorkflow(
		"",
		cmd.WorkflowID,
		cmd.RunID,
		cmd.SignalName,
		arg,
		nil,
		nil,
		false,
		func(_ *commonpb.Payloads, err error) {
			if err != nil {
				d.resumeProcess(tag, nil, temporalsvc.FromTemporalError(err))
				return
			}
			d.resumeProcess(tag, payload.NewPayload(true, payload.Golang), nil)
		},
	)

	return nil
}

func (d *Definition) executeFunctionCall(cmd *function.CallCmd, tag uint64) error {
	activityName := cmd.Task.ID.String()

	args, err := d.dc.ToPayloads(cmd.Task.Payloads)
	if err != nil {
		return fmt.Errorf("failed to convert arguments: %w", err)
	}

	// Default activity options - use workflow's task queue
	opts := bindings.ExecuteActivityOptions{
		TaskQueueName:       d.env.WorkflowInfo().TaskQueueName,
		StartToCloseTimeout: 10 * time.Minute,
	}

	d.env.ExecuteActivity(bindings.ExecuteActivityParams{
		ExecuteActivityOptions: opts,
		ActivityType:           bindings.ActivityType{Name: activityName},
		Input:                  args,
		Header:                 d.getContextHeader(),
	}, func(result *commonpb.Payloads, err error) {
		d.handleFunctionCallResult(tag, result, err)
	})

	return nil
}

func (d *Definition) handleFunctionCallResult(tag uint64, result *commonpb.Payloads, err error) {
	if err != nil {
		// Convert Temporal error to apierror
		d.resumeProcess(tag, function.CallResult{Error: temporalsvc.FromTemporalError(err)}, nil)
		return
	}

	var values payload.Payloads
	if err := d.dc.FromPayloads(result, &values); err != nil {
		d.resumeProcess(tag, function.CallResult{Error: err}, nil)
		return
	}

	if len(values) > 0 {
		d.resumeProcess(tag, function.CallResult{Value: values[0]}, nil)
	} else {
		d.resumeProcess(tag, function.CallResult{}, nil)
	}
}

func (d *Definition) resumeProcess(tag uint64, data any, err error) {
	events := []process.Event{{
		Type:  process.EventYieldComplete,
		Tag:   tag,
		Data:  data,
		Error: err,
	}}
	d.output.Reset()
	stepErr := d.proc.Step(events, &d.output)

	if stepErr != nil {
		d.result = &runtime.Result{Error: stepErr}
		d.completeWithResult()
		return
	}

	switch d.output.Status() {
	case process.StepDone:
		d.result = &runtime.Result{Value: d.output.Result()}
		d.completeWithResult()
	case process.StepYield:
		d.output.ForEachYield(func(y process.Yield) {
			if err := d.executeCommand(y.Cmd, y.Tag); err != nil {
				d.env.Complete(nil, fmt.Errorf("failed to execute command: %w", err))
			}
		})
	case process.StepUpgrade:
		d.executeContinueAsNew()
	}
}

// executeProcessSend handles process.send from workflows.
// If target is an update pseudo-PID (host="update"), maps topics to UpdateCallbacks.
// If target is a Temporal workflow (same task queue), signals it.
// If target is a local process, routes via relay system using local activity.
// Self-sends complete immediately.
func (d *Definition) executeProcessSend(cmd *process.SendCmd, tag uint64) error {
	taskQueue := d.env.WorkflowInfo().TaskQueueName

	// Get workflow's own PID
	selfPID := pid.PID{
		Host:   taskQueue,
		UniqID: d.env.WorkflowInfo().WorkflowExecution.ID,
	}

	// Update response: target has host="update", UniqID is the update ID
	if cmd.To.Host == "update" {
		return d.handleUpdateResponse(cmd, tag)
	}

	// Self-send: could be query response pattern - just complete immediately
	if cmd.To.UniqID == selfPID.UniqID && cmd.To.Host == selfPID.Host {
		d.resumeProcess(tag, process.SendResult{}, nil)
		return nil
	}

	// Check if target is a Temporal workflow (same task queue or explicit "temporal" host)
	isTemporalTarget := cmd.To.Host == taskQueue || cmd.To.Host == "temporal"

	if isTemporalTarget {
		// Signal external Temporal workflow
		return d.signalExternalWorkflow(cmd, tag)
	}

	// Route to local process via relay system
	return d.routeToLocalProcess(cmd, tag, selfPID)
}

// signalExternalWorkflow sends a signal to another Temporal workflow.
func (d *Definition) signalExternalWorkflow(cmd *process.SendCmd, tag uint64) error {
	var arg *commonpb.Payloads
	if len(cmd.Payloads) > 0 {
		var err error
		arg, err = d.dc.ToPayloads(cmd.Payloads)
		if err != nil {
			d.resumeProcess(tag, process.SendResult{Error: err}, nil)
			return nil
		}
	}

	d.env.SignalExternalWorkflow(
		"",
		cmd.To.UniqID,
		"",
		cmd.Topic,
		arg,
		nil,
		nil,
		false,
		func(_ *commonpb.Payloads, err error) {
			d.resumeProcess(tag, process.SendResult{Error: temporalsvc.FromTemporalError(err)}, nil)
		},
	)

	return nil
}

// routeToLocalProcess sends a message to a local process via relay using SideEffect.
// SideEffect ensures the send is recorded and idempotent on replay.
func (d *Definition) routeToLocalProcess(cmd *process.SendCmd, tag uint64, from pid.PID) error {
	// Build relay package
	pkg := &relay.Package{
		Source: from,
		Target: cmd.To,
		Messages: []*relay.Message{{
			Topic:    cmd.Topic,
			Payloads: cmd.Payloads,
		}},
	}

	// Use SideEffect for idempotent send - recorded in history, not re-executed on replay
	d.env.SideEffect(func() (*commonpb.Payloads, error) {
		router := relay.GetRouter(d.ctx)
		if router == nil {
			return nil, fmt.Errorf("relay router not available")
		}

		if err := router.Send(pkg); err != nil {
			return nil, err
		}

		// Return success marker
		return d.dc.ToPayloads(true)
	}, func(result *commonpb.Payloads, err error) {
		if err != nil {
			d.resumeProcess(tag, process.SendResult{Error: err}, nil)
			return
		}
		d.resumeProcess(tag, process.SendResult{}, nil)
	})

	return nil
}

// handleUpdateResponse processes Lua's response to an update (ack/nak/ok/error).
// State machine: pending -> accepted (ack) or rejected (nak) -> completed (ok/error)
func (d *Definition) handleUpdateResponse(cmd *process.SendCmd, tag uint64) error {
	d.replayLog.Debug("handleUpdateResponse", zap.String("id", cmd.To.UniqID), zap.String("topic", cmd.Topic))
	updateID := cmd.To.UniqID
	upd, ok := d.activeUpdates[updateID]
	if !ok {
		d.resumeProcess(tag, process.SendResult{Error: fmt.Errorf("unknown update: %s", updateID)}, nil)
		return nil
	}

	switch cmd.Topic {
	case "ack":
		// Accept the update (validation passed)
		if upd.State != updatePending {
			d.resumeProcess(tag, process.SendResult{Error: fmt.Errorf("update already %s", stateString(upd.State))}, nil)
			return nil
		}
		upd.State = updateAccepted
		upd.Callbacks.Accept()
		d.resumeProcess(tag, process.SendResult{}, nil)

	case "nak":
		// Reject the update (validation failed)
		if upd.State != updatePending {
			d.resumeProcess(tag, process.SendResult{Error: fmt.Errorf("update already %s", stateString(upd.State))}, nil)
			return nil
		}
		upd.State = updateRejected
		errMsg := extractErrorMessage(cmd.Payloads, "update rejected")
		upd.Callbacks.Reject(fmt.Errorf("%s", errMsg))
		delete(d.activeUpdates, updateID)
		d.resumeProcess(tag, process.SendResult{}, nil)

	case "ok":
		// Complete with success
		if upd.State != updateAccepted {
			d.resumeProcess(tag, process.SendResult{Error: fmt.Errorf("update not accepted (state: %s)", stateString(upd.State))}, nil)
			return nil
		}
		upd.State = updateComplete
		// Convert payload to Go-native type for Temporal
		var result any
		if len(cmd.Payloads) > 0 {
			p := cmd.Payloads[0]
			if p.Format() == payload.Lua {
				// Transcode Lua to JSON using context transcoder
				transcoder := payload.GetTranscoder(d.execCtx)
				if transcoder == nil {
					d.resumeProcess(tag, process.SendResult{Error: fmt.Errorf("no transcoder available")}, nil)
					return nil
				}
				jsonPayload, err := transcoder.Transcode(p, payload.JSON)
				if err != nil {
					d.resumeProcess(tag, process.SendResult{Error: fmt.Errorf("failed to convert to JSON: %w", err)}, nil)
					return nil
				}
				jsonBytes := jsonPayload.Data().([]byte)
				if err := json.Unmarshal(jsonBytes, &result); err != nil {
					d.resumeProcess(tag, process.SendResult{Error: fmt.Errorf("failed to unmarshal JSON: %w", err)}, nil)
					return nil
				}
			} else {
				result = p.Data()
			}
		}
		upd.Callbacks.Complete(result, nil)
		delete(d.activeUpdates, updateID)
		d.resumeProcess(tag, process.SendResult{}, nil)

	case "error":
		// Complete with error
		if upd.State != updateAccepted {
			d.resumeProcess(tag, process.SendResult{Error: fmt.Errorf("update not accepted (state: %s)", stateString(upd.State))}, nil)
			return nil
		}
		upd.State = updateComplete
		errMsg := extractErrorMessage(cmd.Payloads, "update failed")
		upd.Callbacks.Complete(nil, fmt.Errorf("%s", errMsg))
		delete(d.activeUpdates, updateID)
		d.resumeProcess(tag, process.SendResult{}, nil)

	default:
		d.resumeProcess(tag, process.SendResult{Error: fmt.Errorf("unknown update response: %s (expected ack/nak/ok/error)", cmd.Topic)}, nil)
	}

	return nil
}

func stateString(s updateState) string {
	switch s {
	case updatePending:
		return "pending"
	case updateAccepted:
		return "accepted"
	case updateRejected:
		return "rejected"
	case updateComplete:
		return "completed"
	default:
		return "unknown"
	}
}

func extractErrorMessage(payloads payload.Payloads, defaultMsg string) string {
	if len(payloads) > 0 {
		if s, ok := payloads[0].Data().(string); ok {
			return s
		}
		return fmt.Sprintf("%v", payloads[0].Data())
	}
	return defaultMsg
}

// getContextHeader creates a header from current FrameContext values for propagation.
func (d *Definition) getContextHeader() *commonpb.Header {
	var header *commonpb.Header

	values := ctxapi.GetValues(d.execCtx)
	if values != nil && values.Len() > 0 {
		data := make(map[string]any)
		values.Iterate(func(key string, val any) {
			switch val.(type) {
			case string, int, int64, float64, bool, map[string]any, []any:
				data[key] = val
			}
		})

		if len(data) > 0 {
			var err error
			header, err = propagator.CreateHeader(data)
			if err != nil {
				d.replayLog.Warn("failed to create context header", zap.Error(err))
			}
		}
	}

	// Add security context to header
	secPayload := propagator.ExtractSecurityPayload(d.execCtx)
	if secPayload != nil {
		var err error
		header, err = propagator.AddSecurityToHeader(header, secPayload)
		if err != nil {
			d.replayLog.Warn("failed to add security to header", zap.Error(err))
		}
	}

	return header
}

// executeProcessSpawn handles process.spawn from workflows by starting a child workflow.
func (d *Definition) executeProcessSpawn(cmd *process.SpawnCmd, tag uint64) error {
	if cmd.Start == nil {
		d.resumeProcess(tag, process.SpawnResult{Error: fmt.Errorf("spawn command missing start config")}, nil)
		return nil
	}

	workflowName := cmd.Start.Source.String()

	args, err := d.dc.ToPayloads(cmd.Start.Input)
	if err != nil {
		d.resumeProcess(tag, process.SpawnResult{Error: fmt.Errorf("failed to convert arguments: %w", err)}, nil)
		return nil
	}

	params := bindings.ExecuteWorkflowParams{
		WorkflowType: &bindings.WorkflowType{Name: workflowName},
		Input:        args,
		Header:       d.getContextHeader(),
		WorkflowOptions: bindings.WorkflowOptions{
			TaskQueueName: d.env.WorkflowInfo().TaskQueueName,
		},
	}

	// Use host as task queue if specified
	if cmd.Start.HostID != "" {
		params.WorkflowOptions.TaskQueueName = cmd.Start.HostID
	}

	var childPID pid.PID
	d.env.ExecuteChildWorkflow(params, func(result *commonpb.Payloads, err error) {
		// Child completed - queue EXIT event for delivery
		if childPID.UniqID == "" {
			return // Child never started successfully
		}

		var resultPayload payload.Payload
		if result != nil && err == nil {
			var values payload.Payloads
			if decodeErr := d.dc.FromPayloads(result, &values); decodeErr == nil && len(values) > 0 {
				resultPayload = values[0]
			}
		}

		// Convert Temporal error to apierror for consistent error handling
		var convertedErr error
		if err != nil {
			convertedErr = temporalsvc.FromTemporalError(err)
		}

		d.childExits = append(d.childExits, childExitEvent{
			ChildPID: childPID,
			Result:   resultPayload,
			Error:    convertedErr,
		})
	}, func(execution bindings.WorkflowExecution, err error) {
		// Child started
		if err != nil {
			d.resumeProcess(tag, process.SpawnResult{Error: temporalsvc.FromTemporalError(err)}, nil)
			return
		}
		childPID = pid.PID{
			Host:   params.WorkflowOptions.TaskQueueName,
			UniqID: execution.ID,
		}
		d.resumeProcess(tag, process.SpawnResult{PID: childPID}, nil)
	})

	return nil
}

// executeProcessTerminate handles process.terminate from workflows by canceling an external workflow.
func (d *Definition) executeProcessTerminate(cmd *process.TerminateCmd, tag uint64) error {
	d.env.RequestCancelExternalWorkflow(
		"",
		cmd.Target.UniqID,
		"",
		func(_ *commonpb.Payloads, err error) {
			if err != nil {
				d.resumeProcess(tag, nil, temporalsvc.FromTemporalError(err))
			} else {
				d.resumeProcess(tag, nil, nil)
			}
		},
	)
	return nil
}

// executeProcessCancel handles process.cancel from workflows by canceling an external workflow.
func (d *Definition) executeProcessCancel(cmd *process.CancelCmd, tag uint64) error {
	d.env.RequestCancelExternalWorkflow(
		"",
		cmd.Target.UniqID,
		"",
		func(_ *commonpb.Payloads, err error) {
			if err != nil {
				d.resumeProcess(tag, nil, temporalsvc.FromTemporalError(err))
			} else {
				d.resumeProcess(tag, nil, nil)
			}
		},
	)
	return nil
}

// executeProcessMonitor is not supported in workflows.
// Child workflows are automatically monitored - EXIT events are delivered when they complete.
// External workflow monitoring is not supported by Temporal.
func (d *Definition) executeProcessMonitor(_ *process.MonitorCmd, tag uint64) error {
	err := apierror.New(apierror.Invalid, "process.monitor not supported in workflow context: child workflows are automatically monitored").
		WithRetryable(apierror.False)
	d.resumeProcess(tag, nil, err)
	return nil
}

// executeProcessUnmonitor is not supported in workflows.
func (d *Definition) executeProcessUnmonitor(_ *process.UnmonitorCmd, tag uint64) error {
	err := apierror.New(apierror.Invalid, "process.unmonitor not supported in workflow context").
		WithRetryable(apierror.False)
	d.resumeProcess(tag, nil, err)
	return nil
}

// executeProcessLink is not supported in workflows - Temporal doesn't support bidirectional linking.
func (d *Definition) executeProcessLink(_ *process.LinkCmd, tag uint64) error {
	err := apierror.New(apierror.Invalid, "process.link not supported in workflow context: Temporal doesn't support bidirectional linking").
		WithRetryable(apierror.False)
	d.resumeProcess(tag, nil, err)
	return nil
}

// executeProcessUnlink is not supported in workflows.
func (d *Definition) executeProcessUnlink(_ *process.UnlinkCmd, tag uint64) error {
	err := apierror.New(apierror.Invalid, "process.unlink not supported in workflow context").
		WithRetryable(apierror.False)
	d.resumeProcess(tag, nil, err)
	return nil
}

// executeProcessCall handles process.call from workflows by executing a child workflow synchronously.
func (d *Definition) executeProcessCall(cmd *process.CallCmd, tag uint64) error {
	workflowName := cmd.Source.String()

	var args *commonpb.Payloads
	if len(cmd.Input) > 0 {
		var err error
		args, err = d.dc.ToPayloads(cmd.Input)
		if err != nil {
			d.resumeProcess(tag, process.CallResult{Result: &runtime.Result{Error: fmt.Errorf("failed to convert arguments: %w", err)}}, nil)
			return nil
		}
	}

	params := bindings.ExecuteWorkflowParams{
		WorkflowType: &bindings.WorkflowType{Name: workflowName},
		Input:        args,
		Header:       d.getContextHeader(),
		WorkflowOptions: bindings.WorkflowOptions{
			TaskQueueName: d.env.WorkflowInfo().TaskQueueName,
		},
	}

	// Use HostID as task queue if specified
	if cmd.HostID != "" {
		params.WorkflowOptions.TaskQueueName = cmd.HostID
	}

	d.env.ExecuteChildWorkflow(params, func(result *commonpb.Payloads, err error) {
		if err != nil {
			d.resumeProcess(tag, process.CallResult{Result: &runtime.Result{Error: temporalsvc.FromTemporalError(err)}}, nil)
			return
		}
		var values payload.Payloads
		if err := d.dc.FromPayloads(result, &values); err != nil {
			d.resumeProcess(tag, process.CallResult{Result: &runtime.Result{Error: err}}, nil)
			return
		}
		if len(values) > 0 {
			d.resumeProcess(tag, process.CallResult{Result: &runtime.Result{Value: values[0]}}, nil)
		} else {
			d.resumeProcess(tag, process.CallResult{Result: &runtime.Result{}}, nil)
		}
	}, func(_ bindings.WorkflowExecution, _ error) {
		// Child workflow started callback - not needed for sync call
	})

	return nil
}

// executeSideEffect executes a side effect function deterministically.
// The result is recorded and replayed on workflow replay.
func (d *Definition) executeSideEffect(cmd *workflowapi.SideEffectCmd, tag uint64) error {
	d.env.SideEffect(func() (*commonpb.Payloads, error) {
		if cmd.Fn == nil {
			return nil, fmt.Errorf("side effect function is nil")
		}
		value, err := cmd.Fn()
		if err != nil {
			return nil, err
		}
		// Wrap []byte as Bytes payload for proper binary encoding
		if binData, ok := value.([]byte); ok {
			return d.dc.ToPayloads(payload.NewPayload(binData, payload.Bytes))
		}
		return d.dc.ToPayloads(value)
	}, func(result *commonpb.Payloads, err error) {
		if err != nil {
			d.resumeProcess(tag, workflowapi.Result{Error: err}, nil)
			return
		}
		// Try to decode as payload.Payload first (for binary data)
		var p payload.Payload
		if err := d.dc.FromPayloads(result, &p); err == nil {
			if p.Format() == payload.Bytes {
				if data, ok := p.Data().([]byte); ok {
					d.resumeProcess(tag, workflowapi.Result{Value: string(data)}, nil)
					return
				}
			}
			d.resumeProcess(tag, workflowapi.Result{Value: p.Data()}, nil)
			return
		}
		// Fall back to generic decoding
		var value any
		if err := d.dc.FromPayloads(result, &value); err != nil {
			d.resumeProcess(tag, workflowapi.Result{Error: err}, nil)
			return
		}
		d.resumeProcess(tag, workflowapi.Result{Value: value}, nil)
	})
	return nil
}

// GetWorkflowInfo implements workflowapi.InfoProvider.
func (d *Definition) GetWorkflowInfo() workflowapi.Info {
	info := d.env.WorkflowInfo()
	return workflowapi.Info{
		WorkflowID:    info.WorkflowExecution.ID,
		RunID:         info.WorkflowExecution.RunID,
		WorkflowType:  info.WorkflowType.Name,
		TaskQueue:     info.TaskQueueName,
		Namespace:     info.Namespace,
		Attempt:       int(info.Attempt),
		HistoryLength: info.GetCurrentHistoryLength(),
		HistorySize:   info.GetCurrentHistorySize(),
	}
}

// executeWorkflowCall executes a child workflow synchronously and returns the result.
func (d *Definition) executeWorkflowCall(cmd *workflowapi.CallCmd, tag uint64) error {
	workflowName := cmd.ID.String()

	var args *commonpb.Payloads
	if len(cmd.Args) > 0 {
		var err error
		args, err = d.dc.ToPayloads(cmd.Args)
		if err != nil {
			d.resumeProcess(tag, workflowapi.CallResult{Error: fmt.Errorf("failed to convert arguments: %w", err)}, nil)
			return nil
		}
	}

	params := bindings.ExecuteWorkflowParams{
		WorkflowType: &bindings.WorkflowType{Name: workflowName},
		Input:        args,
		Header:       d.getContextHeader(),
		WorkflowOptions: bindings.WorkflowOptions{
			TaskQueueName: d.env.WorkflowInfo().TaskQueueName,
		},
	}

	if cmd.Options != nil {
		if cmd.Options.WorkflowID != "" {
			params.WorkflowOptions.WorkflowID = cmd.Options.WorkflowID
		}
		if cmd.Options.TaskQueue != "" {
			params.WorkflowOptions.TaskQueueName = cmd.Options.TaskQueue
		}
		if cmd.Options.ExecutionTimeout != "" {
			dur, err := time.ParseDuration(cmd.Options.ExecutionTimeout)
			if err == nil {
				params.WorkflowOptions.WorkflowExecutionTimeout = dur
			}
		}
		if cmd.Options.RunTimeout != "" {
			dur, err := time.ParseDuration(cmd.Options.RunTimeout)
			if err == nil {
				params.WorkflowOptions.WorkflowRunTimeout = dur
			}
		}
		if cmd.Options.TaskTimeout != "" {
			dur, err := time.ParseDuration(cmd.Options.TaskTimeout)
			if err == nil {
				params.WorkflowOptions.WorkflowTaskTimeout = dur
			}
		}
	}

	d.env.ExecuteChildWorkflow(params, func(result *commonpb.Payloads, err error) {
		if err != nil {
			d.resumeProcess(tag, workflowapi.CallResult{Error: temporalsvc.FromTemporalError(err)}, nil)
			return
		}
		var values payload.Payloads
		if err := d.dc.FromPayloads(result, &values); err != nil {
			d.resumeProcess(tag, workflowapi.CallResult{Error: err}, nil)
			return
		}
		if len(values) > 0 {
			d.resumeProcess(tag, workflowapi.CallResult{Value: values[0]}, nil)
		} else {
			d.resumeProcess(tag, workflowapi.CallResult{}, nil)
		}
	}, func(_ bindings.WorkflowExecution, _ error) {
		// Child workflow started callback - not needed for sync call
	})

	return nil
}

// executeVersion returns a deterministic version number for workflow code changes.
func (d *Definition) executeVersion(cmd *workflowapi.VersionCmd, tag uint64) error {
	version := d.env.GetVersion(cmd.ChangeID, workflow.Version(cmd.MinSupported), workflow.Version(cmd.MaxSupported))
	d.resumeProcess(tag, workflowapi.VersionResult{Version: int(version)}, nil)
	return nil
}

// executeUpsertAttrs updates workflow search attributes and/or memo.
func (d *Definition) executeUpsertAttrs(cmd *workflowapi.UpsertAttrsCmd, tag uint64) error {
	// Upsert search attributes if provided
	if len(cmd.SearchAttrs) > 0 {
		if err := d.env.UpsertSearchAttributes(cmd.SearchAttrs); err != nil {
			d.resumeProcess(tag, nil, temporalsvc.FromTemporalError(err))
			return nil
		}
	}

	// Upsert memo if provided
	if len(cmd.Memo) > 0 {
		if err := d.env.UpsertMemo(cmd.Memo); err != nil {
			d.resumeProcess(tag, nil, temporalsvc.FromTemporalError(err))
			return nil
		}
	}

	d.resumeProcess(tag, true, nil)
	return nil
}

// StackTrace implements WorkflowDefinition.StackTrace.
func (d *Definition) StackTrace() string {
	return fmt.Sprintf("Workflow: %s", d.id.String())
}

// Close implements WorkflowDefinition.Close.
func (d *Definition) Close() {
	if d.proc != nil {
		d.proc.Close()
	}
}

// parseParentClosePolicy converts string to Temporal ParentClosePolicy enum.
func parseParentClosePolicy(policy string) enumspb.ParentClosePolicy {
	switch policy {
	case "terminate":
		return enumspb.PARENT_CLOSE_POLICY_TERMINATE
	case "abandon":
		return enumspb.PARENT_CLOSE_POLICY_ABANDON
	case "request_cancel":
		return enumspb.PARENT_CLOSE_POLICY_REQUEST_CANCEL
	default:
		return enumspb.PARENT_CLOSE_POLICY_UNSPECIFIED
	}
}
