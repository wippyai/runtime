package tasker

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestTasker_BasicExecution(t *testing.T) {
	logger := zap.NewNop()

	t.Run("simple task execution", func(t *testing.T) {
		// Create base VM with tasks module
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded("tasks", NewModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

		// Import test script that will handle scheduled tasks
		script := `
            function test_handler()
				local inbox = tasks.channel()	

				while true do
					local task, ok = inbox:receive()
					if not ok then
						break	
					end	
					assert(task:input() == "hello")
					task:complete("world")
				end

				return "exit"
            end
        `
		err = vm.Import(script, "test", "test_handler")
		assert.NoError(t, err)

		// Create tasker with both layers
		tasker := NewTasker(
			logger,
			vm,
			channel.NewChannelLayer(),
			4096,
		)

		// Start the tasker
		statusCh, err := tasker.Start(context.Background(), "test_handler")
		assert.NoError(t, err)

		// First status should be "engine started"
		select {
		case status := <-statusCh:
			assert.Equal(t, "engine started", status)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for engine start")
		}

		// Execute a task
		resultCh, err := tasker.Execute(context.Background(), "test", []lua.LValue{lua.LString("hello")})
		assert.NoError(t, err)

		// Verify task result
		select {
		case result := <-resultCh:
			assert.NoError(t, result.Error)
			assert.Equal(t, 1, len(result.Result))
			assert.Equal(t, "world", result.Result[0].String())
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for task result")
		}

		// Stop the tasker
		err = tasker.Stop(context.Background())
		assert.NoError(t, err)

		// Verify final status
		select {
		case status := <-statusCh:
			assert.Contains(t, status.(string), "engine exit: exit")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for engine exit")
		}
	})
}
