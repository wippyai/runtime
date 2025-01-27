package pubsub

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"sync"
	"testing"
	"time"
)

func setupTestVM(t *testing.T) (*engine.CoroutineVM, *channel.Layer, *Layer, *engine.Runner) {
	logger := zap.NewNop()

	// Create VM with required modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("pubsub", NewModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	require.NoError(t, err)

	// Setup layers
	channels := channel.NewChannelLayer()
	pubsubLayer := NewSubscriptionLayer(channels)

	// Create runner
	runner := engine.NewRunner(vm,
		engine.WithLayer(channels),
		engine.WithLayer(pubsubLayer),
	)

	return vm, channels, pubsubLayer, runner
}

func TestPubSub(t *testing.T) {
	t.Run("single subscriber basic flow", func(t *testing.T) {
		vm, _, pubsubLayer, runner := setupTestVM(t)
		defer vm.Close()

		script := `
	       function test()
	           local sub = pubsub.subscribe("test-topic")
	           local msg = sub:receive()
	           return msg
	       end
	   `
		err := vm.Import(script, "test", "test")
		require.NoError(t, err)

		ctx := runner.WithContext(context.Background())

		var wg sync.WaitGroup
		wg.Add(1)
		var result interface{}

		go func() {
			defer wg.Done()

			exitCh, err := runner.Start(ctx, "test")
			if err != nil {
				result = err
				return
			}

			res, err := runner.Run(ctx, exitCh)
			if err != nil {
				result = err
				return
			}
			result = res
		}()

		time.Sleep(100 * time.Millisecond) // Let subscriber setup
		pubsubLayer.Publish("test-topic", lua.LString("hello"))

		wg.Wait()
		assert.Equal(t, "hello", result.(lua.LValue).String())
	})

	t.Run("prevent duplicate topic subscription", func(t *testing.T) {
		vm, _, _, runner := setupTestVM(t)
		defer vm.Close()

		script := `
	       function test()
	           local sub1 = pubsub.subscribe("test-topic")
	           local sub2 = pubsub.subscribe("test-topic") -- should fail
	           return "shouldn't reach here"
	       end
	   `
		err := vm.Import(script, "test", "test")
		require.NoError(t, err)

		ctx := runner.WithContext(context.Background())
		exitCh, err := runner.Start(ctx, "test")
		require.NoError(t, err)

		_, err = runner.Run(ctx, exitCh)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already has an active subscription")
	})

	t.Run("unsubscribe flow", func(t *testing.T) {
		vm, _, _, runner := setupTestVM(t)
		defer vm.Close()

		script := `
	       function test()
	           local sub = pubsub.subscribe("test-topic")
	           local ok = pubsub.unsubscribe(sub)
	           -- Verify unsubscribe
	           return ok
	       end
	   `
		err := vm.Import(script, "test", "test")
		require.NoError(t, err)

		ctx := runner.WithContext(context.Background())
		exitCh, err := runner.Start(ctx, "test")
		require.NoError(t, err)

		result, err := runner.Run(ctx, exitCh)
		require.NoError(t, err)
		assert.Equal(t, lua.LTrue, result)
	})

	t.Run("invalid unsubscribe", func(t *testing.T) {
		vm, _, _, runner := setupTestVM(t)
		defer vm.Close()

		script := `
	       function test()
	           local ch = channel.new()
	           pubsub.unsubscribe(ch)
	       end
	   `
		err := vm.Import(script, "test", "test")
		require.NoError(t, err)

		ctx := runner.WithContext(context.Background())
		exitCh, err := runner.Start(ctx, "test")
		require.NoError(t, err)

		_, err = runner.Run(ctx, exitCh)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "channel not found in subscriptions")
	})

	t.Run("multiple messages in order", func(t *testing.T) {
		vm, _, pubsubLayer, runner := setupTestVM(t)
		defer vm.Close()

		script := `
	       function test()
	           local sub = pubsub.subscribe("test-topic")
	           local results = {}
	           results[1] = sub:receive()
	           results[2] = sub:receive()
	           results[3] = sub:receive()
	           return table.concat(results, ",")
	       end
	   `
		err := vm.Import(script, "test", "test")
		require.NoError(t, err)

		ctx := runner.WithContext(context.Background())

		var wg sync.WaitGroup
		wg.Add(1)
		var result interface{}

		go func() {
			defer wg.Done()

			exitCh, err := runner.Start(ctx, "test")
			if err != nil {
				result = err
				return
			}

			res, err := runner.Run(ctx, exitCh)
			if err != nil {
				result = err
				return
			}
			result = res
		}()

		pubsubLayer.Publish("test-topic", lua.LString("one"))
		pubsubLayer.Publish("test-topic", lua.LString("two"))
		pubsubLayer.Publish("test-topic", lua.LString("three"))

		wg.Wait()
		assert.Equal(t, "one,two,three", result.(lua.LValue).String())
	})
}
