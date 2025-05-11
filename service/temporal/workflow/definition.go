package workflow

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
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
	id  registry.ID
	ctx context.Context
	env bindings.WorkflowEnvironment
	dc  converter.DataConverter
	wfl process.Workflow
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

	// Start the workflow using the runner
	if err := d.wfl.Start(d.ctx, pid, payloads); err != nil {
		d.env.Complete(nil, err)
		return
	}

	// todo: register handlers
	env.RegisterSignalHandler(func(name string, input *commonpb.Payloads, header *commonpb.Header) error {
		log.Printf("SIGNAL!")

		//values, err := w.fromPayloads(input)
		//if err != nil {
		//	return err
		//}
		//
		//if len(values) == 0 {
		//	return w.runner.SendValue(name, lua2.LNil)
		//}
		//
		//return w.runner.SendValue(name, values[0])

		return nil
	})
}

// OnWorkflowTaskStarted implements WorkflowDefinition.OnWorkflowTaskStarted
func (d *Definition) OnWorkflowTaskStarted(timeout time.Duration) {
	// This is just a placeholder implementation
	// The real implementation would process workflow tasks

	log.Printf("HELLO WORLD")

	// For now, just complete the workflow
	d.env.Complete(nil, nil)
}

// StackTrace implements WorkflowDefinition.StackTrace
func (d *Definition) StackTrace() string {
	return fmt.Sprintf("WorkflowID: %s", d.id.String())
}

// Close implements WorkflowDefinition.Close
func (d *Definition) Close() {
	// No resources to clean up in this simple implementation
}
