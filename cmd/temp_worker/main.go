package main

import (
	"context"
	"log"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// SimpleActivity is a simple activity that returns a greeting
func SimpleActivity(ctx context.Context, name string) (string, error) {
	log.Printf("SimpleActivity executed with name: %s", name)
	return "Hello, " + name + "!", nil
}

// SimpleWorkflow is a simple workflow that executes an activity
func SimpleWorkflow(ctx workflow.Context, name string) (string, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var result string
	err := workflow.ExecuteActivity(ctx, SimpleActivity, name).Get(ctx, &result)
	if err != nil {
		return "", err
	}

	// Introduce a 5-second delay
	workflow.Sleep(ctx, 5*time.Second)
	log.Printf("Workflow completed after 5s delay")

	return result, nil
}

func main() {
	// Create the client
	c, err := client.Dial(client.Options{})
	if err != nil {
		log.Fatalln("Unable to create Temporal client:", err)
	}
	defer c.Close()

	// Start worker
	go startWorker(c)

	// Start workflow
	startWorkflow(c)
}

func startWorker(c client.Client) {
	w := worker.New(c, "simple-task-queue", worker.Options{})
	w.RegisterWorkflow(SimpleWorkflow)
	w.RegisterActivity(SimpleActivity)

	err := w.Run(worker.InterruptCh())
	if err != nil {
		log.Fatalln("Unable to start worker:", err)
	}
}

func startWorkflow(c client.Client) {
	workflowOptions := client.StartWorkflowOptions{
		ID:        "simple-workflow",
		TaskQueue: "simple-task-queue",
	}

	we, err := c.ExecuteWorkflow(context.Background(), workflowOptions, SimpleWorkflow, "Temporal")
	if err != nil {
		log.Fatalln("Unable to execute workflow:", err)
	}

	log.Println("Started workflow", "WorkflowID", we.GetID(), "RunID", we.GetRunID())

	// Wait for workflow completion
	var result string
	err = we.Get(context.Background(), &result)
	if err != nil {
		log.Fatalln("Unable to get workflow result:", err)
	}
	log.Println("Workflow result:", result)
}
