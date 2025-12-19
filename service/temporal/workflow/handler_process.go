package workflow

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	temporalerrors "github.com/wippyai/runtime/service/temporal/errors"
	commonpb "go.temporal.io/api/common/v1"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.uber.org/zap"
)

// PID host identifiers for routing
const (
	pidHostUpdate   = "update"
	pidHostTemporal = "temporal"
)

// executeProcessSend handles process.send from workflows.
func (d *Definition) executeProcessSend(cmd *process.SendCmd, tag uint64) error {
	taskQueue := d.env.WorkflowInfo().TaskQueueName

	selfPID := pid.PID{
		Node:   pid.NodeID(temporalapi.GetClientID(d.ctx)),
		Host:   taskQueue,
		UniqID: d.env.WorkflowInfo().WorkflowExecution.ID,
	}

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
	isTemporalTarget := cmd.To.Host == taskQueue || cmd.To.Host == pidHostTemporal
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
			d.resumeProcess(tag, process.SendResult{Error: temporalerrors.FromTemporalError(err)}, nil)
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
	}, func(result *commonpb.Payloads, err error) {
		if err != nil {
			d.resumeProcess(tag, process.SendResult{Error: temporalerrors.FromTemporalError(err)}, nil)
			return
		}
		d.resumeProcess(tag, process.SendResult{}, nil)
	})

	return nil
}

// handleUpdateResponse processes workflow response to an update (ack/nak/ok/error).
func (d *Definition) handleUpdateResponse(cmd *process.SendCmd, tag uint64) error {
	d.updates.HandleResponse(cmd.To.UniqID, cmd.Topic, cmd.Payloads, func(data any, err error) {
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

	if cmd.Start.HostID != "" {
		params.WorkflowOptions.TaskQueueName = cmd.Start.HostID
	}

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
			d.resumeProcess(tag, process.SpawnResult{Error: temporalerrors.FromTemporalError(err)}, nil)
			return
		}
		childPID = pid.PID{
			Node:   pid.NodeID(temporalapi.GetClientID(d.ctx)),
			Host:   params.WorkflowOptions.TaskQueueName,
			UniqID: execution.ID,
		}
		d.resumeProcess(tag, process.SpawnResult{PID: childPID}, nil)
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
				d.resumeProcess(tag, nil, temporalerrors.FromTemporalError(err))
			} else {
				d.resumeProcess(tag, nil, nil)
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
				d.resumeProcess(tag, nil, temporalerrors.FromTemporalError(err))
			} else {
				d.resumeProcess(tag, nil, nil)
			}
		},
	)
	return nil
}

// executeProcessMonitor is not supported in workflows.
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

// executeProcessLink is not supported in workflows.
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
			d.resumeProcess(tag, process.CallResult{Result: &runtime.Result{Error: temporalerrors.FromTemporalError(fmt.Errorf("failed to convert arguments: %w", err))}}, nil)
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
		params.WorkflowOptions.TaskQueueName = cmd.HostID
	}

	d.env.ExecuteChildWorkflow(params, func(result *commonpb.Payloads, err error) {
		if err != nil {
			d.resumeProcess(tag, process.CallResult{Result: &runtime.Result{Error: temporalerrors.FromTemporalError(err)}}, nil)
			return
		}
		var values payload.Payloads
		if err := d.dc.FromPayloads(result, &values); err != nil {
			d.resumeProcess(tag, process.CallResult{Result: &runtime.Result{Error: temporalerrors.FromTemporalError(err)}}, nil)
			return
		}
		if len(values) > 0 {
			d.resumeProcess(tag, process.CallResult{Result: &runtime.Result{Value: values[0]}}, nil)
		} else {
			d.resumeProcess(tag, process.CallResult{Result: &runtime.Result{}}, nil)
		}
	}, func(_ bindings.WorkflowExecution, _ error) {})

	return nil
}
