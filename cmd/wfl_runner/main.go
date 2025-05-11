package main

import (
	"context"
	"fmt"
	"go.temporal.io/sdk/client"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// LuaDataInput defines the input structure for the Lua workflow
type LuaDataInput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// LuaDataResult defines the expected result structure from the Lua workflow
type LuaDataResult struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

func main() {
	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-signalChan
		log.Println("Shutdown signal received, canceling operations...")
		cancel()
	}()

	// Create the Temporal client
	c, err := client.Dial(client.Options{})
	if err != nil {
		log.Fatalln("Unable to create Temporal client:", err)
	}
	defer c.Close()

	// Task queue defined in the _index.yaml
	taskQueue := "simple-task-queue-2"

	// Create input for the workflow
	input := LuaDataInput{
		ID:   "test-123",
		Name: "Test Data Item",
	}

	// Options for starting the workflow
	workflowOptions := client.StartWorkflowOptions{
		ID:        "echo-workflow-test-run",
		TaskQueue: taskQueue,
	}

	log.Printf("Starting workflow on task queue: %s", taskQueue)

	// Execute the workflow
	we, err := c.ExecuteWorkflow(ctx, workflowOptions, "EchoWorkflow", input)
	if err != nil {
		log.Fatalf("Failed to start workflow: %v", err)
	}

	log.Printf("Started workflow execution. WorkflowID: %s, RunID: %s", we.GetID(), we.GetRunID())

	// Wait for workflow completion
	var result interface{}
	err = we.Get(ctx, &result)
	if err != nil {
		log.Fatalf("Workflow execution failed: %v", err)
	}

	// Print the result
	fmt.Println("Workflow result:")
	fmt.Printf("%+v\n", result)

	log.Println("Workflow execution completed successfully")
}
