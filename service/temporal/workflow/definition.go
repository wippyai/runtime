package workflow

import (
	"context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/runtime/lua/engine/command"
	"github.com/ponyruntime/pony/runtime/lua/workflow"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	bindings "go.temporal.io/sdk/internalbindings"
	"log"
	"time"
)

// DefinitionFactory creates new workflow definition instances
type DefinitionFactory struct {
	ctx    context.Context
	runner func() *workflow.Runner
}

// newDefinitionFactory creates a new factory instance with a runner factory function
func newDefinitionFactory(ctx context.Context, factory func() any) *DefinitionFactory {
	runnerFactory := func() *workflow.Runner {
		return factory().(*workflow.Runner)
	}

	return &DefinitionFactory{
		ctx:    ctx,
		runner: runnerFactory,
	}
}

// NewWorkflowDefinition creates a new workflow definition instance
func (f *DefinitionFactory) NewWorkflowDefinition() bindings.WorkflowDefinition {
	return &Definition{
		runner: f.runner(),
		ctx:    f.ctx,
	}
}

// Definition represents the core workflow implementation
type Definition struct {
	env    bindings.WorkflowEnvironment
	runner *workflow.Runner
	ctx    context.Context
	dc     converter.DataConverter
	dtt    payload.Transcoder
}

// Execute implements the Definition interface
func (w *Definition) Execute(env bindings.WorkflowEnvironment, header *commonpb.Header, input *commonpb.Payloads) {
	w.env = env
	w.dc = env.GetDataConverter()

	w.dtt = payload.GetTranscoder(w.ctx)
	// todo: encapsulate this! => execute_workflow

	// Start the workflow using the runner
	if err := w.runner.Start(w.ctx, "execute_workflow"); err != nil {
		// Handle error appropriately
		w.env.Complete(nil, err)
		return
	}
}

// OnWorkflowTaskStarted implements the Definition interface
func (w *Definition) OnWorkflowTaskStarted(deadlockDetectionTimeout time.Duration) {
	// Process workflow steps
	cmds, err := w.runner.Step()
	if err != nil {
		w.env.Complete(nil, err)
		return
	}

	// Process commands and handle completion
	if err := w.processCommands(cmds); err != nil {
		w.env.Complete(nil, err)
		return
	}

	// Check if workflow is complete
	if w.runner.IsComplete() {
		result, err := w.runner.GetCompletionResult()
		if err != nil {
			w.env.Complete(nil, err)
			return
		}

		// todo: also encapsulate!
		p, err := w.env.GetDataConverter().ToPayloads(result)
		if err != nil {
			w.env.Complete(nil, err)
			return
		}
		w.env.Complete(p, nil)
	}
}

// processCommands handles the workflow commands
func (w *Definition) processCommands(commands []*command.Command) error {
	for _, cmd := range commands {
		switch cmd.CmdType() {
		case "activity":
			// Handle activity execution
			if err := w.executeActivity(cmd); err != nil {
				return err
			}
		case "timer":
			// Handle timer execution
			if err := w.executeTimer(cmd); err != nil {
				return err
			}
		}
	}
	return nil
}

// StackTrace implements the Definition interface
func (w *Definition) StackTrace() string {
	// Implement stack trace logic
	return "todo: workflow stack trace"
}

// Close implements the Definition interface
func (w *Definition) Close() {
	if w.runner != nil {
		w.runner.Stop()
	}
}

// executeActivity handles the execution of an activity command
func (w *Definition) executeActivity(cmd *command.Command) error {
	name := cmd.Params[0]
	log.Printf("Executing activity: %s\n", name)

	opts := cmd.Params[1]
	log.Printf("Options: %s\n", opts)

	// get all args
	args := cmd.Params[2:]
	log.Printf("Args: %v\n", args)

	//bindings.ExecuteActivityParams{
	//	ExecuteActivityOptions: bindings.ExecuteActivityOptions{
	//		ActivityID:             "", // optional
	//		TaskQueueName:          "",
	//		ScheduleToCloseTimeout: 0,
	//		ScheduleToStartTimeout: 0,
	//		StartToCloseTimeout:    0, // at least one interval
	//		HeartbeatTimeout:       0,
	//		WaitForCancellation:    false,
	//		OriginalTaskQueueName:  "",
	//		RetryPolicy:            nil,
	//		DisableEagerExecution:  false,
	//		VersioningIntent:       0,
	//		Summary:                "",
	//	},
	//	ActivityType:  struct{ Name string }{Name: "simple-activity"},
	//	Input:         nil,
	//	DataConverter: nil,
	//	Header:        nil,
	//}

	/*
		ip, err := dt.ToPayloads(lua.ToGoAny(cmd.Params[0]))
					if err != nil {
						l.env.Complete(nil, err)
						return
					}

					// execute activity
					l.env.ExecuteActivity(bindings.ExecuteActivityParams{
						ExecuteActivityOptions: bindings.ExecuteActivityOptions{
							ActivityID:          "simple-activity",
							TaskQueueName:       l.env.WorkflowInfo().TaskQueueName,
							StartToCloseTimeout: time.Second * 5,
						},
						ActivityType: struct{ Name string }{Name: "simple-activity"},
						Input:        ip,
					}, func(result *commonpb.Payloads, err error) {
						//log.Printf("Activity result: %v %v\n", result, err)

						if err != nil {
							err := l.runner.SendError(cmd, err)
							if err != nil {
								// todo: for real?
								l.env.Complete(nil, err)
								return
							}
							return
						}

						var value = new(any)
						if err := dt.FromPayloads(result, value); err != nil {
							l.env.Complete(nil, err)
							return
						}

						// todo: use our transcoder
						err = l.runner.SendResult(cmd, lua.GoToLua(*value))
						if err != nil {
							l.env.Complete(nil, err)
							return
						}
					})
	*/

	return nil
}

// executeTimer handles the execution of a timer command
func (w *Definition) executeTimer(cmd *command.Command) error {
	return nil
}
