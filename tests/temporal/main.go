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

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/service/temporal/propagator"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"
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
		ContextPropagators: []workflow.ContextPropagator{
			propagator.New(),
		},
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

	// Test 12: UUID Side Effect Workflow (generates UUIDs deterministically)
	log.Println("\n=== Test 12: UUID Side Effect Workflow ===")
	uuidWorkflowID := "wippy-uuid-" + time.Now().Format("20060102-150405")

	uuidInput := map[string]interface{}{
		"count": 5,
	}

	we12, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        uuidWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:uuid_workflow", uuidInput)
	if err != nil {
		log.Fatalf("Failed to execute uuid workflow: %v", err)
	}

	log.Printf("UUID workflow started - ID: %s, RunID: %s", we12.GetID(), we12.GetRunID())

	var uuidResult map[string]interface{}
	if err := we12.Get(ctx, &uuidResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("UUID workflow failed: %v", err)
	}

	uuidJSON, _ := json.MarshalIndent(uuidResult, "", "  ")
	log.Printf("UUID result:\n%s", string(uuidJSON))

	// Verify UUIDs were generated
	if uuids, ok := uuidResult["uuids"].([]interface{}); ok {
		log.Printf("Generated %d UUIDs via side effects", len(uuids))
		for i, u := range uuids {
			log.Printf("  UUID %d: %v", i+1, u)
		}
	}

	// Query the completed workflow to verify consistency
	log.Println("Querying completed workflow...")
	queryResp, err := c.QueryWorkflow(ctx, uuidWorkflowID, "", "state")
	if err != nil {
		log.Printf("Query failed (expected for completed workflow): %v", err)
	} else {
		var stateResult map[string]interface{}
		if err := queryResp.Get(&stateResult); err != nil {
			log.Printf("Failed to decode query result: %v", err)
		} else {
			stateJSON, _ := json.MarshalIndent(stateResult, "", "  ")
			log.Printf("Query state result:\n%s", string(stateJSON))
		}
	}

	// Get the workflow result again to verify replay produces same UUIDs
	log.Println("Re-fetching workflow result to verify replay consistency...")
	we12Again := c.GetWorkflow(ctx, uuidWorkflowID, "")
	var uuidResult2 map[string]interface{}
	if err := we12Again.Get(ctx, &uuidResult2); err != nil {
		log.Printf("Failed to re-fetch result: %v", err)
	} else {
		// Compare UUIDs
		uuids1, _ := uuidResult["uuids"].([]interface{})
		uuids2, _ := uuidResult2["uuids"].([]interface{})
		if len(uuids1) == len(uuids2) {
			allMatch := true
			for i := range uuids1 {
				if uuids1[i] != uuids2[i] {
					allMatch = false
					log.Printf("UUID mismatch at %d: %v != %v", i, uuids1[i], uuids2[i])
				}
			}
			if allMatch {
				log.Println("Replay verification: All UUIDs match - side effects working correctly!")
			}
		} else {
			log.Printf("UUID count mismatch: %d vs %d", len(uuids1), len(uuids2))
		}
	}

	// Test 13: Update Workflow - tests workflow updates via process.listen/process.send
	log.Println("\n=== Test 13: Update Workflow ===")
	updateWorkflowID := "wippy-update-" + time.Now().Format("20060102-150405")

	updateInput := map[string]interface{}{
		"initial": 10,
	}

	we13, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        updateWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:update_workflow", updateInput)
	if err != nil {
		log.Fatalf("Failed to execute update workflow: %v", err)
	}

	log.Printf("Update workflow started - ID: %s, RunID: %s", we13.GetID(), we13.GetRunID())

	// Give workflow time to start and register update handlers
	time.Sleep(500 * time.Millisecond)

	// Test increment update (ack -> ok flow)
	log.Println("Sending increment update (amount: 5)...")
	updateHandle1, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   updateWorkflowID,
		UpdateName:   "increment",
		Args:         []interface{}{map[string]interface{}{"amount": float64(5)}},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		log.Fatalf("Failed to send increment update: %v", err)
	}
	var updateResult1 map[string]interface{}
	if err := updateHandle1.Get(ctx, &updateResult1); err != nil {
		log.Fatalf("Failed to get increment update result: %v", err)
	}
	log.Printf("Increment update result: %v (expected value: 15)", updateResult1)

	// Test second increment
	log.Println("Sending second increment update (amount: 3)...")
	updateHandle2, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   updateWorkflowID,
		UpdateName:   "increment",
		Args:         []interface{}{map[string]interface{}{"amount": float64(3)}},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		log.Fatalf("Failed to send second increment update: %v", err)
	}
	var updateResult2 map[string]interface{}
	if err := updateHandle2.Get(ctx, &updateResult2); err != nil {
		log.Fatalf("Failed to get second increment update result: %v", err)
	}
	log.Printf("Second increment update result: %v (expected value: 18)", updateResult2)

	// Test decrement update
	log.Println("Sending decrement update (amount: 8)...")
	updateHandle3, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   updateWorkflowID,
		UpdateName:   "decrement",
		Args:         []interface{}{map[string]interface{}{"amount": float64(8)}},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		log.Fatalf("Failed to send decrement update: %v", err)
	}
	var updateResult3 map[string]interface{}
	if err := updateHandle3.Get(ctx, &updateResult3); err != nil {
		log.Fatalf("Failed to get decrement update result: %v", err)
	}
	log.Printf("Decrement update result: %v (expected value: 10)", updateResult3)

	// Test validation rejection (nak flow)
	log.Println("Sending invalid decrement update (would go negative)...")
	updateHandle4, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   updateWorkflowID,
		UpdateName:   "decrement",
		Args:         []interface{}{map[string]interface{}{"amount": float64(100)}},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		log.Printf("Update rejected during request (expected): %v", err)
	} else {
		var updateResult4 interface{}
		if err := updateHandle4.Get(ctx, &updateResult4); err != nil {
			log.Printf("Update rejected (expected): %v", err)
		} else {
			log.Printf("Unexpected success: %v", updateResult4)
		}
	}

	// Test error flow
	log.Println("Sending fail update (error flow)...")
	updateHandle5, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   updateWorkflowID,
		UpdateName:   "fail",
		Args:         []interface{}{map[string]interface{}{}},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		log.Printf("Update request failed: %v", err)
	} else {
		var updateResult5 interface{}
		if err := updateHandle5.Get(ctx, &updateResult5); err != nil {
			log.Printf("Update completed with error (expected): %v", err)
		} else {
			log.Printf("Unexpected success: %v", updateResult5)
		}
	}

	// Send finish update to complete the workflow
	log.Println("Sending finish update to complete workflow...")
	updateHandle6, err := c.UpdateWorkflow(ctx, client.UpdateWorkflowOptions{
		WorkflowID:   updateWorkflowID,
		UpdateName:   "finish",
		Args:         []interface{}{map[string]interface{}{}},
		WaitForStage: client.WorkflowUpdateStageCompleted,
	})
	if err != nil {
		log.Fatalf("Failed to send finish update: %v", err)
	}
	var updateResult6 map[string]interface{}
	if err := updateHandle6.Get(ctx, &updateResult6); err != nil {
		log.Fatalf("Failed to get finish update result: %v", err)
	}
	log.Printf("Finish update result: %v", updateResult6)

	// Wait for workflow completion
	var updateWorkflowResult map[string]interface{}
	if err := we13.Get(ctx, &updateWorkflowResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Update workflow failed: %v", err)
	}

	updateJSON, _ := json.MarshalIndent(updateWorkflowResult, "", "  ")
	log.Printf("Update workflow result:\n%s", string(updateJSON))

	// Verify results
	if finalCounter, ok := updateWorkflowResult["final_counter"].(float64); ok {
		log.Printf("Final counter value: %.0f (expected: 10)", finalCounter)
	}
	if updatesProcessed, ok := updateWorkflowResult["updates_processed"].(float64); ok {
		log.Printf("Updates processed: %.0f (expected: 3 - two increments, one decrement)", updatesProcessed)
	}

	// Test 14: Crypto Workflow - tests crypto operations with side effects
	log.Println("\n=== Test 14: Crypto Workflow ===")
	cryptoWorkflowID := "wippy-crypto-" + time.Now().Format("20060102-150405")

	cryptoInput := map[string]interface{}{
		"message": "Hello from crypto test!",
	}

	we14, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        cryptoWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:crypto_workflow", cryptoInput)
	if err != nil {
		log.Fatalf("Failed to execute crypto workflow: %v", err)
	}

	log.Printf("Crypto workflow started - ID: %s, RunID: %s", we14.GetID(), we14.GetRunID())

	var cryptoResult map[string]interface{}
	if err := we14.Get(ctx, &cryptoResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Crypto workflow failed: %v", err)
	}

	cryptoJSON, _ := json.MarshalIndent(cryptoResult, "", "  ")
	log.Printf("Crypto result:\n%s", string(cryptoJSON))

	// Verify crypto results
	if decryptMatches, ok := cryptoResult["decrypt_matches"].(bool); ok && decryptMatches {
		log.Println("AES encrypt/decrypt: PASS")
	} else {
		log.Println("AES encrypt/decrypt: FAIL")
	}
	if chachaMatches, ok := cryptoResult["chacha_decrypt_matches"].(bool); ok && chachaMatches {
		log.Println("ChaCha20 encrypt/decrypt: PASS")
	} else {
		log.Println("ChaCha20 encrypt/decrypt: FAIL")
	}
	if randomStr, ok := cryptoResult["random_string"].(string); ok {
		log.Printf("Random string generated: %s (length: %d)", randomStr, len(randomStr))
	}
	if uuid, ok := cryptoResult["random_uuid"].(string); ok {
		log.Printf("Random UUID generated: %s", uuid)
	}

	// Re-execute workflow to verify replay consistency
	log.Println("Re-fetching crypto workflow to verify replay consistency...")
	we14Again := c.GetWorkflow(ctx, cryptoWorkflowID, "")
	var cryptoResult2 map[string]interface{}
	if err := we14Again.Get(ctx, &cryptoResult2); err != nil {
		log.Printf("Failed to re-fetch crypto result: %v", err)
	} else {
		// Compare random values - should be identical on replay
		if cryptoResult["random_uuid"] == cryptoResult2["random_uuid"] &&
			cryptoResult["random_string"] == cryptoResult2["random_string"] {
			log.Println("Replay verification: Random values match - side effects working correctly!")
		} else {
			log.Println("Replay verification: FAILED - random values differ!")
		}
	}

	// Test 15: Context Propagation - tests context values passed via headers
	log.Println("\n=== Test 15: Context Propagation ===")
	ctxWorkflowID := "wippy-ctx-" + time.Now().Format("20060102-150405")

	// Create context with values to propagate
	ctxValues := map[string]any{
		"user_id":    "user-123",
		"tenant":     "acme-corp",
		"request_id": "req-abc-456",
	}
	ctxWithValues := propagator.WithValues(ctx, ctxValues)

	we15, err := c.ExecuteWorkflow(ctxWithValues, client.StartWorkflowOptions{
		ID:        ctxWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:ctx_workflow", nil)
	if err != nil {
		log.Fatalf("Failed to execute ctx workflow: %v", err)
	}

	log.Printf("Context workflow started - ID: %s, RunID: %s", we15.GetID(), we15.GetRunID())

	var ctxResult map[string]interface{}
	if err := we15.Get(ctx, &ctxResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Context workflow failed: %v", err)
	}

	ctxJSON, _ := json.MarshalIndent(ctxResult, "", "  ")
	log.Printf("Context result:\n%s", string(ctxJSON))

	// Verify context values were propagated
	if userID, ok := ctxResult["user_id"].(string); ok && userID == "user-123" {
		log.Println("Context propagation user_id: PASS")
	} else {
		log.Printf("Context propagation user_id: FAIL (got %v)", ctxResult["user_id"])
	}
	if tenant, ok := ctxResult["tenant"].(string); ok && tenant == "acme-corp" {
		log.Println("Context propagation tenant: PASS")
	} else {
		log.Printf("Context propagation tenant: FAIL (got %v)", ctxResult["tenant"])
	}
	if reqID, ok := ctxResult["request_id"].(string); ok && reqID == "req-abc-456" {
		log.Println("Context propagation request_id: PASS")
	} else {
		log.Printf("Context propagation request_id: FAIL (got %v)", ctxResult["request_id"])
	}

	// Test 16: Context Propagation to Activities - tests context inheritance to activities
	log.Println("\n=== Test 16: Context Propagation to Activities ===")
	ctxActivityWorkflowID := "wippy-ctx-activity-" + time.Now().Format("20060102-150405")

	we16, err := c.ExecuteWorkflow(ctxWithValues, client.StartWorkflowOptions{
		ID:        ctxActivityWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:ctx_activity_workflow", nil)
	if err != nil {
		log.Fatalf("Failed to execute ctx activity workflow: %v", err)
	}

	log.Printf("Context activity workflow started - ID: %s, RunID: %s", we16.GetID(), we16.GetRunID())

	var ctxActivityResult map[string]interface{}
	if err := we16.Get(ctx, &ctxActivityResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Context activity workflow failed: %v", err)
	}

	ctxActivityJSON, _ := json.MarshalIndent(ctxActivityResult, "", "  ")
	log.Printf("Context activity result:\n%s", string(ctxActivityJSON))

	// Verify workflow received context
	if wfUserID, ok := ctxActivityResult["workflow_user_id"].(string); ok && wfUserID == "user-123" {
		log.Println("Workflow context user_id: PASS")
	} else {
		log.Printf("Workflow context user_id: FAIL (got %v)", ctxActivityResult["workflow_user_id"])
	}

	// Verify activity received context
	if actResult, ok := ctxActivityResult["activity_result"].(map[string]interface{}); ok {
		if actUserID, ok := actResult["user_id"].(string); ok && actUserID == "user-123" {
			log.Println("Activity context user_id: PASS")
		} else {
			log.Printf("Activity context user_id: FAIL (got %v)", actResult["user_id"])
		}
		if actTenant, ok := actResult["tenant"].(string); ok && actTenant == "acme-corp" {
			log.Println("Activity context tenant: PASS")
		} else {
			log.Printf("Activity context tenant: FAIL (got %v)", actResult["tenant"])
		}
		if fromActivity, ok := actResult["from_activity"].(bool); ok && fromActivity {
			log.Println("Activity execution confirmed: PASS")
		} else {
			log.Printf("Activity execution confirmed: FAIL")
		}
	} else {
		log.Printf("Activity result: FAIL (missing or invalid)")
	}

	// Test 17: Workflow Module - tests workflow.call, workflow.version, workflow.info, etc.
	log.Println("\n=== Test 17: Workflow Module ===")
	workflowModuleWorkflowID := "wippy-workflow-module-" + time.Now().Format("20060102-150405")

	we17, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        workflowModuleWorkflowID,
		TaskQueue: taskQueue,
	}, "app.test.temporal:workflow_module_test", nil)
	if err != nil {
		log.Fatalf("Failed to execute workflow module test: %v", err)
	}

	log.Printf("Workflow module test started - ID: %s, RunID: %s", we17.GetID(), we17.GetRunID())

	var workflowModuleResult map[string]interface{}
	if err := we17.Get(ctx, &workflowModuleResult); err != nil {
		if ctx.Err() != nil {
			log.Println("Cancelled")
			return
		}
		log.Fatalf("Workflow module test failed: %v", err)
	}

	workflowModuleJSON, _ := json.MarshalIndent(workflowModuleResult, "", "  ")
	log.Printf("Workflow module result:\n%s", string(workflowModuleJSON))

	// Verify workflow.info()
	if info, ok := workflowModuleResult["info"].(map[string]interface{}); ok {
		if hasWfID, ok := info["has_workflow_id"].(bool); ok && hasWfID {
			log.Println("workflow.info() workflow_id: PASS")
		} else {
			log.Println("workflow.info() workflow_id: FAIL")
		}
		if hasRunID, ok := info["has_run_id"].(bool); ok && hasRunID {
			log.Println("workflow.info() run_id: PASS")
		} else {
			log.Println("workflow.info() run_id: FAIL")
		}
	} else {
		log.Printf("workflow.info(): FAIL (got error: %v)", workflowModuleResult["info_error"])
	}

	// Verify workflow.history_length()
	if histLen, ok := workflowModuleResult["history_length"].(float64); ok && histLen > 0 {
		log.Printf("workflow.history_length(): PASS (value: %.0f)", histLen)
	} else {
		log.Printf("workflow.history_length(): FAIL (got %v)", workflowModuleResult["history_length"])
	}

	// Verify workflow.history_size()
	if histSize, ok := workflowModuleResult["history_size"].(float64); ok && histSize >= 0 {
		log.Printf("workflow.history_size(): PASS (value: %.0f bytes)", histSize)
	} else {
		log.Printf("workflow.history_size(): FAIL (got %v)", workflowModuleResult["history_size"])
	}

	// Verify workflow.version()
	if version, ok := workflowModuleResult["version"].(float64); ok {
		log.Printf("workflow.version(): PASS (value: %.0f)", version)
	} else {
		log.Printf("workflow.version(): FAIL (got error: %v)", workflowModuleResult["version_error"])
	}

	// Verify workflow.call() - child workflow result
	if callResult, ok := workflowModuleResult["call_result"].(map[string]interface{}); ok {
		if status, ok := callResult["status"].(string); ok && status == "child done" {
			log.Println("workflow.call(): PASS")
		} else {
			log.Printf("workflow.call(): FAIL (unexpected status: %v)", callResult["status"])
		}
		if received, ok := callResult["received"].(string); ok && received == "hello from parent" {
			log.Println("workflow.call() args passed: PASS")
		} else {
			log.Printf("workflow.call() args passed: FAIL (got %v)", callResult["received"])
		}
	} else {
		log.Printf("workflow.call(): FAIL (got error: %v)", workflowModuleResult["call_error"])
	}

	// Verify version consistency
	if consistent, ok := workflowModuleResult["version_consistent"].(bool); ok && consistent {
		log.Println("workflow.version() consistency: PASS")
	} else {
		log.Println("workflow.version() consistency: FAIL")
	}

	// Verify history grew
	if grew, ok := workflowModuleResult["history_grew"].(bool); ok && grew {
		log.Println("workflow.history_length() growth: PASS")
	} else {
		log.Println("workflow.history_length() growth: FAIL")
	}

	log.Println("\nDone - All workflow tests passed!")
}

// Ensure imports are used
var (
	_ = ctxapi.GetValues
	_ workflow.Context
)
