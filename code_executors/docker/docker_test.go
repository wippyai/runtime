package docker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestExecutor_Execute(t *testing.T) {
	t.Skip("not ready yet")
	// Create a test logger
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer func() {
		_ = logger.Sync()
	}()

	tests := []struct {
		name    string
		cmd     string
		wantErr bool
	}{
		{
			name:    "simple echo command",
			cmd:     "echo 'hello world'",
			wantErr: false,
		},
		{
			name:    "multiple commands",
			cmd:     "echo 'hello' && ls -la",
			wantErr: false,
		},
		{
			name:    "invalid command",
			cmd:     "invalidcommand",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new executor
			executor, err := NewExecutor(logger)
			require.NoError(t, err)

			// Create context with timeout
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			// Execute the command
			err = executor.Execute(ctx, tt.cmd)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Clean up
			assert.NoError(t, executor.Close(ctx))
		})
	}
}

func TestExecutor_ExecuteWithTimeout(t *testing.T) {
	t.Skip("not ready yet")
	// Create a test logger
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer func() {
		_ = logger.Sync()
	}()

	// Create a new executor
	executor, err := NewExecutor(logger)
	require.NoError(t, err)

	// Create context with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Execute a long-running command
	err = executor.Execute(ctx, "sleep 5")

	// Expect an error due to context timeout
	assert.Error(t, err)

	// Clean up
	assert.NoError(t, executor.Close(context.Background()))
}

func TestNewExecutor(t *testing.T) {
	t.Skip("not ready yet")
	logger, err := zap.NewDevelopment()
	require.NoError(t, err)
	defer func() {
		_ = logger.Sync()
	}()

	executor, err := NewExecutor(logger)
	assert.NoError(t, err)
	assert.NotNil(t, executor)
	assert.NotNil(t, executor.cli)
	assert.NotNil(t, executor.cc)

	// Clean up
	assert.NoError(t, executor.Close(context.Background()))
}
