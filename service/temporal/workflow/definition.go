package workflow

import (
	"context"
	"fmt"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	wf "github.com/wippyai/runtime/api/workflow"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.uber.org/zap"
)

// temporalTimeReference implements workflow.TimeReference using Temporal's workflow time
type temporalTimeReference struct {
	env bindings.WorkflowEnvironment
}

func (t *temporalTimeReference) Now() time.Time {
	return t.env.Now()
}

func (t *temporalTimeReference) StartTime() time.Time {
	return t.env.WorkflowInfo().WorkflowStartTime
}

// DefinitionFactory creates workflow definition instances
type DefinitionFactory struct {
	ID     registry.ID
	HostID registry.ID
	log    *zap.Logger
	ctx    context.Context
}

// NewDefinitionFactory creates a new workflow definition factory
func NewDefinitionFactory(id registry.ID, hostID registry.ID, log *zap.Logger) *DefinitionFactory {
	return &DefinitionFactory{
		ID:     id,
		HostID: hostID,
		log:    log,
	}
}

// WithContext returns a new factory with the given context
func (f *DefinitionFactory) WithContext(ctx context.Context) any {
	return &DefinitionFactory{
		ID:     f.ID,
		HostID: f.HostID,
		log:    f.log,
		ctx:    ctx,
	}
}

// NewWorkflowDefinition creates a new workflow definition instance
func (f *DefinitionFactory) NewWorkflowDefinition() bindings.WorkflowDefinition {
	return &Definition{
		id:     f.ID,
		hostID: f.HostID,
		log:    f.log,
		ctx:    f.ctx,
	}
}

// Definition implements Temporal's WorkflowDefinition interface
type Definition struct {
	id      registry.ID
	hostID  registry.ID
	log     *zap.Logger
	ctx     context.Context
	execCtx context.Context
	env     bindings.WorkflowEnvironment
	dc      converter.DataConverter
	wfl     wf.Workflow
	result  *runtime.Result
}

// Execute implements WorkflowDefinition.Execute
func (d *Definition) Execute(env bindings.WorkflowEnvironment, header *commonpb.Header, input *commonpb.Payloads) {
	d.env = env
	d.dc = env.GetDataConverter()

	prototypes := process.GetPrototypes(d.ctx)
	if prototypes == nil {
		d.env.Complete(nil, fmt.Errorf("no prototype factory found"))
		return
	}

	factory, ok := prototypes.(process.Factory)
	if !ok {
		d.env.Complete(nil, fmt.Errorf("prototype factory does not implement Factory interface"))
		return
	}

	proc, err := factory.Create(d.id)
	if err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to create workflow process: %w", err))
		return
	}

	workflow, ok := proc.(wf.Workflow)
	if !ok {
		d.env.Complete(nil, fmt.Errorf("process does not implement Workflow interface"))
		return
	}
	d.wfl = workflow

	pid := relay.PID{
		Node:   "",
		Host:   "temporal",
		UniqID: env.WorkflowInfo().WorkflowExecution.ID,
	}

	var payloads payload.Payloads
	if err := d.dc.FromPayloads(input, &payloads); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to decode input payloads: %w", err))
		return
	}

	execCtx, fc := ctxapi.OpenFrameContextOn(d.ctx, d.ctx)

	pairs := []ctxapi.Pair{
		{Key: runtime.FrameIDKey, Value: d.id},
		{Key: runtime.FramePIDKey, Value: pid},
		{Key: runtime.FrameHostKey, Value: d.hostID},
	}

	if err := fc.SetMultiple(pairs...); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to set frame context: %w", err))
		return
	}

	// Set TimeReference for deterministic time access
	timeRef := &temporalTimeReference{env: d.env}
	if err := wf.WithTimeReference(execCtx, timeRef); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to set time reference: %w", err))
		return
	}

	// Attach upstream handler for workflow commands - must be done before Start()
	// The upstream handler will be retrieved from the workflow instance after Start()
	// We'll set a placeholder here and update it after creating the workflow

	hooks := process.GetOnCompleteHooks(execCtx)
	if err := process.SetOnCompleteHooks(execCtx, append(hooks, func(ctx context.Context, pid relay.PID, result *runtime.Result) {
		d.result = result
	})); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to set onComplete hooks: %w", err))
		return
	}

	d.execCtx = execCtx

	if err := d.wfl.Start(execCtx, pid, payloads); err != nil {
		d.env.Complete(nil, fmt.Errorf("failed to start workflow: %w", err))
		return
	}
}

// OnWorkflowTaskStarted implements WorkflowDefinition.OnWorkflowTaskStarted
func (d *Definition) OnWorkflowTaskStarted(timeout time.Duration) {
	for {
		stepResult, err := d.wfl.Step()

		if d.result != nil {
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
			return
		}

		if err != nil {
			d.env.Complete(nil, fmt.Errorf("workflow step failed: %w", err))
			return
		}

		if stepResult == process.StepDone {
			d.env.Complete(nil, nil)
			return
		}

		if stepResult == process.StepIdle {
			break
		}
	}

	transcoder := payload.GetTranscoder(d.execCtx)
	if transcoder == nil {
		d.env.Complete(nil, fmt.Errorf("no payload transcoder found"))
		return
	}

	commands := d.wfl.Commands()
	for _, cmd := range commands {
		if err := ExecuteCommand(cmd, d.env, d.dc, transcoder); err != nil {
			d.env.Complete(nil, fmt.Errorf("failed to execute command: %w", err))
			return
		}
	}
}

// StackTrace implements WorkflowDefinition.StackTrace
func (d *Definition) StackTrace() string {
	return fmt.Sprintf("Workflow: %s", d.id.String())
}

// Close implements WorkflowDefinition.Close
func (d *Definition) Close() {
	if d.wfl != nil {
		d.wfl.Terminate()
	}
}
