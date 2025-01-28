package main

import (
	"context"
	"github.com/ponyruntime/pony/pkg/payload/lua"
	runtime "github.com/ponyruntime/pony/runtime/lua/workflow"
	"log"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/command"
	"github.com/ponyruntime/pony/runtime/lua/engine/pubsub"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
)

// ActivityInput represents the input for our activity
type ActivityInput struct {
	Message string
}

// SimpleActivity is our activity implementation
func SimpleActivity(ctx context.Context, input ActivityInput) (string, error) {
	log.Printf("!!Activity received input: %v\n", input)
	return "Processed: " + input.Message, nil
}

// LuaWorkflowDefinition represents our workflow implementation
type LuaWorkflowDefinition struct {
	env    bindings.WorkflowEnvironment
	runner *runtime.WorkflowRunner
	ctx    context.Context
}

func NewLuaWorkflowDefinition(ctx context.Context) *LuaWorkflowDefinition {
	return &LuaWorkflowDefinition{
		ctx: ctx,
	}
}

func (l *LuaWorkflowDefinition) NewWorkflowDefinition() bindings.WorkflowDefinition {
	return &LuaWorkflowDefinition{
		ctx: l.ctx,
	}
}

// Execute implements the WorkflowDefinition interface
func (l *LuaWorkflowDefinition) Execute(env bindings.WorkflowEnvironment, header *commonpb.Header, input *commonpb.Payloads) {
	l.env = env

	// Create VM with required modules
	vm, err := engine.NewCVM(
		zap.NewNop(),
		engine.WithPreloaded("command", command.NewCommandModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)

	// todo: add temporal specific + time + time temporal specific command bypass

	if err != nil {
		// todo: how do we handle errors in the workflow?
		log.Printf("Error creating VM: %v\n", err)
		return
	}

	luaScript := `
		function test_workflow()
			-- Create a command to process activity
			local cmd = command.new("SimpleActivity", {message = "Hello from Lua!"})
			local resp = cmd:response()
			
			-- Wait for response
			local result = resp:receive()
			return result .. "YO"
		end
	`

	// Import Lua script
	err = vm.Import(luaScript, "test", "test_workflow")
	if err != nil {
		log.Printf("Error importing script: %v\n", err)
		return
	}

	// Create layers
	channels := channel.NewChannelLayer()
	cmdLayer := command.NewCommandLayer(channels)
	pubLayer := pubsub.NewSubscriptionLayer(channels)

	// Create runner with layers
	engineRunner := engine.NewRunner(vm,
		engine.WithLayer(channels),
		engine.WithLayer(cmdLayer),
		engine.WithLayer(pubLayer),
	)

	// Create workflow runner
	l.runner = runtime.NewWorkflowRunner(engineRunner, cmdLayer, pubLayer)

	// Start the workflow
	err = l.runner.Start(l.ctx, "test_workflow")
	if err != nil {
		// todo: how do we handle errors in the workflow?
		log.Printf("Error starting workflow: %v\n", err)
		return
	}
}

// OnWorkflowTaskStarted implements the WorkflowDefinition interface
func (l *LuaWorkflowDefinition) OnWorkflowTaskStarted(deadlockDetectionTimeout time.Duration) {
	// Process workflow steps
	cmds, err := l.runner.Step()
	if err != nil {
		l.env.Complete(nil, err)
		return
	}

	log.Printf("Commands: %v\n", cmds)
	dt := l.env.GetDataConverter()
	//// Process commands if any
	for _, cmd := range cmds {
		switch cmd.CmdType() {
		case "SimpleActivity":
			log.Printf("Processing activity command: %v\n", lua.ToGoAny(cmd.Params[0]))

			ip, err := dt.ToPayloads(lua.ToGoAny(cmd.Params[0]))
			if err != nil {
				l.env.Complete(nil, err)
				return
			}

			// Execute activity
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
					err := l.runner.SetCommandError(cmd, err)
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

				log.Printf("Activity result: %v\n", *value)

				// todo: use our transcoder
				err = l.runner.SetCommandResult(cmd, lua.GoToLua(*value))
				if err != nil {
					l.env.Complete(nil, err)
					return
				}
			})
		}
	}

	// Check if workflow is complete
	if l.runner.IsComplete() {
		log.Printf("!!!!!!!!Workflow is complete\n")
		result, err := l.runner.GetCompletionResult()
		if err != nil {
			l.env.Complete(nil, err)
			return
		}

		p, err := l.env.GetDataConverter().ToPayloads(result.String())
		if err != nil {
			l.env.Complete(nil, err)
			return
		}
		l.env.Complete(p, nil)
	}
}

// StackTrace implements the WorkflowDefinition interface
func (l *LuaWorkflowDefinition) StackTrace() string {
	// todo: implmenet
	return "Workflow stack trace"
}

// Close implements the WorkflowDefinition interface
func (l *LuaWorkflowDefinition) Close() {
	l.runner.Stop()
}

func main() {
	// Create temporal client
	c, err := client.Dial(client.Options{
		HostPort: client.DefaultHostPort,
	})
	if err != nil {
		log.Fatalln("Unable to create client", err)
	}
	defer c.Close()

	// Create worker
	w := worker.New(c, "lua-task-queue", worker.Options{})

	// Create context for workflow
	ctx := context.Background()

	// Register workflow and activity
	w.RegisterWorkflowWithOptions(
		NewLuaWorkflowDefinition(ctx),
		workflow.RegisterOptions{Name: "lua-workflow"},
	)
	w.RegisterActivityWithOptions(
		SimpleActivity,
		activity.RegisterOptions{Name: "simple-activity"},
	)

	// Start worker
	go func() {
		if err := w.Run(worker.InterruptCh()); err != nil {
			log.Fatalln("Unable to start worker", err)
		}
	}()

	// Execute workflow
	workflowOptions := client.StartWorkflowOptions{
		ID:        "lua-workflow",
		TaskQueue: "lua-task-queue",
	}
	input := ActivityInput{Message: "Hello from Temporal!"}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	we, err := c.ExecuteWorkflow(ctx, workflowOptions, "lua-workflow", input)
	if err != nil {
		log.Fatalln("Unable to execute workflow", err)
	}

	var result string
	if err := we.Get(ctx, &result); err != nil {
		log.Fatalln("Unable to get workflow result", err)
	}

	log.Printf("Workflow result: %s\n", result)

	// Keep the program running to observe logs
	time.Sleep(1 * time.Second)
}
