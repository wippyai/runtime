package pubsub

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestBasicPubSub(t *testing.T) {
	logger := zap.NewNop()

	t.Run("single subscriber", func(t *testing.T) {
		// Create VM with required modules
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("pubsub", NewModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		// Import test script
		script := `
            function test()
                local sub = pubsub.subscribe("test-topic")
                local msg, ok = sub:receive()
                return msg
            end
        `
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		// Setup layers - channels must be initialized first
		channels := channel.NewChannelLayer()
		pubsubLayer := NewSubscriptionLayer(channels)

		// Create runner with layers - pubsub must be above channels to intercept yields
		runner := engine.NewRunner(vm,
			engine.WithLayer(pubsubLayer), // Process subscriptions first
			engine.WithLayer(channels),    // Then handle channel operations
		)

		// Start execution
		ctx := context.Background()
		ctx = engine.WithTaskGroup(ctx, runner.GetTaskGroup())

		// Execute in background
		doneCh := make(chan interface{})
		go func() {
			result, err := runner.Execute(ctx, "test")
			require.NoError(t, err)
			doneCh <- result
		}()

		// Wait a bit and publish message
		pubsubLayer.Publish("test-topic", lua.LString("hello"))

		// Verify result
		select {
		case result := <-doneCh:
			assert.Equal(t, "hello", result.(lua.LValue).String())
		}
	})
}
