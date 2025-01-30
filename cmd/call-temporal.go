package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"
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

	// Get the stab activity
	err := workflow.ExecuteActivity(ctx, "stab-activity").Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to execute stab activity", "error", err)
		return err
	}

	logger.Info("Stab workflow completed successfully")
	return nil
}

func executeWorkflow(c client.Client, wg *sync.WaitGroup, index int) {
	defer wg.Done()

	// Configure workflow options with unique ID
	options := client.StartWorkflowOptions{
		ID:                 fmt.Sprintf("stab-workflow-%d-%s", index, time.Now().Format("2006-01-02-15-04-05")),
		TaskQueue:          "wippy_demos_wf",
		WorkflowRunTimeout: time.Minute,
	}

	// Get workflow
	we, err := c.ExecuteWorkflow(context.Background(), options, StabWorkflow)
	if err != nil {
		log.Printf("Failed to execute workflow %d: %v\n", index, err)
		return
	}

	// Wait for workflow completion
	err = we.Get(context.Background(), nil)
	if err != nil {
		log.Printf("Workflow %d failed: %v\n", index, err)
		return
	}

	log.Printf("Workflow %d completed! WorkflowID: %s RunID: %s\n", index, we.GetID(), we.GetRunID())
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

	// Create wait group to track workflow completion
	var wg sync.WaitGroup

	// Launch 100 workflows in parallel
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go executeWorkflow(c, &wg, i)
	}

	// Wait for all workflows to complete
	wg.Wait()
	log.Println("All workflows completed!")
}
