package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

const (
	workflowQueue = "workflow-queue"
	activityQueue = "test-queue"
)

var (
	hostPort  = flag.String("host", "localhost:7233", "Temporal server host:port")
	namespace = flag.String("namespace", "default", "Temporal namespace")
	mode      = flag.String("mode", "run", "Mode: run (execute workflow), worker (start workflow worker only)")
	input     = flag.String("input", "test-from-go", "Input name for the workflow")
)

// SimpleWorkflow executes a Lua activity registered by wippy
func SimpleWorkflow(ctx workflow.Context, name string) (map[string]interface{}, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		TaskQueue:           activityQueue,
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var result map[string]interface{}
	err := workflow.ExecuteActivity(ctx, "app.test.temporal:process_data", map[string]interface{}{
		"id":   fmt.Sprintf("wf-%d", workflow.Now(ctx).Unix()),
		"name": name,
	}).Get(ctx, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// EchoWorkflow tests the echo activity
func EchoWorkflow(ctx workflow.Context, data map[string]interface{}) (map[string]interface{}, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		TaskQueue:           activityQueue,
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var result map[string]interface{}
	err := workflow.ExecuteActivity(ctx, "app.test.temporal:echo_activity", data).Get(ctx, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
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

	switch *mode {
	case "worker":
		runWorkerOnly(c)
	case "run":
		runWorkflow(c)
	default:
		log.Fatalf("Unknown mode: %s (use 'run' or 'worker')", *mode)
	}
}

func runWorkerOnly(c client.Client) {
	w := worker.New(c, workflowQueue, worker.Options{})
	w.RegisterWorkflow(SimpleWorkflow)
	w.RegisterWorkflow(EchoWorkflow)

	log.Printf("Starting workflow worker on queue: %s", workflowQueue)
	log.Println("Activities will be handled by wippy on queue:", activityQueue)
	log.Println("Press Ctrl+C to stop")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := w.Run(worker.InterruptCh()); err != nil {
			log.Fatalf("Worker failed: %v", err)
		}
	}()

	<-sigCh
	log.Println("Shutting down...")
	w.Stop()
}

func runWorkflow(c client.Client) {
	w := worker.New(c, workflowQueue, worker.Options{})
	w.RegisterWorkflow(SimpleWorkflow)
	w.RegisterWorkflow(EchoWorkflow)

	log.Printf("Starting workflow worker on queue: %s", workflowQueue)
	go func() {
		if err := w.Run(worker.InterruptCh()); err != nil {
			log.Fatalf("Worker failed: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	workflowID := fmt.Sprintf("temporal-test-%s", time.Now().Format("20060102-150405"))
	log.Printf("Executing workflow: %s", workflowID)
	log.Printf("Activity queue: %s (handled by wippy)", activityQueue)

	we, err := c.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: workflowQueue,
	}, SimpleWorkflow, *input)
	if err != nil {
		log.Fatalf("Failed to execute workflow: %v", err)
	}

	log.Printf("Workflow started - ID: %s, RunID: %s", we.GetID(), we.GetRunID())

	var result map[string]interface{}
	if err := we.Get(context.Background(), &result); err != nil {
		log.Fatalf("Workflow failed: %v", err)
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	log.Printf("Workflow completed:\n%s", string(resultJSON))

	w.Stop()
}
