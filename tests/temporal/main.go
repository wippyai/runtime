package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

const taskQueue = "test-queue"

var (
	hostPort  = flag.String("host", "localhost:7233", "Temporal server host:port")
	namespace = flag.String("namespace", "default", "Temporal namespace")
)

// ProcessDataInput is the input for ProcessData activity
type ProcessDataInput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProcessDataOutput is the output from ProcessData activity
type ProcessDataOutput struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

// ProcessData activity processes incoming data
func ProcessData(ctx context.Context, input ProcessDataInput) (*ProcessDataOutput, error) {
	info := activity.GetInfo(ctx)
	log.Printf("[Activity] ProcessData executing: id=%s, name=%s (attempt %d)",
		input.ID, input.Name, info.Attempt)

	return &ProcessDataOutput{
		Message: fmt.Sprintf("Processed: id=%s, name=%s", input.ID, input.Name),
		Status:  "success",
	}, nil
}

// EchoActivity returns whatever was sent to it
func EchoActivity(ctx context.Context, data map[string]interface{}) (map[string]interface{}, error) {
	log.Printf("[Activity] Echo: %v", data)
	return data, nil
}

// SimpleWorkflow executes ProcessData activity
func SimpleWorkflow(ctx workflow.Context, name string) (*ProcessDataOutput, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var result ProcessDataOutput
	err := workflow.ExecuteActivity(ctx, ProcessData, ProcessDataInput{
		ID:   fmt.Sprintf("wf-%d", workflow.Now(ctx).Unix()),
		Name: name,
	}).Get(ctx, &result)

	return &result, err
}

// EchoWorkflow executes EchoActivity
func EchoWorkflow(ctx workflow.Context, data map[string]interface{}) (map[string]interface{}, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var result map[string]interface{}
	err := workflow.ExecuteActivity(ctx, EchoActivity, data).Get(ctx, &result)
	return result, err
}

func main() {
	flag.Parse()

	log.Printf("Connecting to Temporal at %s (namespace: %s)", *hostPort, *namespace)

	c, err := client.Dial(client.Options{
		HostPort:  *hostPort,
		Namespace: *namespace,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer c.Close()

	log.Println("Connected to Temporal")

	// Create and start worker
	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(SimpleWorkflow)
	w.RegisterWorkflow(EchoWorkflow)
	w.RegisterActivity(ProcessData)
	w.RegisterActivity(EchoActivity)

	log.Printf("Starting worker on queue: %s", taskQueue)
	go func() {
		if err := w.Run(worker.InterruptCh()); err != nil {
			log.Fatalf("Worker failed: %v", err)
		}
	}()

	time.Sleep(500 * time.Millisecond)

	// Run test workflow
	workflowID := fmt.Sprintf("test-%s", time.Now().Format("20060102-150405"))
	log.Printf("Starting workflow: %s", workflowID)

	we, err := c.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: taskQueue,
	}, SimpleWorkflow, "test-input")
	if err != nil {
		log.Fatalf("Failed to execute workflow: %v", err)
	}

	log.Printf("Workflow started - ID: %s, RunID: %s", we.GetID(), we.GetRunID())

	var result ProcessDataOutput
	if err := we.Get(context.Background(), &result); err != nil {
		log.Fatalf("Workflow failed: %v", err)
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	log.Printf("Workflow completed:\n%s", string(resultJSON))

	w.Stop()
	log.Println("Done")
}
