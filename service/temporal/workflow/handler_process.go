// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"fmt"

	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	temporalerrors "github.com/wippyai/runtime/service/temporal/errors"
	"github.com/wippyai/runtime/service/temporal/propagator"
	commonpb "go.temporal.io/api/common/v1"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.uber.org/zap"
)

// PID host identifiers for routing
const (
	pidHostUpdate   = "update"
	pidHostTemporal = "temporal"
)

// selfPID returns the PID for the current workflow, either from frame context or constructed from env.
func (d *Definition) selfPID() pid.PID {
	if p, ok := runtime.GetFramePID(d.execCtx); ok {
		return p
	}
	return pid.PID{
		Node:   d.resolveClientID(),
		Host:   d.resolveWorkerID(d.env.WorkflowInfo().TaskQueueName),
		UniqID: d.env.WorkflowInfo().WorkflowExecution.ID,
	}
}

// executeProcessSend handles process.send from workflows.
func (d *Definition) executeProcessSend(cmd *process.SendCmd, tag uint64) error {
	selfPID := d.selfPID()

	// Update response: target has host="update"
	if cmd.To.Host == pidHostUpdate {
		return d.handleUpdateResponse(cmd, tag)
	}

	// Self-send
	if cmd.To.UniqID == selfPID.UniqID && cmd.To.Host == selfPID.Host {
		d.resumeProcess(tag, process.SendResult{}, nil)
		return nil
	}

	// Temporal workflow target
	clientID := d.resolveClientID()
	isTemporalTarget := (cmd.To.Node != "" && cmd.To.Node == clientID) ||
		cmd.To.Host == selfPID.Host ||
		cmd.To.Host == pidHostTemporal
	if isTemporalTarget {
		return d.signalExternalWorkflow(cmd, tag)
	}

	// Route to local process
	return d.routeToLocalProcess(cmd, tag, selfPID)
}

// signalExternalWorkflow sends a signal to another Temporal workflow.
func (d *Definition) signalExternalWorkflow(cmd *process.SendCmd, tag uint64) error {
	var arg *commonpb.Payloads
	if len(cmd.Payloads) > 0 {
		var err error
		arg, err = d.dc.ToPayloads(cmd.Payloads)
		if err != nil {
			d.resumeProcess(tag, process.SendResult{Error: temporalerrors.FromTemporalError(err)}, nil)
			return nil
		}
	}

	from := d.selfPID()
	header := d.getContextHeaderWithValues(map[string]any{
		propagator.SignalFromValueKey: from.String(),
	})

	d.env.SignalExternalWorkflow(
		"",
		cmd.To.UniqID,
		"",
		cmd.Topic,
		arg,
		nil,
		header,
		false,
		func(_ *commonpb.Payloads, err error) {
			d.enqueueYieldCompletion(tag, process.SendResult{Error: temporalerrors.FromTemporalError(err)}, nil)
		},
	)

	return nil
}

// routeToLocalProcess sends a message to a local process via relay using SideEffect.
func (d *Definition) routeToLocalProcess(cmd *process.SendCmd, tag uint64, from pid.PID) error {
	pkg := &relay.Package{
		Source: from,
		Target: cmd.To,
		Messages: []*relay.Message{{
			Topic:    cmd.Topic,
			Payloads: cmd.Payloads,
		}},
	}

	d.env.SideEffect(func() (*commonpb.Payloads, error) {
		router := relay.GetRouter(d.ctx)
		if router == nil {
			return nil, fmt.Errorf("relay router not available")
		}

		if err := router.Send(pkg); err != nil {
			return nil, err
		}

		return d.dc.ToPayloads(true)
	}, func(_ *commonpb.Payloads, err error) {
		if err != nil {
			d.resumeProcess(tag, process.SendResult{Error: temporalerrors.FromTemporalError(err)}, nil)
			return
		}
		d.resumeProcess(tag, process.SendResult{}, nil)
	}, "process.send")

	return nil
}

