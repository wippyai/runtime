package supervisor

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/supervisor"
	sysprocess "github.com/wippyai/runtime/system/process"

	"github.com/stretchr/testify/assert"
)

func TestIsTerminalError(t *testing.T) {
	t.Run("nil is not terminal", func(t *testing.T) {
		assert.False(t, isTerminalError(nil))
	})

	t.Run("context.Canceled is terminal", func(t *testing.T) {
		assert.True(t, isTerminalError(context.Canceled))
	})

	t.Run("supervisor ErrTerminated is terminal", func(t *testing.T) {
		assert.True(t, isTerminalError(supervisor.ErrTerminated))
	})

	t.Run("supervisor ErrExit is terminal", func(t *testing.T) {
		assert.True(t, isTerminalError(supervisor.ErrExit))
	})

	t.Run("plain error is not terminal", func(t *testing.T) {
		assert.False(t, isTerminalError(errors.New("something broke")))
	})

	t.Run("wrapped plain error is not terminal", func(t *testing.T) {
		assert.False(t, isTerminalError(fmt.Errorf("process failed: %w", errors.New("crash"))))
	})

	t.Run("process ErrTerminated is not terminal", func(t *testing.T) {
		// Process termination is an external signal, not a permanent condition.
		// The supervisor should restart the service when desired=running.
		assert.False(t, isTerminalError(sysprocess.ErrTerminated))
	})

	t.Run("wrapped process ErrTerminated is not terminal", func(t *testing.T) {
		// This is the exact error shape that arrives from monitorLoop
		wrapped := fmt.Errorf("process failed: %w", sysprocess.ErrTerminated)
		assert.False(t, isTerminalError(wrapped))
	})
}
