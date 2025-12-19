package workflow

import (
	"context"
	"fmt"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	workflowapi "github.com/wippyai/runtime/api/runtime/workflow"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	temporalerrors "github.com/wippyai/runtime/service/temporal/errors"
	"github.com/wippyai/runtime/service/temporal/propagator"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.uber.org/zap"
)

const (
	maxSignalQueueSize    = 10000
	maxChildExitQueueSize = 1000
)

// workflowTimeRef implements clock.TimeReference using Temporal's deterministic time.
type workflowTimeRef struct {
	env bindings.WorkflowEnvironment
}

func (r *workflowTimeRef) Now() time.Time       { return r.env.Now() }
func (r *workflowTimeRef) StartTime() time.Time { return r.env.Now() }

// DefinitionFactory creates workflow definition instances.
type DefinitionFactory struct {
	ID  registry.ID
	log *zap.Logger
	ctx context.Context
}

// WithContext returns a new factory with the given context.
func (f *DefinitionFactory) WithContext(ctx context.Context) any {
	return &DefinitionFactory{
		ID:  f.ID,
		log: f.log,
		ctx: ctx,
	}
}

// NewWorkflowDefinition creates a new workflow definition instance.
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

// Definition implements Temporal's WorkflowDefinition interface.
type Definition struct {
	id        registry.ID
	log       *zap.Logger
	replayLog *propagator.ReplayLogger
	ctx       context.Context
	execCtx   context.Context
	env       bindings.WorkflowEnvironment
	dc        converter.DataConverter
	proc      process.Process
	result    *runtime.Result
	output    process.StepOutput
	signals   []incomingSignal

	childExits []childExitEvent
	canceled   bool
	completed  bool
	queryState map[string]any

	timers  *TimerManager
	updates *UpdateManager
}

// Execute implements WorkflowDefinition.Execute.
func (d *Definition) Execute(env bindings.WorkflowEnvironment, header *commonpb.Header, input *commonpb.Payloads) {
	d.env = env
	d.dc = env.GetDataConverter()
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
		Node:   pid.NodeID(temporalapi.GetClientID(d.ctx)),
		Host:   env.WorkflowInfo().TaskQueueName,
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

	if secPayload, err := propagator.ExtractSecurityFromHeader(header); err != nil {
		d.replayLog.Warn("failed to extract security from header", zap.Error(err))
	} else if secPayload != nil {
		if err := propagator.ApplySecurityPayload(execCtx, secPayload); err != nil {
			d.replayLog.Warn("failed to apply security context", zap.Error(err))
		}
	}

	timeRef := &workflowTimeRef{env: env}
	if err := clockapi.WithTimeReference(execCtx, timeRef); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to set time reference: %w", err))
		return
	}

	if err := workflowapi.SetDeterministic(execCtx); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to set deterministic mode: %w", err))
		return
	}

	if err := workflowapi.SetInfoProvider(execCtx, d); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to set info provider: %w", err))
		return
	}

	d.execCtx = execCtx

	env.RegisterCancelHandler(d.handleCancel)
	env.RegisterSignalHandler(d.handleSignal)
	env.RegisterQueryHandler(d.handleQuery)
	env.RegisterUpdateHandler(d.handleUpdate)

	d.queryState = make(map[string]any)
	d.timers = NewTimerManager(env, d.replayLog, &d.signals)
	d.updates = NewUpdateManager(d.replayLog)

	method := "main"
	if meta != nil && meta.Method != "" {
		method = meta.Method
	}

	if err := d.proc.Init(execCtx, method, payloads); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to start workflow: %w", err))
		return
	}
}

func (d *Definition) completeWithResult() {
	d.completed = true
	if d.result == nil {
		d.env.Complete(nil, nil)
		return
	}

	if d.result.Error != nil {
		d.env.Complete(nil, temporalerrors.ToApplicationError(d.result.Error))
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
