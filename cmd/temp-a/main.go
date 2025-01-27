package main

import (
	"context"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/activity"
	bindings "go.temporal.io/sdk/internalbindings"
	"log"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// ActivityInput represents the input for our activity
type ActivityInput struct {
	Message string
}

// SimpleActivity is our activity implementation in Go
func SimpleActivity(ctx context.Context, input ActivityInput) (string, error) {
	log.Printf("Activity received input: %v\n", input)
	return "Processed: " + input.Message, nil
}

// LuaWorkflowDefinition represents our custom workflow definition
type LuaWorkflowDefinition struct {
	env bindings.WorkflowEnvironment
}

func (l *LuaWorkflowDefinition) NewWorkflowDefinition() bindings.WorkflowDefinition {
	return &LuaWorkflowDefinition{}
}

// Execute implements the WorkflowDefinition interface
func (l *LuaWorkflowDefinition) Execute(env bindings.WorkflowEnvironment, header *commonpb.Header, input *commonpb.Payloads) {
	l.env = env

	// Log workflow inputs for observation
	var workflowInput ActivityInput
	if err := env.GetDataConverter().FromPayloads(input, &workflowInput); err != nil {
		log.Printf("Error deserializing input: %v\n", err)
		return
	}

	log.Printf("Workflow received input: %v\n", workflowInput)
	log.Printf("Workflow info: %+v\n", env.WorkflowInfo())

}

// OnWorkflowTaskStarted implements the WorkflowDefinition interface
func (l *LuaWorkflowDefinition) OnWorkflowTaskStarted(deadlockDetectionTimeout time.Duration) {
	log.Printf("Workflow task started with deadline: %v\n", deadlockDetectionTimeout)

	l.env.ExecuteActivity(bindings.ExecuteActivityParams{
		ExecuteActivityOptions: bindings.ExecuteActivityOptions{
			ActivityID:          "simple-activity",
			TaskQueueName:       l.env.WorkflowInfo().TaskQueueName,
			StartToCloseTimeout: time.Second * 5,
		},
		ActivityType: struct{ Name string }{Name: "simple-activity"},
	}, func(result *commonpb.Payloads, err error) {
		log.Printf("Activity result: %v\n", result)
	})

	p, err := l.env.GetDataConverter().ToPayloads("Hello from Lua Temporal!2")
	if err != nil {
		log.Printf("Error serializing output: %v\n", err)
		return
	}

	l.env.Complete(p, nil)
}

func (l *LuaWorkflowDefinition) StackTrace() string { return "" }
func (l *LuaWorkflowDefinition) Close()             {}

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

	// Register workflow and activity
	w.RegisterWorkflowWithOptions(
		&LuaWorkflowDefinition{},
		workflow.RegisterOptions{Name: "lua-workflow"},
	)
	w.RegisterActivityWithOptions(SimpleActivity, activity.RegisterOptions{Name: "simple-activity"})

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
	input := ActivityInput{Message: "Hello from Lua Temporal!"}

	we, err := c.ExecuteWorkflow(context.Background(), workflowOptions, "lua-workflow", input)
	if err != nil {
		log.Fatalln("Unable to execute workflow", err)
	}

	var result string
	if err := we.Get(context.Background(), &result); err != nil {
		log.Fatalln("Unable to get workflow result", err)
	}

	log.Printf("Workflow result: %s\n", result)

	// Keep the program running to observe logs
	time.Sleep(1 * time.Second)
}
