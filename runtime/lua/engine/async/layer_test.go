package async

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
	"time"
)

// after is a simple async function that sends a value after a delay
func after(L *lua.LState) int {
	if err := ValidateContext(L); err != nil {
		L.RaiseError("after: %s", err)
		return 0
	}

	delay := L.CheckInt(1)
	if delay <= 0 {
		L.RaiseError("delay must be positive")
		return 0
	}

	ch := channel.Named("timer", 1)
	go func() {
		select {
		case <-time.After(time.Duration(delay) * time.Millisecond):
			Send(L, ch, lua.LTrue, true)
			Send(L, ch, lua.LNil, false)
		case <-L.Context().Done():
			return
		}
	}()

	L.Push(channel.Wrap(L, ch))
	return 1
}

func TestAsyncLayer(t *testing.T) {
	logger := zap.NewNop()

	t.Run("basic after operation", func(t *testing.T) {
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
			engine.WithGlobalFunction("after", after),
		)
		require.NoError(t, err)
		defer vm.Close()

		tg := engine.NewTaskGroup(100)
		ctx := engine.WithTaskGroup(context.Background(), tg)
		ctx = WithAsyncChannel(ctx)
		vm.SetContext(ctx)

		script := `
            function test()
                local ch = after(100)  -- wait 100ms
                local result = ch:receive()
                return result
            end
        `

		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := channel.NewChannelRunner()
		asyncRunner := NewAsyncRunner(channels)
		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(asyncRunner),
			engine.WithLayer(channels),
		)

		start := time.Now()
		result, err := wrapped.Execute(ctx, "test")
		duration := time.Since(start)

		require.NoError(t, err)
		assert.Equal(t, lua.LTrue, result)
		assert.GreaterOrEqual(t, duration, 100*time.Millisecond)
	})

	t.Run("context cancellation", func(t *testing.T) {
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
			engine.WithGlobalFunction("after", after),
		)
		require.NoError(t, err)
		defer vm.Close()

		tg := engine.NewTaskGroup(100)
		ctx, cancel := context.WithCancel(context.Background())
		ctx = engine.WithTaskGroup(ctx, tg)
		ctx = WithAsyncChannel(ctx)
		vm.SetContext(ctx)

		script := `
            function test()
                local ch = after(1000)  -- wait 1s
                ch:receive()  -- should be interrupted
                return "not reached"
            end
        `

		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := channel.NewChannelRunner()
		asyncRunner := NewAsyncRunner(channels)
		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(asyncRunner),
			engine.WithLayer(channels),
		)

		// Cancel after a short delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		_, err = wrapped.Execute(ctx, "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})
}
