package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// LuaDataInput defines the input structure for the ProcessData Lua activity
type LuaDataInput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// LuaDataResult defines the expected result structure from the ProcessData Lua activity
type LuaDataResult struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

// ProcessDataWorkflow executes the Lua activity registered as "ProcessData"
func ProcessDataWorkflow(ctx workflow.Context, input LuaDataInput) (*LuaDataResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("ProcessDataWorkflow started", "input", input)

	// Define activity options
	options := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		TaskQueue:           "simple-task-queue-2", // Match the task queue in _index.yaml
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	// Execute the Lua activity by its registered name
	var result LuaDataResult
	err := workflow.ExecuteActivity(ctx, "ProcessData", input).Get(ctx, &result)
	if err != nil {
		logger.Error("Failed to execute ProcessData activity", "error", err)
		return nil, err
	}

	logger.Info("ProcessData activity completed", "result", result)
	return &result, nil
}

func main() {
	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-signalChan
		log.Println("Shutdown signal received, gracefully shutting down...")
		cancel()
		// Give some time for cleanup before forcing exit
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()

	// Create the client
	c, err := client.Dial(client.Options{})
	if err != nil {
		log.Fatalln("Unable to create Temporal client:", err)
	}
	defer c.Close()

	// Define task queues
	workerTaskQueue := "simple-task-queue"
	luaTaskQueue := "simple-task-queue-2"

	log.Printf("Starting worker with task queue: %s", workerTaskQueue)
	log.Printf("Configured to call Lua activity on task queue: %s", luaTaskQueue)

	// Use WaitGroup to coordinate shutdown
	var wg sync.WaitGroup
	wg.Add(1)

	// Start worker for our Go workflow
	go func() {
		defer wg.Done()
		startWorker(c, workerTaskQueue, ctx.Done())
	}()

	// Start massive parallel workflow execution
	log.Println("Starting 700,000 workflow executions in batches of 5...")
	startMassiveWorkflows(c, workerTaskQueue, ctx)

	// Wait for worker to complete before exiting
	wg.Wait()
	log.Println("Application shutdown complete")
}

func startWorker(c client.Client, taskQueue string, stopCh <-chan struct{}) {
	w := worker.New(c, taskQueue, worker.Options{})

	// Register only the Lua workflow
	w.RegisterWorkflow(ProcessDataWorkflow)

	log.Printf("Worker registered and running with task queue: %s", taskQueue)

	// Use the worker.InterruptCh() but close it when our stopCh is triggered
	// This ensures type compatibility with what w.Run expects
	interruptCh := worker.InterruptCh()
	go func() {
		<-stopCh
		// We can't close interruptCh directly since it's created by worker.InterruptCh()
		// But we can trigger our own exit
		w.Stop()
	}()

	err := w.Run(interruptCh)
	if err != nil {
		log.Printf("Worker stopped with error: %v", err)
	} else {
		log.Println("Worker stopped gracefully")
	}
}

func startMassiveWorkflows(c client.Client, taskQueue string, ctx context.Context) {
	const totalRuns = 7000000
	const batchSize = 500
	const logFrequency = 1000 // Log progress every 1000 batches

	// Rate limiting - don't start more than this many workflows per second
	const maxStartRate = 300 // Adjust based on your system capacity
	rateLimiter := time.Tick(time.Second / time.Duration(maxStartRate) * time.Duration(batchSize))

	var totalCompleted int64
	var totalFailed int64
	var completedMutex sync.Mutex

	startTime := time.Now()

	for batch := 0; batch < totalRuns/batchSize; batch++ {
		select {
		case <-ctx.Done():
			log.Println("Context cancelled, stopping workflow execution")
			return
		case <-rateLimiter:
			// Rate limiting applied
		}

		var batchWg sync.WaitGroup
		batchWg.Add(batchSize)

		// Start batch of workflows concurrently
		for i := 0; i < batchSize; i++ {
			go func(index int) {
				defer batchWg.Done()

				batchID := batch*batchSize + index
				workflowID := fmt.Sprintf("process-data-workflow-%d", batchID)

				// Create input for the Lua activity
				input := LuaDataInput{
					ID:   fmt.Sprintf("test-%d", batchID),
					Name: fmt.Sprintf("Test Data Item %d", batchID),
				}

				workflowOptions := client.StartWorkflowOptions{
					ID:        workflowID,
					TaskQueue: taskQueue,
				}

				we, err := c.ExecuteWorkflow(context.Background(), workflowOptions, ProcessDataWorkflow, input)
				if err != nil {
					log.Printf("Failed to start workflow %s: %v", workflowID, err)

					completedMutex.Lock()
					totalFailed++
					completedMutex.Unlock()
					return
				}

				// Wait for workflow to complete with timeout
				resultCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				var result LuaDataResult
				err = we.Get(resultCtx, &result)

				completedMutex.Lock()
				if err != nil {
					totalFailed++
				} else {
					totalCompleted++
				}

				// Log progress periodically
				currentBatch := batch + 1
				if index == 0 && currentBatch%logFrequency == 0 {
					elapsedTime := time.Since(startTime)
					progress := float64(currentBatch*batchSize) / float64(totalRuns) * 100

					log.Printf("Progress: %.2f%% (%d/%d) - Completed: %d, Failed: %d, Elapsed: %v",
						progress, currentBatch*batchSize, totalRuns, totalCompleted, totalFailed, elapsedTime)
				}
				completedMutex.Unlock()
			}(i)
		}

		// Wait for all workflows in this batch to complete
		batchWg.Wait()
	}

	totalTime := time.Since(startTime)
	log.Printf("All workflow executions finished - Completed: %d, Failed: %d, Total time: %v",
		totalCompleted, totalFailed, totalTime)
}