// handleUpdateResponse processes workflow response to an update (ack/nak/ok/error).
func (d *Definition) handleUpdateResponse(cmd *process.SendCmd, tag uint64) error {
	d.updates.HandleResponse(cmd.To.UniqID, cmd.Topic, cmd.Payloads, func(_ any, err error) {
		if err != nil {
			d.resumeProcess(tag, process.SendResult{Error: err}, nil)
		} else {
			d.resumeProcess(tag, process.SendResult{}, nil)
		}
	})
	return nil
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

	if err := applyTemporalChildWorkflowOptions(&params, cmd.Start.Options); err != nil {
		d.resumeProcess(tag, process.SpawnResult{
			Error: fmt.Errorf("invalid temporal child workflow options for %s: %w", workflowName, err),
		}, nil)
		return nil
	}

	if cmd.Start.HostID != "" {
		params.TaskQueueName = cmd.Start.HostID
	}
	if len(cmd.Start.Context) > 0 {
		spawnCtx, fc := ctxapi.ForkFrameContext(d.execCtx)
		if err := fc.SetMultiple(cmd.Start.Context...); err != nil {
			ctxapi.ReleaseFrameContext(fc)
			d.resumeProcess(tag, process.SpawnResult{Error: fmt.Errorf("failed to apply spawn context: %w", err)}, nil)
			return nil
		}
		params.Header = d.getContextHeaderFrom(spawnCtx, nil)
		ctxapi.ReleaseFrameContext(fc)
	}

	selfPID := d.selfPID()

	var childPID pid.PID
	d.env.ExecuteChildWorkflow(params, func(result *commonpb.Payloads, err error) {
		if childPID.UniqID == "" {
			return
		}

		var resultPayload payload.Payload
		if result != nil && err == nil {
			var values payload.Payloads
			if decodeErr := d.dc.FromPayloads(result, &values); decodeErr == nil && len(values) > 0 {
				resultPayload = values[0]
				d.replayLog.Debug("decoded child workflow result",
					zap.String("child_pid", childPID.String()),
					zap.String("format", resultPayload.Format()))
			}
		}

		var convertedErr error
		if err != nil {
			convertedErr = temporalerrors.FromTemporalError(err)
		}

		if len(d.childExits) >= maxChildExitQueueSize {
			d.replayLog.Warn("child exit queue full, dropping event",
				zap.String("child_pid", childPID.String()))
			return
		}
		d.childExits = append(d.childExits, childExitEvent{
			ChildPID: childPID,
			Result:   resultPayload,
			Error:    convertedErr,
		})
	}, func(execution bindings.WorkflowExecution, err error) {
		if err != nil {
			d.enqueueYieldCompletion(tag, process.SpawnResult{Error: temporalerrors.FromTemporalError(err)}, nil)
			return
		}

		childHost := cmd.Start.HostID
		if childHost == "" {
			childHost = selfPID.Host
		}

		childPID = pid.PID{
			Node:   d.resolveClientID(),
			Host:   childHost,
			UniqID: execution.ID,
		}
		d.enqueueYieldCompletion(tag, process.SpawnResult{PID: childPID}, nil)
	})

	return nil
}

// executeProcessTerminate handles process.terminate from workflows.
func (d *Definition) executeProcessTerminate(cmd *process.TerminateCmd, tag uint64) error {
	d.env.RequestCancelExternalWorkflow(
		"",
		cmd.Target.UniqID,
		"",
		func(_ *commonpb.Payloads, err error) {
			if err != nil {
				d.enqueueYieldCompletion(tag, nil, temporalerrors.FromTemporalError(err))
			} else {
				d.enqueueYieldCompletion(tag, nil, nil)
			}
		},
	)
	return nil
}

// executeProcessCancel handles process.cancel from workflows.
func (d *Definition) executeProcessCancel(cmd *process.CancelCmd, tag uint64) error {
	d.env.RequestCancelExternalWorkflow(
		"",
		cmd.Target.UniqID,
		"",
		func(_ *commonpb.Payloads, err error) {
			if err != nil {
				d.enqueueYieldCompletion(tag, nil, temporalerrors.FromTemporalError(err))
			} else {
				d.enqueueYieldCompletion(tag, nil, nil)
			}
		},
	)
	return nil
}

// rejectUnsupportedCommand resumes the process with a non-retryable invalid error.
func (d *Definition) rejectUnsupportedCommand(tag uint64, msg string) error {
	err := apierror.New(apierror.Invalid, msg).WithRetryable(apierror.False)
	d.resumeProcess(tag, nil, err)
	return nil
}

func (d *Definition) executeProcessMonitor(_ *process.MonitorCmd, tag uint64) error {
	return d.rejectUnsupportedCommand(tag, "process.monitor not supported in workflow context: child workflows are automatically monitored")
}

func (d *Definition) executeProcessUnmonitor(_ *process.UnmonitorCmd, tag uint64) error {
	return d.rejectUnsupportedCommand(tag, "process.unmonitor not supported in workflow context")
}

func (d *Definition) executeProcessLink(_ *process.LinkCmd, tag uint64) error {
	return d.rejectUnsupportedCommand(tag, "process.link not supported in workflow context: Temporal doesn't support bidirectional linking")
}

func (d *Definition) executeProcessUnlink(_ *process.UnlinkCmd, tag uint64) error {
	return d.rejectUnsupportedCommand(tag, "process.unlink not supported in workflow context")
}

// executeProcessExec handles process.exec from workflows by executing a child workflow synchronously.
func (d *Definition) executeProcessExec(cmd *process.ExecCmd, tag uint64) error {
	workflowName := cmd.Source.String()

	var args *commonpb.Payloads
	if len(cmd.Input) > 0 {
		var err error
		args, err = d.dc.ToPayloads(cmd.Input)
		if err != nil {
			d.resumeProcess(tag, process.ExecResult{Result: &runtime.Result{Error: temporalerrors.FromTemporalError(fmt.Errorf("failed to convert arguments: %w", err))}}, nil)
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

	if cmd.HostID != "" {
		params.TaskQueueName = cmd.HostID
	}

	d.env.ExecuteChildWorkflow(params, func(result *commonpb.Payloads, err error) {
		if err != nil {
			d.enqueueYieldCompletion(tag, process.ExecResult{Result: &runtime.Result{Error: temporalerrors.FromTemporalError(err)}}, nil)
			return
		}
		var values payload.Payloads
		if err := d.dc.FromPayloads(result, &values); err != nil {
			d.enqueueYieldCompletion(tag, process.ExecResult{Result: &runtime.Result{Error: temporalerrors.FromTemporalError(err)}}, nil)
			return
		}
		if len(values) > 0 {
			d.enqueueYieldCompletion(tag, process.ExecResult{Result: &runtime.Result{Value: values[0]}}, nil)
		} else {
			d.enqueueYieldCompletion(tag, process.ExecResult{Result: &runtime.Result{}}, nil)
		}
	}, func(_ bindings.WorkflowExecution, _ error) {})

	return nil
}
