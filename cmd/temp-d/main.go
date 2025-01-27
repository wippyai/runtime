package main

import (
	"context"
	"go.temporal.io/sdk/activity"
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

// SimpleActivity is our activity implementation
func SimpleActivity(ctx context.Context, input ActivityInput) (string, error) {
	return "Processed: " + input.Message, nil
}

// SimpleWorkflow is our workflow implementation
func SimpleWorkflow(ctx workflow.Context, input ActivityInput) (string, error) {
	// Workflow options
	options := workflow.ActivityOptions{
		StartToCloseTimeout: time.Second * 5,
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	// Define our activity result
	var result string

	// Execute activity
	err := workflow.ExecuteActivity(ctx, SimpleActivity, input).Get(ctx, &result)
	if err != nil {
		return "", err
	}

	return result, nil
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
	w := worker.New(c, "simple-task-queue", worker.Options{})

	// Register workflow and activity
	w.RegisterWorkflow(SimpleWorkflow)
	w.RegisterActivityWithOptions(SimpleActivity, activity.RegisterOptions{
		Name: "SimpleActivity",
	})

	go func() {
		// Start worker
		err = w.Run(worker.InterruptCh())
		if err != nil {
			log.Fatalln("Unable to start worker", err)
		}

	}()

	// Note: In a real application, you'd typically start the workflow in a separate process
	// Here's how you would start it:/*
	workflowOptions := client.StartWorkflowOptions{
		ID:        "simple-workflow",
		TaskQueue: "simple-task-queue",
	}
	input := ActivityInput{Message: "Hello, Temporal!"}

	we, err := c.ExecuteWorkflow(context.Background(), workflowOptions, SimpleWorkflow, input)
	if err != nil {
		log.Fatalln("Unable to execute workflow", err)
	}

	var result string
	err = we.Get(context.Background(), &result)
	if err != nil {
		log.Fatalln("Unable to get workflow result", err)
	}

	log.Printf("Workflow result: %s\n", result)

	time.Sleep(10 * time.Second)
}
