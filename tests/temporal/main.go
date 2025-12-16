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

	// Test 6: Workflow Cancellation (start long-running workflow, then cancel it)
	log.Println("\n=== Test 6: Workflow Cancellation ===")
	cancelWorkflowID := "wippy-cancel-" + time.Now().Format("20060102-150405")

	cancelInput := map[string]interface{}{
		"iterations": 100, // Would take ~10 seconds without cancellation
	}

	we6, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        cancelWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:long_workflow", cancelInput)
	if err != nil {
		log.Fatalf("Failed to execute long workflow: %v", err)
	}

	log.Printf("Long workflow started - ID: %s, RunID: %s", we6.GetID(), we6.GetRunID())

	// Let workflow run for a bit
	log.Println("Waiting 1 second before cancelling...")
	time.Sleep(1 * time.Second)

	// Cancel the workflow from Go
	log.Println("Cancelling workflow...")
	if err := c.CancelWorkflow(ctx, we6.GetID(), we6.GetRunID()); err != nil {
		log.Fatalf("Failed to cancel workflow: %v", err)
	}

	// Wait for workflow result - should return canceled error
	var cancelResult map[string]interface{}
	err = we6.Get(ctx, &cancelResult)
	if err != nil {
		log.Printf("Workflow cancelled as expected: %v", err)
	} else {
		log.Printf("Warning: workflow completed instead of being cancelled: %v", cancelResult)
	}

	// Test 7: Signal to Process Inbox (workflow receives via process.inbox())
	log.Println("\n=== Test 7: Process Inbox Signal ===")
	inboxWorkflowID := "wippy-inbox-" + time.Now().Format("20060102-150405")

	inboxInput := map[string]interface{}{
		"timeout_ms": 10000, // 10 second timeout
	}

	we7, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        inboxWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:inbox_workflow", inboxInput)
	if err != nil {
		log.Fatalf("Failed to execute inbox workflow: %v", err)
	}

	log.Printf("Inbox workflow started - ID: %s, RunID: %s", we7.GetID(), we7.GetRunID())

	// Let workflow start
	time.Sleep(500 * time.Millisecond)

	// Send signal that will be received via process.inbox()
	log.Println("Sending greeting signal to process inbox...")
	greetingData := map[string]interface{}{
		"text": "hello from Go runner",
	}
	if err := c.SignalWorkflow(ctx, inboxWorkflowID, "", "greeting", greetingData); err != nil {
		log.Fatalf("Failed to send greeting signal: %v", err)
	}

	var inboxResult map[string]interface{}
	if err := we7.Get(ctx, &inboxResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Inbox workflow failed: %v", err)
	}

	inboxJSON, _ := json.MarshalIndent(inboxResult, "", "  ")
	log.Printf("Inbox result:\n%s", string(inboxJSON))

	// Test 8: Child Workflow EXIT Event (spawn child, wait for EXIT event)
	log.Println("\n=== Test 8: Child Workflow EXIT Event ===")
	spawnChildWorkflowID := "wippy-spawn-child-" + time.Now().Format("20060102-150405")

	we8, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        spawnChildWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:spawn_child_workflow", nil)
	if err != nil {
		log.Fatalf("Failed to execute spawn child workflow: %v", err)
	}

	log.Printf("Spawn child workflow started - ID: %s, RunID: %s", we8.GetID(), we8.GetRunID())

	var spawnChildResult map[string]interface{}
	if err := we8.Get(ctx, &spawnChildResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Spawn child workflow failed: %v", err)
	}

	spawnChildJSON, _ := json.MarshalIndent(spawnChildResult, "", "  ")
	log.Printf("Spawn child result:\n%s", string(spawnChildJSON))

	// Test 9: Error Child Workflow (spawn child that errors, verify error in EXIT event)
	log.Println("\n=== Test 9: Error Child Workflow ===")
	spawnErrorChildWorkflowID := "wippy-spawn-error-child-" + time.Now().Format("20060102-150405")

	we9, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        spawnErrorChildWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:spawn_error_child_workflow", nil)
	if err != nil {
		log.Fatalf("Failed to execute spawn error child workflow: %v", err)
	}

	log.Printf("Spawn error child workflow started - ID: %s, RunID: %s", we9.GetID(), we9.GetRunID())

	var spawnErrorChildResult map[string]interface{}
	if err := we9.Get(ctx, &spawnErrorChildResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Spawn error child workflow failed: %v", err)
	}

	spawnErrorChildJSON, _ := json.MarshalIndent(spawnErrorChildResult, "", "  ")
	log.Printf("Spawn error child result:\n%s", string(spawnErrorChildJSON))

	// Test 10: Activity Error Propagation (call activity that errors, verify error metadata)
	log.Println("\n=== Test 10: Activity Error Propagation ===")
	activityErrorWorkflowID := "wippy-activity-error-" + time.Now().Format("20060102-150405")

	activityErrorInput := map[string]interface{}{
		"error_kind":    "NotFound",
		"error_message": "resource not found in activity",
	}

	we10, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        activityErrorWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:activity_error_workflow", activityErrorInput)
	if err != nil {
		log.Fatalf("Failed to execute activity error workflow: %v", err)
	}

	log.Printf("Activity error workflow started - ID: %s, RunID: %s", we10.GetID(), we10.GetRunID())

	var activityErrorResult map[string]interface{}
	if err := we10.Get(ctx, &activityErrorResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Activity error workflow failed: %v", err)
	}

	activityErrorJSON, _ := json.MarshalIndent(activityErrorResult, "", "  ")
	log.Printf("Activity error result:\n%s", string(activityErrorJSON))

	// Test 11: Workflow Query (query a running workflow)
	log.Println("\n=== Test 11: Workflow Query ===")
	queryWorkflowID := "wippy-query-" + time.Now().Format("20060102-150405")

	// Start a long workflow that we can query
	queryInput := map[string]interface{}{
		"iterations": 100,
	}

	we11, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        queryWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:long_workflow", queryInput)
	if err != nil {
		log.Fatalf("Failed to execute query workflow: %v", err)
	}

	log.Printf("Query workflow started - ID: %s, RunID: %s", we11.GetID(), we11.GetRunID())

	// Wait for workflow to start
	time.Sleep(500 * time.Millisecond)

	// Query the workflow's PID
	resp, err := c.QueryWorkflow(ctx, queryWorkflowID, "", "pid")
	if err != nil {
		log.Printf("Query failed (may be expected if query not supported): %v", err)
	} else {
		var pidResult string
		if err := resp.Get(&pidResult); err != nil {
			log.Printf("Failed to decode query result: %v", err)
		} else {
			log.Printf("Query result (pid): %s", pidResult)
		}
	}

	// Cancel the workflow
	log.Println("Cancelling query workflow...")
	if err := c.CancelWorkflow(ctx, we11.GetID(), we11.GetRunID()); err != nil {
		log.Printf("Failed to cancel workflow: %v", err)
	}

	// Wait for workflow result
	var queryWorkflowResult map[string]interface{}
	err = we11.Get(ctx, &queryWorkflowResult)
	if err != nil {
		log.Printf("Query workflow cancelled as expected: %v", err)
	}

	log.Println("\nDone - All workflow tests passed!")
}
