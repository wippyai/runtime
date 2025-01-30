package main

import (
	"context"
	"go.temporal.io/sdk/workflow"
	"log"
	"time"

	"go.temporal.io/sdk/client"
)

// StabWorkflow is a simple workflow that executes the stab activity
func StabWorkflow(ctx workflow.Context) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Stab workflow started")

	// Configure activity options
	ao := workflow.ActivityOptions{
		TaskQueue:           "wippy_demos",
		StartToCloseTimeout: time.Second * 5,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Execute the stab activity
	err := workflow.ExecuteActivity(ctx, "stab-activity").Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to execute stab activity", "error", err)
		return err
	}

	logger.Info("Stab workflow completed successfully")
	return nil
}

func main() {
	// Create temporal client
	c, err := client.Dial(client.Options{
		HostPort: "localhost:7233",
	})
	if err != nil {
		log.Fatalln("Unable to create client", err)
	}
	defer c.Close()

	// Configure workflow options
	options := client.StartWorkflowOptions{
		ID:                 "stab-workflow-" + time.Now().Format("2006-01-02-15-04-05"),
		TaskQueue:          "wippy_demos_wf",
		WorkflowRunTimeout: time.Minute,
	}

	// Execute workflow
	we, err := c.ExecuteWorkflow(context.Background(), options, StabWorkflow)
	if err != nil {
		log.Fatalln("Unable to execute workflow", err)
	}

	// Wait for workflow completion
	err = we.Get(context.Background(), nil)
	if err != nil {
		log.Fatalln("Unable to get workflow result", err)
	}

	log.Printf("Workflow completed! WorkflowID: %s RunID: %s\n", we.GetID(), we.GetRunID())
}
