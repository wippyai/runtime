package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// ActivityParams represents the input to the Lua activity
type ActivityParams struct {
	Input string `json:"input"`
}

// ActivityResult represents the output from the Lua activity
type ActivityResult struct {
	Output string `json:"output"`
}

// Simple workflow that executes the Lua activity
func SimpleWorkflow(ctx workflow.Context, input string) (string, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		TaskQueue:           "dev:test-queue",
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	// Call the Lua activity registered by wippy
	// Send data matching Lua activity expectations: {id, name}
	var result map[string]interface{}
	err := workflow.ExecuteActivity(ctx, "ProcessData", map[string]interface{}{
		"id":   "test-123",
		"name": input,
	}).Get(ctx, &result)
	if err != nil {
		return "", err
	}

	if message, ok := result["message"].(string); ok {
		return message, nil
	}
	return "success", nil
}

func main() {
	// Create Temporal client
	c, err := client.Dial(client.Options{
		HostPort:  "localhost:7233",
		Namespace: "default",
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	// Create worker ONLY for workflow on SEPARATE queue
	// Wippy worker handles activities on dev:test-queue
	w := worker.New(c, "dev:workflow-queue", worker.Options{})

	// Register only the workflow
	w.RegisterWorkflow(SimpleWorkflow)

	// Start worker in background
	log.Println("Starting Go worker for workflows on dev:workflow-queue...")
	go func() {
		err := w.Run(worker.InterruptCh())
		if err != nil {
			log.Fatalf("Worker failed: %v", err)
		}
	}()

	// Wait for worker to start
	time.Sleep(2 * time.Second)

	// Execute workflow on workflow queue (activities go to dev:test-queue)
	log.Println("Executing workflow that will call Lua activity...")
	workflowOptions := client.StartWorkflowOptions{
		ID:        "lua-activity-test-" + time.Now().Format("20060102-150405"),
		TaskQueue: "dev:workflow-queue",
	}

	we, err := c.ExecuteWorkflow(context.Background(), workflowOptions, SimpleWorkflow, "test-from-go")
	if err != nil {
		log.Fatalf("Failed to execute workflow: %v", err)
	}

	log.Printf("Started workflow ID: %s, RunID: %s", we.GetID(), we.GetRunID())

	// Wait for workflow to complete
	var result string
	err = we.Get(context.Background(), &result)
	if err != nil {
		log.Fatalf("Workflow failed: %v", err)
	}

	// Pretty print the result
	resultJSON, _ := json.MarshalIndent(map[string]string{"result": result}, "", "  ")
	log.Printf("Workflow completed!\n%s", string(resultJSON))
	log.Println("SUCCESS: Lua activity invoked from Go workflow!")

	// Stop worker
	w.Stop()
}
