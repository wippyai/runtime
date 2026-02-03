package workflow

import (
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	workflowapi "github.com/wippyai/runtime/api/runtime/workflow"
	temporalerrors "github.com/wippyai/runtime/service/temporal/errors"
	commonpb "go.temporal.io/api/common/v1"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.temporal.io/sdk/workflow"
)

// Default timeouts for activity execution
const (
	defaultActivityTimeout = 10 * time.Minute
)

// executeFunctionCall executes a function as an activity.
func (d *Definition) executeFunctionCall(cmd *function.CallCmd, tag uint64) error {
	activityName := cmd.Task.ID.String()

	args, err := d.dc.ToPayloads(cmd.Task.Payloads)
	if err != nil {
		return fmt.Errorf("failed to convert arguments: %w", err)
	}

	opts := bindings.ExecuteActivityOptions{
		TaskQueueName:       d.env.WorkflowInfo().TaskQueueName,
		StartToCloseTimeout: defaultActivityTimeout,
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

// handleFunctionCallResult processes the result of a function call.
func (d *Definition) handleFunctionCallResult(tag uint64, result *commonpb.Payloads, err error) {
	if err != nil {
		d.resumeProcess(tag, function.CallResult{Error: temporalerrors.FromTemporalError(err)}, nil)
		return
	}

	var values payload.Payloads
	if err := d.dc.FromPayloads(result, &values); err != nil {
		d.resumeProcess(tag, function.CallResult{Error: temporalerrors.FromTemporalError(err)}, nil)
		return
	}

	if len(values) > 0 {
		d.resumeProcess(tag, function.CallResult{Value: values[0]}, nil)
	} else {
		d.resumeProcess(tag, function.CallResult{}, nil)
	}
}

// executeSideEffect executes a side effect function deterministically.
func (d *Definition) executeSideEffect(cmd *workflowapi.SideEffectCmd, tag uint64) error {
	d.env.SideEffect(func() (*commonpb.Payloads, error) {
		if cmd.Fn == nil {
			return nil, fmt.Errorf("side effect function is nil")
		}
		value, err := cmd.Fn()
		if err != nil {
			return nil, err
		}
		if binData, ok := value.([]byte); ok {
			return d.dc.ToPayloads(payload.NewPayload(binData, payload.Bytes))
		}
		return d.dc.ToPayloads(value)
	}, func(result *commonpb.Payloads, err error) {
		if err != nil {
			d.resumeProcess(tag, workflowapi.Result{Error: temporalerrors.FromTemporalError(err)}, nil)
			return
		}
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
		var value any
		if err := d.dc.FromPayloads(result, &value); err != nil {
			d.resumeProcess(tag, workflowapi.Result{Error: temporalerrors.FromTemporalError(err)}, nil)
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

// executeWorkflowExec executes a child workflow synchronously and returns the result.
func (d *Definition) executeWorkflowExec(cmd *workflowapi.ExecCmd, tag uint64) error {
	workflowName := cmd.ID.String()

	var args *commonpb.Payloads
	if len(cmd.Args) > 0 {
		var err error
		args, err = d.dc.ToPayloads(cmd.Args)
		if err != nil {
			d.resumeProcess(tag, workflowapi.ExecResult{Error: fmt.Errorf("failed to convert arguments: %w", err)}, nil)
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
			params.WorkflowID = cmd.Options.WorkflowID
		}
		if cmd.Options.TaskQueue != "" {
			params.TaskQueueName = cmd.Options.TaskQueue
		}
		if cmd.Options.ExecutionTimeout != "" {
			dur, err := time.ParseDuration(cmd.Options.ExecutionTimeout)
			if err == nil {
				params.WorkflowExecutionTimeout = dur
			}
		}
		if cmd.Options.RunTimeout != "" {
			dur, err := time.ParseDuration(cmd.Options.RunTimeout)
			if err == nil {
				params.WorkflowRunTimeout = dur
			}
		}
		if cmd.Options.TaskTimeout != "" {
			dur, err := time.ParseDuration(cmd.Options.TaskTimeout)
			if err == nil {
				params.WorkflowTaskTimeout = dur
			}
		}
	}

	d.env.ExecuteChildWorkflow(params, func(result *commonpb.Payloads, err error) {
		if err != nil {
			d.resumeProcess(tag, workflowapi.ExecResult{Error: temporalerrors.FromTemporalError(err)}, nil)
			return
		}
		var values payload.Payloads
		if err := d.dc.FromPayloads(result, &values); err != nil {
			d.resumeProcess(tag, workflowapi.ExecResult{Error: temporalerrors.FromTemporalError(err)}, nil)
			return
		}
		if len(values) > 0 {
			d.resumeProcess(tag, workflowapi.ExecResult{Value: values[0]}, nil)
		} else {
			d.resumeProcess(tag, workflowapi.ExecResult{}, nil)
		}
	}, func(_ bindings.WorkflowExecution, _ error) {})

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
	if len(cmd.SearchAttrs) > 0 {
		if err := d.env.UpsertSearchAttributes(cmd.SearchAttrs); err != nil {
			d.resumeProcess(tag, nil, temporalerrors.FromTemporalError(err))
			return nil
		}
	}

	if len(cmd.Memo) > 0 {
		if err := d.env.UpsertMemo(cmd.Memo); err != nil {
			d.resumeProcess(tag, nil, temporalerrors.FromTemporalError(err))
			return nil
		}
	}

	d.resumeProcess(tag, true, nil)
	return nil
}
