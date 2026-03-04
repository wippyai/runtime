// SPDX-License-Identifier: MPL-2.0

// Package workflow implements Temporal workflow definitions, signal/update handling,
// timer management, and process command execution within the Temporal deterministic runtime.
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
	ctx context.Context
	log *zap.Logger
	ID  registry.ID
	// Captured at registration time from worker context to avoid identity loss
	// if SDK-invoked definition instances don't preserve context values.
	clientID string
	workerID string
}

// WithContext returns a new factory with the given context.
func (f *DefinitionFactory) WithContext(ctx context.Context) any {
	clientID := temporalapi.GetClientID(ctx)
	workerID := temporalapi.GetWorkerID(ctx)
	return &DefinitionFactory{
		ID:       f.ID,
		log:      f.log,
		ctx:      ctx,
		clientID: clientID,
		workerID: workerID,
	}
}

// NewWorkflowDefinition creates a new workflow definition instance.
func (f *DefinitionFactory) NewWorkflowDefinition() bindings.WorkflowDefinition {
	return &Definition{
		id:       f.ID,
		log:      f.log,
		ctx:      f.ctx,
		clientID: f.clientID,
		workerID: f.workerID,
	}
}

// incomingSignal represents a queued signal to be delivered to the workflow.
type incomingSignal struct {
	From     pid.PID
	Name     string
	Payloads payload.Payloads
}

// childExitEvent represents a child workflow completion to be delivered as EXIT event.
type childExitEvent struct {
	Result   payload.Payload
	Error    error
	ChildPID pid.PID
}

// Definition implements Temporal's WorkflowDefinition interface.
type Definition struct {
	env        bindings.WorkflowEnvironment
	dc         converter.DataConverter
	proc       process.Process
	frameCtx   ctxapi.FrameContext
	ctx        context.Context
	execCtx    context.Context
	log        *zap.Logger
	result     *runtime.Result
	replayLog  *propagator.ReplayLogger
	queryState map[string]any
	timers     *TimerManager
	updates    *UpdateManager
	id         registry.ID
	clientID   string
	workerID   string
	signals    []incomingSignal
	childExits []childExitEvent
	// asyncActivities tracks in-flight funcs.async activity calls by future topic.
	asyncActivities map[string]bindings.ActivityID
	// pendingCompletions buffers async yield completions raised from Temporal callbacks.
	// They are drained in OnWorkflowTaskStarted so command generation happens only after
	// the SDK has set command event sequencing for the current workflow task.
	pendingCompletions []process.Event
	output             process.StepOutput
	canceled           bool
	completed          bool
}

// Execute implements WorkflowDefinition.Execute.
func (d *Definition) Execute(env bindings.WorkflowEnvironment, header *commonpb.Header, input *commonpb.Payloads) {
	if d.frameCtx != nil {
		ctxapi.ReleaseFrameContext(d.frameCtx)
		d.frameCtx = nil
	}

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
		d.env.Complete(nil, fmt.Errorf(
			"failed to decode input payloads (converter=%T workflow=%s): %w",
			d.dc,
			d.id.String(),
			err,
		))
		return
	}

	clientID := d.resolveClientID()
	workerID := d.resolveWorkerID(env.WorkflowInfo().TaskQueueName)
	processPID := pid.PID{
		Node:   clientID,
		Host:   workerID,
		UniqID: env.WorkflowInfo().WorkflowExecution.ID,
	}

	execCtx, fc := ctxapi.ForkFrameContext(d.ctx)
	keepFrame := false
	defer func() {
		if !keepFrame {
			ctxapi.ReleaseFrameContext(fc)
		}
	}()
	pairs := []ctxapi.Pair{
		{Key: runtime.FrameIDKey, Value: d.id},
		{Key: runtime.FramePIDKey, Value: processPID},
	}

	if err := fc.SetMultiple(pairs...); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to set frame context: %w", err))
		return
	}

	if ctxValues, err := propagator.ExtractFromHeader(d.dc, header); err != nil {
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

	if secPayload, err := propagator.ExtractSecurityFromHeader(d.dc, header); err != nil {
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

	d.execCtx = execCtx
	d.frameCtx = fc
	keepFrame = true
}

func (d *Definition) resolveClientID() string {
	if clientID := temporalapi.GetClientID(d.ctx); clientID != "" {
		return clientID
	}
	return d.clientID
}

func (d *Definition) resolveWorkerID(fallback string) string {
	if workerID := temporalapi.GetWorkerID(d.ctx); workerID != "" {
		return workerID
	}
	if d.workerID != "" {
		return d.workerID
	}
	return fallback
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
	return d.getContextHeaderFrom(d.execCtx, nil)
}

func (d *Definition) getContextHeaderWithValues(extra map[string]any) *commonpb.Header {
	return d.getContextHeaderFrom(d.execCtx, extra)
}

func (d *Definition) getContextHeaderFrom(ctx context.Context, extra map[string]any) *commonpb.Header {
	var header *commonpb.Header

	values := ctxapi.GetValues(ctx)
	data := make(map[string]any)
	if values != nil && values.Len() > 0 {
		values.Iterate(func(key string, val any) {
			switch val.(type) {
			case string, int, int64, float64, bool, map[string]any, []any:
				data[key] = val
			}
		})
	}
	if len(extra) > 0 {
		for k, v := range extra {
			data[k] = v
		}
	}

	if len(data) > 0 {
		var err error
		header, err = propagator.CreateHeader(d.dc, data)
		if err != nil {
			d.replayLog.Warn("failed to create context header", zap.Error(err))
		}
	}

	secPayload := propagator.ExtractSecurityPayload(ctx)
	if secPayload != nil {
		var err error
		header, err = propagator.AddSecurityToHeader(d.dc, header, secPayload)
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
	if d.frameCtx != nil {
		ctxapi.ReleaseFrameContext(d.frameCtx)
		d.frameCtx = nil
	}
}
