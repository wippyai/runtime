package workflow

import (
	"context"
	"fmt"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	commonpb "go.temporal.io/api/common/v1"
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

// Definition implements Temporal's WorkflowDefinition interface.
// It bridges the Process model with Temporal's workflow execution.
type Definition struct {
	id      registry.ID
	log     *zap.Logger
	ctx     context.Context
	execCtx context.Context
	env     bindings.WorkflowEnvironment
	dc      converter.DataConverter
	proc    process.Process
	result  *runtime.Result
	output  process.StepOutput // reusable output buffer
	signals []incomingSignal   // queued signals
}

// Execute implements WorkflowDefinition.Execute.
// Called by Temporal SDK to start workflow execution.
func (d *Definition) Execute(env bindings.WorkflowEnvironment, _ *commonpb.Header, input *commonpb.Payloads) {
	d.env = env
	d.dc = env.GetDataConverter()

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

	// Set deterministic time reference for workflow execution
	timeRef := &workflowTimeRef{
		env:       env,
		startTime: env.Now(),
	}
	if err := clockapi.WithTimeReference(execCtx, timeRef); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to set time reference: %w", err))
		return
	}

	d.execCtx = execCtx

	// Register signal handler
	env.RegisterSignalHandler(d.handleSignal)

	method := "main"
	if meta != nil && meta.Method != "" {
		method = meta.Method
	}

	if err := d.proc.Init(execCtx, method, payloads); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to start workflow: %w", err))
		return
	}
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

		d.output.Reset()
		err := d.proc.Step(events, &d.output)

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

		case process.StepContinue:
			d.output.ForEachYield(func(y process.Yield) {
				if err := d.executeCommand(y.Cmd, y.Tag); err != nil {
					d.env.Complete(nil, fmt.Errorf("failed to execute command: %w", err))
				}
			})
		}
	}
}

func (d *Definition) completeWithResult() {
	if d.result.Error != nil {
		d.env.Complete(nil, d.result.Error)
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
	case *ChildWorkflowCmd:
		return d.executeChildWorkflow(c, tag)
	case *SignalCmd:
		return d.executeSignal(c, tag)
	case *function.CallCmd:
		return d.executeFunctionCall(c, tag)
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
			d.resumeProcess(tag, nil, lar.Err)
			return
		}
		var values payload.Payloads
		if err := d.dc.FromPayloads(lar.Result, &values); err != nil {
			d.resumeProcess(tag, nil, err)
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
		d.resumeProcess(tag, nil, err)
		return
	}

	var values payload.Payloads
	if err := d.dc.FromPayloads(result, &values); err != nil {
		d.resumeProcess(tag, nil, err)
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
			d.resumeProcess(tag, nil, err)
			return
		}
		d.resumeProcess(tag, payload.NewPayload(true, payload.Golang), nil)
	})
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
				d.resumeProcess(tag, nil, err)
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
	}, func(result *commonpb.Payloads, err error) {
		d.handleFunctionCallResult(tag, result, err)
	})

	return nil
}

func (d *Definition) handleFunctionCallResult(tag uint64, result *commonpb.Payloads, err error) {
	if err != nil {
		d.resumeProcess(tag, function.CallResult{Error: err}, nil)
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
		return
	}

	switch d.output.Status() {
	case process.StepDone:
		d.result = &runtime.Result{Value: d.output.Result()}
	case process.StepYield:
		d.output.ForEachYield(func(y process.Yield) {
			if err := d.executeCommand(y.Cmd, y.Tag); err != nil {
				d.env.Complete(nil, fmt.Errorf("failed to execute command: %w", err))
			}
		})
	}
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
