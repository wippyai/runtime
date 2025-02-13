package async

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
)

func TestSend(t *testing.T) {
	t.Run("successful send", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		// Setup context
		ctx := context.Background()
		tg := engine.NewTaskGroup(10)
		ctx = engine.WithTaskGroup(ctx, tg)
		ctx = NewAsyncLayer(nil, 4096).WithContext(ctx)

		L.SetContext(ctx)

		// Spawn a test channel and value
		ch := channel.Named("test", 0)
		value := lua.LString("test")

		// send value
		_ = Send(L, ch, value, true)

		// Verify async was sent by checking the async channel
		_, asyncCh, _ := getContext(ctx)
		select {
		case schedule := <-asyncCh:
			assert.Equal(t, ch, schedule.ch)
			assert.Equal(t, value, schedule.value)
			assert.True(t, schedule.ok)
		default:
			t.Error("expected async to be sent")
		}
	})

	t.Run("send with invalid context", func(*testing.T) {
		L := lua.NewState()
		defer L.Close()

		// Setup invalid context
		ctx := context.Background()
		L.SetContext(ctx)

		ch := channel.Named("test", 0)
		value := lua.LString("test")

		// send should not panic but silently fail
		_ = Send(L, ch, value, true)
	})
}
