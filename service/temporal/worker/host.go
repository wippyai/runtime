package worker

import (
	"context"
	"fmt"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	temporalprop "github.com/wippyai/runtime/service/temporal/propagator"
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
	if w.temporalClient == nil {
		return pid.PID{}, fmt.Errorf("temporal client not available")
	}

	taskQueue := w.taskQueue
	if taskQueue == "" {
		taskQueue = w.config.TaskQueue
	}

	workflowName := start.Source.String()
	hostID := pid.HostID(w.id.String())

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

	workflowID := start.Name
	var workflowPID pid.PID
	if workflowID == "" {
		workflowPID = w.pidGen.Generate(hostID)
		workflowID = workflowPID.UniqID
	} else {
		workflowPID = (&pid.PID{
			Node:   w.clientNodeID,
			Host:   hostID,
			UniqID: workflowID,
		}).Precomputed()
	}

	opts := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: taskQueue,
	}

	_, err := w.temporalClient.ExecuteWorkflow(execCtx, opts, workflowName, start.Input)
	if err != nil {
		if start.Name != "" {
			if _, ok := err.(*serviceerror.WorkflowExecutionAlreadyStarted); ok {
				if err := w.signalMessages(execCtx, workflowID, start.Messages, start); err != nil {
					return pid.PID{}, err
				}
				return workflowPID, nil
			}
		}
		return pid.PID{}, err
	}

	if len(start.Messages) > 0 {
		if err := w.signalMessages(execCtx, workflowID, start.Messages, start); err != nil {
			return workflowPID, err
		}
	}

	return workflowPID, nil
}

// Terminate stops a running Temporal workflow.
func (w *Worker) Terminate(ctx context.Context, target pid.PID) error {
	if w.temporalClient == nil {
		return fmt.Errorf("temporal client not available")
	}
	if target.UniqID == "" {
		return fmt.Errorf("target workflow ID is empty")
	}
	return w.temporalClient.TerminateWorkflow(ctx, target.UniqID, "", "process.terminate")
}

func (w *Worker) signalMessages(ctx context.Context, workflowID string, messages []*relay.Message, start *process.Start) error {
	if len(messages) == 0 {
		return nil
	}
	var sender pid.PID
	if start != nil && start.Options != nil {
		if v, ok := start.Options.Get(process.LifecycleParentKey); ok {
			if p, ok := v.(pid.PID); ok {
				sender = p
			}
		}
	}
	for _, msg := range messages {
		if msg == nil || msg.Topic == "" {
			continue
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
		signalCtx := ctx
		var fc ctxapi.FrameContext
		if sender.Node != "" || sender.Host != "" || sender.UniqID != "" {
			signalCtx, fc = ctxapi.OpenFrameContextOn(ctx, ctx)
			values, err := ctxapi.GetOrCreateValues(signalCtx)
			if err == nil {
				values.Set(temporalprop.SignalFromValueKey, sender.String())
			}
		}
		signalCtx = withContextValuesFallback(signalCtx)
		err := w.temporalClient.SignalWorkflow(signalCtx, workflowID, "", msg.Topic, signalArg)
		if fc != nil {
			ctxapi.ReleaseFrameContext(fc)
		}
		if err != nil {
			return err
		}
	}
	return nil
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
