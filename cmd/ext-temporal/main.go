package main

import (
	"log"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
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

	// Execute the stab activity
	err := workflow.ExecuteActivity(ctx, "hello_world.activity", "hello world").Get(ctx, nil)
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

	// Create worker
	w := worker.New(c, "wippy_demos_wf", worker.Options{})

	// Register workflow
	w.RegisterWorkflow(StabWorkflow)

	// Start worker
	err = w.Run(worker.InterruptCh())
	if err != nil {
		log.Fatalln("Unable to start worker", err)
	}
}
