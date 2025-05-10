package main

import (
	"context"
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

// SimpleActivity is a simple activity that returns a greeting (kept for backward compatibility)
func SimpleActivity(ctx context.Context, name string) (string, error) {
	log.Printf("SimpleActivity executed with name: %s", name)
	return "Hello, " + name + "!", nil
}

// SimpleWorkflow is a simple workflow that executes an activity (kept for backward compatibility)
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

	workflow.Sleep(ctx, 5*time.Second)
	log.Printf("Workflow completed after 5s delay")

	return result, nil
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

	// Start workflow that calls the Lua activity
	startWorkflow(c, workerTaskQueue)

	// Wait for worker to complete before exiting
	wg.Wait()
	log.Println("Application shutdown complete")
}

func startWorker(c client.Client, taskQueue string, stopCh <-chan struct{}) {
	w := worker.New(c, taskQueue, worker.Options{})

	// Register both workflows for compatibility
	w.RegisterWorkflow(ProcessDataWorkflow)
	w.RegisterWorkflow(SimpleWorkflow)
	w.RegisterActivity(SimpleActivity)

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

func startWorkflow(c client.Client, taskQueue string) {
	// Create input for the Lua activity
	input := LuaDataInput{
		ID:   "test-123",
		Name: "Test Data Item",
	}

	workflowOptions := client.StartWorkflowOptions{
		ID:        "process-data-workflow",
		TaskQueue: taskQueue,
	}

	log.Printf("Starting workflow on task queue: %s", taskQueue)
	we, err := c.ExecuteWorkflow(context.Background(), workflowOptions, ProcessDataWorkflow, input)
	if err != nil {
		log.Fatalln("Unable to execute workflow:", err)
	}

	log.Printf("Started workflow to call Lua activity, WorkflowID: %s, RunID: %s", we.GetID(), we.GetRunID())

	// Set a timeout for workflow completion
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Wait for workflow completion
	var result LuaDataResult
	err = we.Get(ctx, &result)
	if err != nil {
		log.Printf("Unable to get workflow result: %v", err)
		log.Println("Workflow may still be running, but we won't wait longer")
	} else {
		log.Printf("Workflow completed. Lua activity result: %+v", result)
	}

	log.Printf("Task queue used for worker: %s", taskQueue)
	log.Printf("Task queue used for Lua activity: simple-task-queue-2")
}
