package channel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
)

// after is a simple async function that sends a value after a delay
func after(l *lua.LState) int {
	delay := l.CheckInt(1)
	if delay <= 0 {
		l.RaiseError("delay must be positive")
		return 0
	}

	ch := Named("timer", 1)
	go func() {
		select {
		case <-time.After(time.Duration(delay) * time.Millisecond):
			_ = Send(l, ch, lua.LTrue)
			_ = Close(l, ch)
		case <-l.Context().Done():
			return
		}
	}()

	l.Push(Wrap(l, ch))
	return 1
}

func TestAsyncLayer(t *testing.T) {
	logger := zap.NewNop()

	t.Run("basic after operation", func(t *testing.T) {
		vm, err := engine.NewCVM(
			logger,
			engine.WithPreloaded("channel", NewChannelModule().Loader),
			engine.WithGlobalFunction("after", after),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
	       function test()
	           local ch = after(100)  -- wait 100ms
	           local result = ch:receive()
	           return result
	       end
	   `

		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		channels := NewChannelLayer()
		runner := engine.NewRunner(vm, engine.WithLayer(channels))

		uw, ctx := runner.InitUnitOfWork(context.Background())
		defer func() { _ = uw.Close() }()

		start := time.Now()
		result, err := runner.Execute(ctx, "test")
		duration := time.Since(start)

		require.NoError(t, err)
		assert.Equal(t, lua.LTrue, result)
		assert.GreaterOrEqual(t, duration, 100*time.Millisecond)
	})
}
