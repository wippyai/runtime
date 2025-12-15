package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

const taskQueue = "test-queue"

var (
	hostPort  = flag.String("host", "localhost:7233", "Temporal server host:port")
	namespace = flag.String("namespace", "default", "Temporal namespace")
)

// WippyEchoWorkflow calls the wippy echo_activity
func WippyEchoWorkflow(ctx workflow.Context, data map[string]interface{}) (map[string]interface{}, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var result map[string]interface{}
	// Call wippy activity by its full registry name
	err := workflow.ExecuteActivity(ctx, "app.test.temporal:echo_activity", data).Get(ctx, &result)
	return result, err
}

// WippyProcessDataWorkflow calls the wippy process_data activity
func WippyProcessDataWorkflow(ctx workflow.Context, data map[string]interface{}) (map[string]interface{}, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var result map[string]interface{}
	// Call wippy activity by its full registry name
	err := workflow.ExecuteActivity(ctx, "app.test.temporal:process_data", data).Get(ctx, &result)
	return result, err
}

func main() {
	flag.Parse()

	// Setup cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("\nReceived interrupt, cancelling...")
		cancel()
	}()

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

	// Create worker for workflows only (activities are handled by wippy)
	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(WippyEchoWorkflow)
	w.RegisterWorkflow(WippyProcessDataWorkflow)

	log.Printf("Starting workflow worker on queue: %s", taskQueue)
	log.Println("Note: Activities should be registered by wippy runtime")

	go func() {
		if err := w.Run(worker.InterruptCh()); err != nil {
			log.Printf("Worker stopped: %v", err)
		}
	}()

	time.Sleep(500 * time.Millisecond)

	// Test 1: Echo workflow
	log.Println("\n=== Test 1: Echo Activity ===")
	echoWorkflowID := "wippy-echo-" + time.Now().Format("20060102-150405")

	echoInput := map[string]interface{}{
		"message": "Hello from test client",
		"count":   42,
	}

	we, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        echoWorkflowID,
		TaskQueue: taskQueue,
	}, WippyEchoWorkflow, echoInput)
	if err != nil {
		log.Fatalf("Failed to execute echo workflow: %v", err)
	}

	log.Printf("Echo workflow started - ID: %s, RunID: %s", we.GetID(), we.GetRunID())

	var echoResult map[string]interface{}
	if err := we.Get(ctx, &echoResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			w.Stop()
			return
		}
		log.Fatalf("Echo workflow failed: %v", err)
	}

	echoJSON, _ := json.MarshalIndent(echoResult, "", "  ")
	log.Printf("Echo result:\n%s", string(echoJSON))

	// Test 2: ProcessData workflow
	log.Println("\n=== Test 2: ProcessData Activity ===")
	processWorkflowID := "wippy-process-" + time.Now().Format("20060102-150405")

	processInput := map[string]interface{}{
		"id":   "test-123",
		"name": "Test User",
	}

	we2, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        processWorkflowID,
		TaskQueue: taskQueue,
	}, WippyProcessDataWorkflow, processInput)
	if err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			w.Stop()
			return
		}
		log.Fatalf("Failed to execute process workflow: %v", err)
	}

	log.Printf("Process workflow started - ID: %s, RunID: %s", we2.GetID(), we2.GetRunID())

	var processResult map[string]interface{}
	if err := we2.Get(ctx, &processResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			w.Stop()
			return
		}
		log.Fatalf("Process workflow failed: %v", err)
	}

	processJSON, _ := json.MarshalIndent(processResult, "", "  ")
	log.Printf("Process result:\n%s", string(processJSON))

	w.Stop()
	log.Println("\nDone - all tests passed!")
}
