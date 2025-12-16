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
)

const taskQueue = "test-queue"

var (
	hostPort  = flag.String("host", "localhost:7233", "Temporal server host:port")
	namespace = flag.String("namespace", "default", "Temporal namespace")
)

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

	// Test 1: Lua Hello Workflow (handled entirely by wippy - no Go worker needed)
	log.Println("\n=== Test 1: Lua Hello Workflow ===")
	helloWorkflowID := "wippy-hello-" + time.Now().Format("20060102-150405")

	helloInput := map[string]interface{}{
		"name": "Temporal",
	}

	// Call the Lua workflow by its registered name (wippy worker handles this)
	we, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        helloWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:hello_workflow", helloInput)
	if err != nil {
		log.Fatalf("Failed to execute hello workflow: %v", err)
	}

	log.Printf("Hello workflow started - ID: %s, RunID: %s", we.GetID(), we.GetRunID())

	var helloResult map[string]interface{}
	if err := we.Get(ctx, &helloResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Hello workflow failed: %v", err)
	}

	helloJSON, _ := json.MarshalIndent(helloResult, "", "  ")
	log.Printf("Hello result:\n%s", string(helloJSON))

	// Test 2: Concurrent Workflow (coroutines, channels, timers)
	log.Println("\n=== Test 2: Concurrent Workflow ===")
	concurrentWorkflowID := "wippy-concurrent-" + time.Now().Format("20060102-150405")

	concurrentInput := map[string]interface{}{
		"workers": 3,
		"jobs":    6,
	}

	we2, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        concurrentWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:concurrent_workflow", concurrentInput)
	if err != nil {
		log.Fatalf("Failed to execute concurrent workflow: %v", err)
	}

	log.Printf("Concurrent workflow started - ID: %s, RunID: %s", we2.GetID(), we2.GetRunID())

	var concurrentResult map[string]interface{}
	if err := we2.Get(ctx, &concurrentResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Concurrent workflow failed: %v", err)
	}

	concurrentJSON, _ := json.MarshalIndent(concurrentResult, "", "  ")
	log.Printf("Concurrent result:\n%s", string(concurrentJSON))

	// Test 3: Timed Workflow (time progression with multiple sleeps)
	log.Println("\n=== Test 3: Timed Workflow ===")
	timedWorkflowID := "wippy-timed-" + time.Now().Format("20060102-150405")

	timedInput := map[string]interface{}{
		"steps":    3,
		"delay_ms": 1000, // Temporal minimum timer resolution is 1 second
	}

	we3, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        timedWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:timed_workflow", timedInput)
	if err != nil {
		log.Fatalf("Failed to execute timed workflow: %v", err)
	}

	log.Printf("Timed workflow started - ID: %s, RunID: %s", we3.GetID(), we3.GetRunID())

	var timedResult map[string]interface{}
	if err := we3.Get(ctx, &timedResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Timed workflow failed: %v", err)
	}

	timedJSON, _ := json.MarshalIndent(timedResult, "", "  ")
	log.Printf("Timed result:\n%s", string(timedJSON))

	// Test 4: Activity Workflow (calls activity via funcs.call)
	log.Println("\n=== Test 4: Activity Workflow ===")
	activityWorkflowID := "wippy-activity-" + time.Now().Format("20060102-150405")

	activityInput := map[string]interface{}{
		"name": "ActivityTest",
	}

	we4, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        activityWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:activity_workflow", activityInput)
	if err != nil {
		log.Fatalf("Failed to execute activity workflow: %v", err)
	}

	log.Printf("Activity workflow started - ID: %s, RunID: %s", we4.GetID(), we4.GetRunID())

	var activityResult map[string]interface{}
	if err := we4.Get(ctx, &activityResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Activity workflow failed: %v", err)
	}

	activityJSON, _ := json.MarshalIndent(activityResult, "", "  ")
	log.Printf("Activity result:\n%s", string(activityJSON))

	// Test 5: Signal Workflow (receives signals, calls activities, returns results)
	log.Println("\n=== Test 5: Signal Workflow ===")
	signalWorkflowID := "wippy-signal-" + time.Now().Format("20060102-150405")

	signalInput := map[string]interface{}{
		"name": "SignalTest",
	}

	we5, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        signalWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:signal_workflow", signalInput)
	if err != nil {
		log.Fatalf("Failed to execute signal workflow: %v", err)
	}

	log.Printf("Signal workflow started - ID: %s, RunID: %s", we5.GetID(), we5.GetRunID())

	// Give workflow time to start and subscribe
	time.Sleep(500 * time.Millisecond)

	// Send job signals
	for i := 1; i <= 3; i++ {
		jobData := map[string]interface{}{
			"id":   i,
			"task": fmt.Sprintf("job-%d", i),
		}
		log.Printf("Sending add_job signal #%d", i)
		if err := c.SignalWorkflow(ctx, signalWorkflowID, "", "add_job", jobData); err != nil {
			log.Fatalf("Failed to send add_job signal: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Send exit signal
	log.Println("Sending exit signal")
	if err := c.SignalWorkflow(ctx, signalWorkflowID, "", "exit", nil); err != nil {
		log.Fatalf("Failed to send exit signal: %v", err)
	}

	var signalResult map[string]interface{}
	if err := we5.Get(ctx, &signalResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Signal workflow failed: %v", err)
	}

	signalJSON, _ := json.MarshalIndent(signalResult, "", "  ")
	log.Printf("Signal result:\n%s", string(signalJSON))

	log.Println("\nDone - All workflow tests passed!")
}
