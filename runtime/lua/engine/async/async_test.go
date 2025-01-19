package async

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"testing"
)

func TestAsyncContext(t *testing.T) {
	t.Run("WithAsyncChannel", func(t *testing.T) {
		ctx := context.Background()
		asyncCtx := WithAsyncChannel(ctx)

		ch := getAsyncChannel(asyncCtx)
		assert.NotNil(t, ch)
		assert.Equal(t, 4096, cap(ch))
	})

	t.Run("getAsyncChannel with invalid context", func(t *testing.T) {
		ctx := context.Background()
		ch := getAsyncChannel(ctx)
		assert.Nil(t, ch)
	})
}

func TestValidateContext(t *testing.T) {
	t.Run("valid context", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		// Setup valid context with both TaskGroup and AsyncChannel
		ctx := context.Background()
		tg := engine.NewTaskGroup(10)
		ctx = engine.WithTaskGroup(ctx, tg)
		ctx = WithAsyncChannel(ctx)

		L.SetContext(ctx)

		err := ValidateContext(L)
		assert.NoError(t, err)
	})

	t.Run("missing task group", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		// Setup context with only AsyncChannel
		ctx := context.Background()
		ctx = WithAsyncChannel(ctx)

		L.SetContext(ctx)

		err := ValidateContext(L)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot send from non-task context")
	})

	t.Run("missing async channel", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		// Setup context with only TaskGroup
		ctx := context.Background()
		tg := engine.NewTaskGroup(10)
		ctx = engine.WithTaskGroup(ctx, tg)

		L.SetContext(ctx)

		err := ValidateContext(L)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot send from non-task context")
	})
}

func TestSend(t *testing.T) {
	t.Run("successful send", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		// Setup context
		ctx := context.Background()
		tg := engine.NewTaskGroup(10)
		ctx = engine.WithTaskGroup(ctx, tg)
		ctx = WithAsyncChannel(ctx)

		L.SetContext(ctx)

		// Create test channel and value
		ch := channel.Named("test", 0)
		value := lua.LString("test")

		// Send value
		Send(L, ch, value, true)

		// Verify schedule was sent by checking the async channel
		asyncCh := getAsyncChannel(ctx)
		select {
		case schedule := <-asyncCh:
			assert.Equal(t, ch, schedule.ch)
			assert.Equal(t, value, schedule.value)
			assert.True(t, schedule.ok)
		default:
			t.Error("expected schedule to be sent")
		}
	})

	t.Run("send with invalid context", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		// Setup invalid context
		ctx := context.Background()
		L.SetContext(ctx)

		ch := channel.Named("test", 0)
		value := lua.LString("test")

		// Send should not panic but silently fail
		Send(L, ch, value, true)
	})

	t.Run("send with full channel", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		// Setup context
		ctx := context.Background()
		tg := engine.NewTaskGroup(10)
		ctx = engine.WithTaskGroup(ctx, tg)
		ctx = WithAsyncChannel(ctx)

		L.SetContext(ctx)

		ch := channel.Named("test", 0)
		value := lua.LString("test")

		// Fill up the channel
		asyncCh := getAsyncChannel(ctx)
		for i := 0; i < cap(asyncCh); i++ {
			asyncCh <- schedule{ch: ch, value: value, ok: true}
		}

		// Send should not block or panic
		Send(L, ch, value, true)
	})
}
