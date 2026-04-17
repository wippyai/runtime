// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	workflowapi "github.com/wippyai/runtime/api/runtime/workflow"
	temporalerrors "github.com/wippyai/runtime/service/temporal/errors"
	temporaloptions "github.com/wippyai/runtime/service/temporal/options"
	commonpb "go.temporal.io/api/common/v1"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
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
	if err := applyTemporalActivityOptions(&opts, d.mergedFunctionTaskOptions(cmd.Task)); err != nil {
		d.resumeProcess(tag, function.CallResult{
			Error: fmt.Errorf("invalid temporal activity options for %s: %w", activityName, err),
		}, nil)
		return nil
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

// executeFunctionAsyncStart starts an activity and wires its completion to a future topic.
func (d *Definition) executeFunctionAsyncStart(cmd *function.AsyncStartCmd, tag uint64) error {
	activityName := cmd.Task.ID.String()

	args, err := d.dc.ToPayloads(cmd.Task.Payloads)
	if err != nil {
		return fmt.Errorf("failed to convert arguments: %w", err)
	}

	opts := bindings.ExecuteActivityOptions{
		TaskQueueName:       d.env.WorkflowInfo().TaskQueueName,
		StartToCloseTimeout: defaultActivityTimeout,
	}
	if err := applyTemporalActivityOptions(&opts, d.mergedFunctionTaskOptions(cmd.Task)); err != nil {
		d.resumeProcess(tag, function.AsyncStartResult{
			Error: fmt.Errorf("invalid temporal activity options for %s: %w", activityName, err),
		}, nil)
		return nil
	}

	if d.asyncActivities == nil {
		d.asyncActivities = make(map[string]bindings.ActivityID, 4)
	}

	topic := cmd.Topic
	activityID := d.env.ExecuteActivity(bindings.ExecuteActivityParams{
		ExecuteActivityOptions: opts,
		ActivityType:           bindings.ActivityType{Name: activityName},
		Input:                  args,
		Header:                 d.getContextHeader(),
	}, func(result *commonpb.Payloads, err error) {
		d.handleFunctionAsyncResult(topic, result, err)
	})
	d.asyncActivities[topic] = activityID

	d.resumeProcess(tag, function.AsyncStartResult{}, nil)
	return nil
}

// executeFunctionAsyncCancel requests activity cancellation and closes the future topic.
func (d *Definition) executeFunctionAsyncCancel(cmd *function.AsyncCancelCmd, tag uint64) error {
	if d.asyncActivities != nil {
		if activityID, ok := d.asyncActivities[cmd.Topic]; ok {
			delete(d.asyncActivities, cmd.Topic)
			d.env.RequestCancelActivity(activityID)
		}
	}

	d.stepProcess([]process.Event{
		{
			Type: process.EventYieldComplete,
			Tag:  tag,
		},
		{
			Type: process.EventMessage,
			Data: &relay.Package{
				Messages: []*relay.Message{{
					Topic:    cmd.Topic,
					Payloads: payload.Payloads{payload.NewTerminal()},
				}},
			},
		},
	})
	return nil
}

func (d *Definition) handleFunctionAsyncResult(topic string, result *commonpb.Payloads, err error) {
	// Canceled/unknown topics are intentionally ignored.
	if d.asyncActivities != nil {
		if _, ok := d.asyncActivities[topic]; !ok {
			return
		}
		delete(d.asyncActivities, topic)
	}

	if err != nil {
		d.enqueueInternalSignal(topic, payload.Payloads{
			payload.NewError(temporalerrors.FromTemporalError(err)),
			payload.NewTerminal(),
		})
		return
	}

	var values payload.Payloads
	if err := d.dc.FromPayloads(result, &values); err != nil {
		d.enqueueInternalSignal(topic, payload.Payloads{
			payload.NewError(temporalerrors.FromTemporalError(err)),
			payload.NewTerminal(),
		})
		return
	}

	if len(values) == 0 {
		values = payload.Payloads{payload.New(nil)}
	}
	values = append(values, payload.NewTerminal())
	d.enqueueInternalSignal(topic, values)
}

func (d *Definition) enqueueInternalSignal(topic string, payloads payload.Payloads) {
	if len(d.signals) >= maxSignalQueueSize {
		d.replayLog.Warn("signal queue full, dropping internal signal", zap.String("topic", topic))
		return
	}
	d.signals = append(d.signals, incomingSignal{
		Name:     topic,
		Payloads: payloads,
	})
}

// handleFunctionCallResult processes the result of a function call.
func (d *Definition) handleFunctionCallResult(tag uint64, result *commonpb.Payloads, err error) {
	if err != nil {
		d.enqueueYieldCompletion(tag, function.CallResult{Error: temporalerrors.FromTemporalError(err)}, nil)
		return
	}

	var values payload.Payloads
	if err := d.dc.FromPayloads(result, &values); err != nil {
		d.enqueueYieldCompletion(tag, function.CallResult{Error: temporalerrors.FromTemporalError(err)}, nil)
		return
	}

	if len(values) > 0 {
		d.enqueueYieldCompletion(tag, function.CallResult{Value: values[0]}, nil)
	} else {
		d.enqueueYieldCompletion(tag, function.CallResult{}, nil)
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
		var values payload.Payloads
		if err := d.dc.FromPayloads(result, &values); err != nil {
			d.resumeProcess(tag, workflowapi.Result{Error: temporalerrors.FromTemporalError(err)}, nil)
			return
		}
		if len(values) > 0 {
			d.resumeProcess(tag, workflowapi.Result{Value: values[0]}, nil)
			return
		}
		d.resumeProcess(tag, workflowapi.Result{}, nil)
	}, "workflow.sideEffect")
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
		overrides := attrs.NewBag()
		if cmd.Options.WorkflowID != "" {
			overrides.Set(optionWorkflowID, cmd.Options.WorkflowID)
		}
		if cmd.Options.TaskQueue != "" {
			overrides.Set(optionWorkflowTaskQueue, cmd.Options.TaskQueue)
		}
		if cmd.Options.ExecutionTimeout != "" {
			overrides.Set(optionWorkflowExecutionTimeout, cmd.Options.ExecutionTimeout)
		}
		if cmd.Options.RunTimeout != "" {
			overrides.Set(optionWorkflowRunTimeout, cmd.Options.RunTimeout)
		}
		if cmd.Options.TaskTimeout != "" {
			overrides.Set(optionWorkflowTaskTimeout, cmd.Options.TaskTimeout)
		}
		if err := applyTemporalChildWorkflowOptions(&params, overrides); err != nil {
			d.resumeProcess(tag, workflowapi.ExecResult{
				Error: fmt.Errorf("invalid temporal child workflow options for %s: %w", workflowName, err),
			}, nil)
			return nil
		}
	}

	d.env.ExecuteChildWorkflow(params, func(result *commonpb.Payloads, err error) {
		if err != nil {
			d.enqueueYieldCompletion(tag, workflowapi.ExecResult{Error: temporalerrors.FromTemporalError(err)}, nil)
			return
		}
		var values payload.Payloads
		if err := d.dc.FromPayloads(result, &values); err != nil {
			d.enqueueYieldCompletion(tag, workflowapi.ExecResult{Error: temporalerrors.FromTemporalError(err)}, nil)
			return
		}
		if len(values) > 0 {
			if values[0] != nil {
				d.replayLog.Debug("decoded workflow.exec result",
					zap.String("workflow", workflowName),
					zap.String("format", values[0].Format()))
			}
			d.enqueueYieldCompletion(tag, workflowapi.ExecResult{Value: values[0]}, nil)
		} else {
			d.enqueueYieldCompletion(tag, workflowapi.ExecResult{}, nil)
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
		searchAttributes, err := temporaloptions.MapToSearchAttributes(map[string]any(cmd.SearchAttrs))
		if err != nil {
			d.resumeProcess(tag, nil, temporalerrors.FromTemporalError(err))
			return nil
		}
		if err := d.env.UpsertTypedSearchAttributes(searchAttributes); err != nil {
			d.resumeProcess(tag, nil, temporalerrors.FromTemporalError(err))
			return nil
		}
	}

	if len(cmd.Memo) > 0 {
		if err := d.env.UpsertMemo(map[string]any(cmd.Memo)); err != nil {
			d.resumeProcess(tag, nil, temporalerrors.FromTemporalError(err))
			return nil
		}
	}

	d.resumeProcess(tag, true, nil)
	return nil
}

type functionOptionsProvider interface {
	GetOptions(id registry.ID) (runtimeapi.Bag, bool)
}

func (d *Definition) mergedFunctionTaskOptions(task runtimeapi.Task) attrs.Bag {
	var merged attrs.Bag

	if reg := function.GetRegistry(d.ctx); reg != nil {
		if provider, ok := reg.(functionOptionsProvider); ok {
			if defaults, ok := provider.GetOptions(task.ID); ok && defaults != nil {
				if cloned, ok := defaults.Clone().(attrs.Bag); ok {
					merged = cloned
				}
			}
		}
	}

	if overrides, ok := task.Options.(attrs.Bag); ok && overrides != nil {
		if merged == nil {
			if cloned, ok := overrides.Clone().(attrs.Bag); ok {
				return cloned
			}
			return overrides
		}
		return merged.Merge(overrides)
	}

	return merged
}
