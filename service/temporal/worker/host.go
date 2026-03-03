// SPDX-License-Identifier: MPL-2.0

package worker

import (
	"context"
	"errors"
	"fmt"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	temporalprop "github.com/wippyai/runtime/service/temporal/propagator"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

// Run starts a Temporal workflow as a process host.
func (w *Worker) Run(ctx context.Context, start *process.Start) (pid.PID, error) {
	if start == nil {
		return pid.PID{}, fmt.Errorf("start config is required")
	}
	if w.closed.Load() {
		return pid.PID{}, fmt.Errorf("worker is closed")
	}
	runtimeState := w.loadRuntime()
	if runtimeState == nil || runtimeState.temporalClient == nil {
		return pid.PID{}, fmt.Errorf("temporal client not available")
	}

	taskQueue := runtimeState.taskQueue
	if taskQueue == "" {
		taskQueue = w.config.TaskQueue
	}

	workflowName := start.Source.String()
	hostID := w.id.String()

	execCtx := ctx
	var fc ctxapi.FrameContext
	if len(start.Context) > 0 {
		execCtx, fc = ctxapi.OpenFrameContextOn(ctx, ctx)
		if err := fc.SetMultiple(start.Context...); err != nil {
			ctxapi.ReleaseFrameContext(fc)
			return pid.PID{}, fmt.Errorf("failed to set workflow context: %w", err)
		}
		defer ctxapi.ReleaseFrameContext(fc)
	}
	execCtx = withContextValuesFallback(execCtx)

	opts := client.StartWorkflowOptions{
		TaskQueue: taskQueue,
	}
	optState, err := applyTemporalStartWorkflowOptions(&opts, start)
	if err != nil {
		return pid.PID{}, err
	}

	workflowID := processName(start)
	if opts.ID != "" {
		workflowID = opts.ID
	}
	manualWorkflowID := workflowID != ""

	if !optState.hasErrorOnStarted {
		// Named/explicit IDs default to singleton semantics (use existing).
		// Generated IDs default to strict fresh-run semantics (fail on duplicate).
		opts.WorkflowExecutionErrorWhenAlreadyStarted = !manualWorkflowID
	}
	if !optState.hasConflictPolicy {
		opts.WorkflowIDConflictPolicy = resolveConflictPolicy(opts.WorkflowExecutionErrorWhenAlreadyStarted)
	}

	var workflowPID pid.PID
	if manualWorkflowID {
		workflowPID = (&pid.PID{
			Node:   w.clientNodeID,
			Host:   hostID,
			UniqID: workflowID,
		}).Precomputed()
	} else {
		basePID := w.pidGen.Generate(hostID)
		workflowID = basePID.UniqID
		if w.workflowPrefix != "" {
			workflowID = w.workflowPrefix + "_" + workflowID
		}
		workflowPID = (&pid.PID{
			Node:   w.clientNodeID,
			Host:   hostID,
			UniqID: workflowID,
		}).Precomputed()
	}

	opts.ID = workflowID

	firstSignalIdx, firstSignal := firstSignalMessage(start.Messages)
	if firstSignal != nil {
		run, err := w.signalWithStartMessage(runtimeState, execCtx, workflowID, workflowName, opts, start, firstSignal)
		if err != nil {
			var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
			if errors.As(err, &alreadyStarted) && shouldUseExistingOnAlreadyStarted(opts) {
				w.publishRunHandoff(start, runtimeState.ctx, workflowID, alreadyStarted.RunId)
				if err := w.signalMessages(execCtx, workflowID, start.Messages, start); err != nil {
					return pid.PID{}, err
				}
				return workflowPID, nil
			}
			return pid.PID{}, err
		}
		if run != nil {
			w.publishRunHandoff(start, runtimeState.ctx, workflowID, run.GetRunID())
		}
		if err := w.signalMessages(execCtx, workflowID, start.Messages[firstSignalIdx+1:], start); err != nil {
			return pid.PID{}, err
		}
		return workflowPID, nil
	}

	run, err := runtimeState.temporalClient.ExecuteWorkflow(execCtx, opts, workflowName, start.Input)
	if err != nil {
		var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &alreadyStarted) && shouldUseExistingOnAlreadyStarted(opts) {
			w.publishRunHandoff(start, runtimeState.ctx, workflowID, alreadyStarted.RunId)
			return workflowPID, nil
		}
		return pid.PID{}, err
	}
	if run != nil {
		w.publishRunHandoff(start, runtimeState.ctx, workflowID, run.GetRunID())
	}

	return workflowPID, nil
}

func resolveConflictPolicy(errorWhenAlreadyStarted bool) enumspb.WorkflowIdConflictPolicy {
	if errorWhenAlreadyStarted {
		return enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL
	}
	return enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING
}

func shouldUseExistingOnAlreadyStarted(opts client.StartWorkflowOptions) bool {
	switch opts.WorkflowIDConflictPolicy {
	case enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING:
		return true
	case enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL:
		return false
	default:
		return !opts.WorkflowExecutionErrorWhenAlreadyStarted
	}
}

