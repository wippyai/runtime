package activity_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// TestActivityExecution_Integration tests that activities can be executed.
// This is a basic integration test using Temporal's test server.
// For full wippy integration, run wippy with the app/src/test/temporal config.
func TestActivityExecution_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create test server
	server, err := testsuite.StartDevServer(context.Background(), testsuite.DevServerOptions{
		LogLevel: "error",
	})
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	// Create client
	c := server.Client()
	defer c.Close()

	// Create worker
	taskQueue := "test-activity-queue"
	w := worker.New(c, taskQueue, worker.Options{})

	// Register a simple test activity
	w.RegisterActivity(simpleActivity)
	w.RegisterWorkflow(simpleWorkflow)

	// Start worker
	require.NoError(t, w.Start())
	defer w.Stop()

	// Execute workflow
	workflowOptions := client.StartWorkflowOptions{
		ID:        "test-activity-" + time.Now().Format("20060102-150405"),
		TaskQueue: taskQueue,
	}

	we, err := c.ExecuteWorkflow(context.Background(), workflowOptions, simpleWorkflow, "test-input")
	require.NoError(t, err)

	var result string
	err = we.Get(context.Background(), &result)
	require.NoError(t, err)
	require.Equal(t, "processed: test-input", result)
}

func simpleWorkflow(ctx workflow.Context, input string) (string, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var result string
	err := workflow.ExecuteActivity(ctx, simpleActivity, input).Get(ctx, &result)
	if err != nil {
		return "", err
	}
	return result, nil
}

func simpleActivity(_ context.Context, input string) (string, error) {
	return "processed: " + input, nil
}
