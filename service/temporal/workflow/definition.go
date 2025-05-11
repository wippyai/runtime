package workflow

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"log"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	bindings "go.temporal.io/sdk/internalbindings"
)

// DefinitionFactory creates workflow definition instances
type DefinitionFactory struct {
	ID  registry.ID
	ctx context.Context
}

// NewDefinitionFactory creates a new workflow definition factory
func NewDefinitionFactory(id registry.ID) *DefinitionFactory {
	return &DefinitionFactory{
		ID: id,
	}
}

func (f *DefinitionFactory) WithContext(ctx context.Context) any {
	return &DefinitionFactory{ID: f.ID, ctx: ctx}
}

// NewWorkflowDefinition creates a new workflow definition instance
func (f *DefinitionFactory) NewWorkflowDefinition() bindings.WorkflowDefinition {
	return &Definition{id: f.ID, ctx: f.ctx}
}

// Definition is a simple workflow definition implementation
type Definition struct {
	id     registry.ID
	ctx    context.Context
	env    bindings.WorkflowEnvironment
	dc     converter.DataConverter
	wfl    process.Workflow
	result *runtime.Result
}

// Execute implements WorkflowDefinition.Execute
func (d *Definition) Execute(env bindings.WorkflowEnvironment, header *commonpb.Header, input *commonpb.Payloads) {
	d.env = env
	d.dc = env.GetDataConverter()

	// Init the workflow
	pFactory := process.GetPrototypeFactory(d.ctx)
	if pFactory == nil {
		d.env.Complete(nil, fmt.Errorf("no prototype factory found"))
		return
	}

	proc, err := pFactory.Create(d.id)
	if err != nil {
		d.env.Complete(nil, err)
		return
	}

	wfl, ok := proc.(process.Workflow)
	if !ok {
		d.env.Complete(nil, fmt.Errorf("process does not implement Workflow interface"))
		return
	}
	d.wfl = wfl

	pid := pubsub.PID{
		Node:   pubsub.GetNode(d.ctx).ID(),
		Host:   "temporal", // todo: properly calculate this
		ID:     d.id,
		UniqID: d.env.WorkflowInfo().WorkflowExecution.ID,
	}

	var payloads payload.Payloads
	if err := d.dc.FromPayloads(input, &payloads); err != nil {
		d.env.Complete(nil, err)
		return
	}

	// Completion callback
	ctx := process.WithAddedOnComplete(d.ctx, func(pid pubsub.PID, result *runtime.Result) {
		d.result = result
	})

	// Start the workflow using the runner
	if err := d.wfl.Start(ctx, pid, payloads); err != nil {
		d.env.Complete(nil, err)
		return
	}

	// Register signal handler
	env.RegisterSignalHandler(func(name string, input *commonpb.Payloads, header *commonpb.Header) error {
		log.Printf("SIGNAL! %v", name)
		//d.wfl.Send(pubsub.NewPackage())
		return nil
	})

	env.RegisterQueryHandler(func(name string, input *commonpb.Payloads, header *commonpb.Header) (*commonpb.Payloads, error) {
		log.Printf("QUERY! %v", name)
		// todo: send as task plus callback to wait for!

		return nil, nil
	})
}

// OnWorkflowTaskStarted implements WorkflowDefinition.OnWorkflowTaskStarted
func (d *Definition) OnWorkflowTaskStarted(timeout time.Duration) {
	// iterate workflow until we advance internal state
	for {
		err := d.wfl.Step()
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
				panic(err)
			}

			// we are done, ignore anything else
			d.env.Complete(res, nil)
			return
		}

		if err != nil {
			panic(err)
		}

		if d.wfl.Ready() == 0 {
			break
		}
	}

	// Get the transcoder for command execution
	transcoder := payload.GetTranscoder(d.ctx)

	// Process commands
	commands := d.wfl.Commands()
	for _, cmd := range commands {
		if err := ExecuteCommand(cmd, d.env, d.dc, transcoder); err != nil {
			panic(fmt.Errorf("failed to execute command: %w", err))
		}
	}
}

// StackTrace implements WorkflowDefinition.StackTrace
func (d *Definition) StackTrace() string {
	// todo: implement stack trace
	return fmt.Sprintf("WorkflowID: %s", d.id.String())
}

// Close implements WorkflowDefinition.Close
func (d *Definition) Close() {
	d.wfl.Terminate()
}