// Terminate stops a running Temporal workflow.
func (w *Worker) Terminate(ctx context.Context, target pid.PID) error {
	runtimeState := w.loadRuntime()
	if runtimeState == nil || runtimeState.temporalClient == nil {
		return fmt.Errorf("temporal client not available")
	}
	if target.UniqID == "" {
		return fmt.Errorf("target workflow ID is empty")
	}
	return runtimeState.temporalClient.TerminateWorkflow(ctx, target.UniqID, "", "process.terminate")
}

func (w *Worker) signalMessages(ctx context.Context, workflowID string, messages []*relay.Message, start *process.Start) error {
	runtimeState := w.loadRuntime()
	if runtimeState == nil || runtimeState.temporalClient == nil {
		return fmt.Errorf("temporal client not available")
	}

	if len(messages) == 0 {
		return nil
	}
	sender := resolveSignalSender(start)
	for _, msg := range messages {
		if msg == nil || msg.Topic == "" {
			continue
		}
		signalCtx, fc := withSignalSender(ctx, sender)
		err := runtimeState.temporalClient.SignalWorkflow(signalCtx, workflowID, "", msg.Topic, signalArg(msg))
		releaseSignalFrame(fc)
		if err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) signalWithStartMessage(
	runtimeState *workerRuntime,
	ctx context.Context,
	workflowID string,
	workflowName string,
	opts client.StartWorkflowOptions,
	start *process.Start,
	msg *relay.Message,
) (client.WorkflowRun, error) {
	sender := resolveSignalSender(start)
	signalCtx, fc := withSignalSender(ctx, sender)
	defer releaseSignalFrame(fc)

	return runtimeState.temporalClient.SignalWithStartWorkflow(
		signalCtx,
		workflowID,
		msg.Topic,
		signalArg(msg),
		opts,
		workflowName,
		start.Input,
	)
}

func firstSignalMessage(messages []*relay.Message) (int, *relay.Message) {
	for i, msg := range messages {
		if msg == nil || msg.Topic == "" {
			continue
		}
		return i, msg
	}
	return -1, nil
}

func processName(start *process.Start) string {
	if start == nil || start.Options == nil {
		return ""
	}
	return start.Options.GetString(process.ProcessNameKey, "")
}

func resolveSignalSender(start *process.Start) pid.PID {
	if start == nil || start.Options == nil {
		return pid.PID{}
	}
	v, ok := start.Options.Get(process.ProcessParentKey)
	if !ok {
		return pid.PID{}
	}
	p, ok := v.(pid.PID)
	if !ok {
		return pid.PID{}
	}
	return p
}

func withSignalSender(ctx context.Context, sender pid.PID) (context.Context, ctxapi.FrameContext) {
	signalCtx := ctx
	var fc ctxapi.FrameContext

	if sender.Node != "" || sender.Host != "" || sender.UniqID != "" {
		signalCtx, fc = ctxapi.OpenFrameContextOn(ctx, ctx)
		values, err := ctxapi.GetOrCreateValues(signalCtx)
		if err == nil {
			values.Set(temporalprop.SignalFromValueKey, sender.String())
		}
	}

	return withContextValuesFallback(signalCtx), fc
}

func releaseSignalFrame(fc ctxapi.FrameContext) {
	if fc != nil {
		ctxapi.ReleaseFrameContext(fc)
	}
}

func signalArg(msg *relay.Message) any {
	switch len(msg.Payloads) {
	case 0:
		return nil
	case 1:
		return msg.Payloads[0]
	default:
		return msg.Payloads
	}
}

// withContextValuesFallback mirrors FrameContext values into plain context values.
// Some Temporal client paths only consume one of those propagation channels.
func withContextValuesFallback(ctx context.Context) context.Context {
	existing := temporalprop.GetContextValues(ctx)
	values := ctxapi.GetValues(ctx)

	total := len(existing)
	if values != nil {
		total += values.Len()
	}
	if total == 0 {
		return ctx
	}

	merged := make(map[string]any, total)
	for k, v := range existing {
		merged[k] = v
	}
	if values != nil {
		values.Iterate(func(key string, val any) {
			merged[key] = val
		})
	}

	return temporalprop.WithValues(ctx, merged)
}

func shouldPublishRunHandoff(start *process.Start) bool {
	if start == nil || start.Options == nil {
		return false
	}
	if start.Options.GetBool(process.ProcessMonitorKey, false) {
		return true
	}
	if start.Options.GetBool(process.ProcessLinkKey, false) {
		return true
	}
	return false
}

func (w *Worker) publishRunHandoff(start *process.Start, runtimeCtx context.Context, workflowID, runID string) {
	if !shouldPublishRunHandoff(start) || workflowID == "" || runID == "" {
		return
	}
	if runtimeCtx == nil || w.config == nil {
		return
	}

	registry := temporalapi.GetWorkflowRunHandoff(runtimeCtx)
	if registry == nil {
		return
	}

	registry.Publish(w.config.Client.String(), workflowID, runID)
}
