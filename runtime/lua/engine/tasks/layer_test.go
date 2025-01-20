package tasks

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

func TestTasksSingleExecution(t *testing.T) {
	logger := zap.NewNop()

	t.Run("simple task execution", func(t *testing.T) {
		// Create base VM with tasks module
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded("tasks", NewModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		assert.NoError(t, err)

		// Create channel runner and task runner
		channelRunner := channel.NewChannelRunner()
		taskMixer := NewMixer(channelRunner, 10)

		// Create wrapped VM with both layers
		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(channelRunner),
			engine.WithLayer(taskMixer),
		)

		// Import test script that will handle scheduled tasks
		script := `
            function test_handler()
				local inbox = tasks.channel()	

				while true do
					local task, ok = inbox:receive()
					if not ok then
						break	
					end	
					task:complete("world")
				end 
				return "exit"
            end
        `
		err = vm.Import(script, "test", "test_handler")
		assert.NoError(t, err)

		// Set up task group context
		ctx, cancel := context.WithCancel(engine.WithTaskGroup(context.Background(), wrapped.GetTaskGroup()))
		defer cancel()

		done := make(chan struct{}, 1)

		// Start execution
		go func() {
			result, err := wrapped.Execute(ctx, "test_handler")
			assert.NoError(t, err)
			if result != nil {
				assert.Equal(t, "exit", result.String())
			} else {
				t.Fatal("no result")
			}
			done <- struct{}{}
		}()

		// send task
		out, err := taskMixer.Send(ctx, "test", lua.LString("hello"))
		assert.NoError(t, err)

		select {
		case result := <-out:
			err = taskMixer.CloseOutbox(ctx)
			assert.NoError(t, err)
			assert.Equal(t, "world", result.Values[0].String())
			select {
			case <-done:
			case <-time.After(1 * time.Second):
				cancel()
				t.Fatal("timeout on close")
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timeout")
		}
	})
}
