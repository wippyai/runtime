package workflow

import (
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	bindings "go.temporal.io/sdk/internalbindings"
)

// Definition is a simple workflow definition implementation
type Definition struct {
	ID  registry.ID
	env bindings.WorkflowEnvironment
	dc  converter.DataConverter
}

// Execute implements WorkflowDefinition.Execute
func (d *Definition) Execute(env bindings.WorkflowEnvironment, header *commonpb.Header, input *commonpb.Payloads) {
	d.env = env
	d.dc = env.GetDataConverter()
	// This is just a placeholder implementation
	// The real implementation would do something useful with the input
}

// OnWorkflowTaskStarted implements WorkflowDefinition.OnWorkflowTaskStarted
func (d *Definition) OnWorkflowTaskStarted(timeout time.Duration) {
	// This is just a placeholder implementation
	// The real implementation would process workflow tasks

	// For now, just complete the workflow
	d.env.Complete(nil, nil)
}

// StackTrace implements WorkflowDefinition.StackTrace
func (d *Definition) StackTrace() string {
	return fmt.Sprintf("WorkflowID: %s", d.ID.String())
}

// Close implements WorkflowDefinition.Close
func (d *Definition) Close() {
	// No resources to clean up in this simple implementation
}

// DefinitionFactory creates workflow definition instances
type DefinitionFactory struct {
	ID registry.ID
}

// NewDefinitionFactory creates a new workflow definition factory
func NewDefinitionFactory(id registry.ID) *DefinitionFactory {
	return &DefinitionFactory{
		ID: id,
	}
}

// NewWorkflowDefinition creates a new workflow definition instance
func (f *DefinitionFactory) NewWorkflowDefinition() bindings.WorkflowDefinition {
	return &Definition{
		ID: f.ID,
	}
}
