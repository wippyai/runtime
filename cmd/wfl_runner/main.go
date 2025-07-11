package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
)

// LuaDataInput defines the input structure for the Lua workflow
type LuaDataInput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// LuaDataResult defines the expected result structure from the Lua workflow
type LuaDataResult struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

const (
	// Total number of workflows to execute
	totalWorkflows = 600000

	// Number of workflows to run in parallel
	parallelWorkflows = 500

	// Base task queue name
	baseTaskQueue = "simple-task-queue-2"

	// Progress report interval (in seconds)
	progressInterval = 5
)

func main() {
	startTime := time.Now()

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-signalChan
		log.Println("Shutdown signal received, canceling operations...")
		cancel()
	}()

	// Create the Temporal client
	c, err := client.Dial(client.Options{})
	if err != nil {
		cancel()
		log.Fatalln("Unable to create Temporal client:", err)
	}
	defer cancel()
	defer c.Close()

	// Channel to coordinate workflow execution
	workChan := make(chan int, totalWorkflows)

	// Wait group to wait for all goroutines to finish
	var wg sync.WaitGroup

	// Counters for progress tracking
	var completed, failed int64

	// Create a context for each batch of workflows
	batchCtx, batchCancel := context.WithCancel(ctx)
	defer batchCancel()

	// Start progress reporting goroutine
	go reportProgress(batchCtx, &completed, &failed, startTime)

	// Start worker goroutines
	log.Printf("Starting %d worker goroutines to execute %d workflows", parallelWorkflows, totalWorkflows)
	wg.Add(parallelWorkflows)
	for i := 0; i < parallelWorkflows; i++ {
		go func(workerID int) {
			defer wg.Done()
			executeWorkflows(batchCtx, c, workChan, workerID, &completed, &failed)
		}(i)
	}

	// Queue all workflow executions
	for i := 0; i < totalWorkflows; i++ {
		select {
		case workChan <- i:
			// Successfully queued
		case <-ctx.Done():
			log.Println("Cancellation received, stopping workflow queuing")
			close(workChan)
			wg.Wait()
			return
		}
	}

	// Close the channel to signal no more work
	close(workChan)

	// Wait for all workers to complete
	wg.Wait()

	// Final progress report
	duration := time.Since(startTime)
	log.Printf("Execution complete. Total: %d, Completed: %d, Failed: %d, Duration: %s",
		totalWorkflows, completed, failed, duration)

	successRate := float64(completed) / float64(totalWorkflows) * 100
	log.Printf("Success rate: %.2f%%, Average workflow/second: %.2f",
		successRate, float64(completed)/duration.Seconds())
}

// executeWorkflows processes workflows from the work channel
func executeWorkflows(ctx context.Context, c client.Client, workChan <-chan int,
	workerID int, completed, failed *int64) {
	for workID := range workChan {
		// Check if context is canceled
		select {
		case <-ctx.Done():
			log.Printf("Worker %d: Stopping due to cancellation", workerID)
			return
		default:
			// Continue execution
		}

		// Create unique workflow ID and input data
		workflowID := fmt.Sprintf("echo-workflow-%d", workID)

		// Task queue defined in the _index.yaml
		taskQueue := baseTaskQueue

		// Create input for the workflow
		input := LuaDataInput{
			ID:   fmt.Sprintf("test-%d", workID),
			Name: fmt.Sprintf("Test Data Item %d", workID),
		}

		// Options for starting the workflow
		workflowOptions := client.StartWorkflowOptions{
			ID:        workflowID,
			TaskQueue: taskQueue,
		}

		// Execute the workflow
		we, err := c.ExecuteWorkflow(ctx, workflowOptions, "EchoWorkflow", input)
		if err != nil {
			log.Printf("Worker %d: Failed to start workflow %s: %v", workerID, workflowID, err)
			atomic.AddInt64(failed, 1)
			continue
		}

		// Wait for workflow completion
		var result interface{}
		err = we.Get(ctx, &result)
		if err != nil {
			log.Printf("Worker %d: Workflow execution failed for %s: %v", workerID, workflowID, err)
			atomic.AddInt64(failed, 1)
		} else {
			atomic.AddInt64(completed, 1)
		}
	}
}

// reportProgress periodically reports progress of workflow execution
func reportProgress(ctx context.Context, completed, failed *int64, startTime time.Time) {
	ticker := time.NewTicker(progressInterval * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			completedCount := atomic.LoadInt64(completed)
			failedCount := atomic.LoadInt64(failed)
			totalProcessed := completedCount + failedCount

			// Calculate percentage and rate
			percentComplete := float64(totalProcessed) / float64(totalWorkflows) * 100
			duration := time.Since(startTime)
			rate := float64(totalProcessed) / duration.Seconds()

			// Estimate time remaining
			var remaining string
			if rate > 0 {
				remainingSecs := int64(float64(totalWorkflows-totalProcessed) / rate)
				remainingDuration := time.Duration(remainingSecs) * time.Second
				remaining = fmt.Sprintf(", Estimated time remaining: %s", remainingDuration.Round(time.Second))
			}

			log.Printf("Progress: %.2f%% (%d/%d) - Completed: %d, Failed: %d, Rate: %.2f workflows/sec%s",
				percentComplete, totalProcessed, totalWorkflows, completedCount, failedCount, rate, remaining)
		case <-ctx.Done():
			return
		}
	}
}
