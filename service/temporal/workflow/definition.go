package workflow

import (
	"context"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/runtime/lua/engine/command"
	"github.com/ponyruntime/pony/runtime/lua/workflow"
	"github.com/ponyruntime/pony/system/payload/lua"
	lua2 "github.com/yuin/gopher-lua"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	bindings "go.temporal.io/sdk/internalbindings"
	workflow2 "go.temporal.io/sdk/workflow"
	"time"
)

var envCtx = &contextapi.Key{Name: "temporal.env"}

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
	if err := w.runner.Start(context.WithValue(w.ctx, envCtx, env), "execute_workflow"); err != nil {
		// Handle error appropriately
		w.env.Complete(nil, err)
		return
	}

	env.RegisterSignalHandler(func(name string, input *commonpb.Payloads, header *commonpb.Header) error {
		values, err := w.fromPayloads(input)
		if err != nil {
			return err
		}

		if len(values) == 0 {
			return w.runner.SendValue(name, lua2.LNil)
		}

		return w.runner.SendValue(name, values[0])
	})
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
		panic(err)
	}

	// Check if workflow is complete
	if w.runner.IsComplete() {
		result, err := w.runner.GetCompletionResult()
		if err != nil {
			panic(err)
		}

		// todo: also encapsulate!
		p, err := w.env.GetDataConverter().ToPayloads(result)
		if err != nil {
			panic(err)
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

	var activityOptions = new(ActivityOptions)
	if err := w.dtt.Unmarshal(payload.NewPayload(cmd.Params[1], payload.Lua), activityOptions); err != nil {
		return err
	}

	tOps, err := activityOptions.ToExecuteActivityOptions()
	if err != nil {
		return err
	}

	// get all args
	args, err := w.toPayloads(cmd.Params[2:])
	if err != nil {
		return err
	}

	w.env.ExecuteActivity(bindings.ExecuteActivityParams{
		ExecuteActivityOptions: tOps,
		ActivityType:           struct{ Name string }{Name: name.String()},
		Input:                  args,
	}, func(result *commonpb.Payloads, err error) {
		if err != nil {
			err := w.runner.SendError(cmd, err)
			if err != nil {
				panic(err) // must never happen
			}
			return
		}

		values, err := w.fromPayloads(result)
		if err != nil {
			panic(err)
		}

		if len(values) == 0 {
			err = w.runner.SendResult(cmd, lua2.LNil)
			if err != nil {
				panic(err)
			}
			return
		}

		err = w.runner.SendResult(cmd, values[0])
		if err != nil {
			panic(err)
		}
	})

	return nil
}

// executeTimer handles the execution of a timer command
func (w *Definition) executeTimer(cmd *command.Command) error {
	// Get duration from the timer parameters
	var timerOptions = new(struct {
		Duration string `json:"duration"`
	})

	if err := w.dtt.Unmarshal(payload.NewPayload(cmd.Params[0], payload.Lua), timerOptions); err != nil {
		return err
	}

	// Parse duration string to time.Duration
	duration, err := time.ParseDuration(timerOptions.Duration)
	if err != nil {
		return err
	}

	// Create and start the timer using workflow environment
	w.env.NewTimer(duration, workflow2.TimerOptions{Summary: ""}, func(result *commonpb.Payloads, err error) {
		if err != nil {
			err := w.runner.SendError(cmd, err)
			if err != nil {
				panic(err) // must never happen
			}
			return
		}

		// Send success result back to Lua
		err = w.runner.SendResult(cmd, lua2.LBool(true))
		if err != nil {
			panic(err)
		}
	})

	return nil
}

func (w *Definition) toPayloads(args []lua2.LValue) (*commonpb.Payloads, error) {
	argPayloads := make(payload.Payloads, len(args))
	for i, arg := range args {
		argPayloads[i] = payload.NewPayload(arg, payload.Lua)
	}

	return w.dc.ToPayloads(argPayloads)
}

func (w *Definition) fromPayloads(payloads *commonpb.Payloads) ([]lua2.LValue, error) {
	var args = make([]lua2.LValue, len(payloads.GetPayloads()))
	for i, p := range payloads.GetPayloads() {
		var value = new(any)
		if err := w.dc.FromPayload(p, value); err != nil {
			return nil, err
		}
		args[i] = lua.GoToLua(*value)
	}

	return args, nil
}
