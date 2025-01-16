package stream

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestAsyncStreamRead(t *testing.T) {
	t.Run("async stream reading", func(t *testing.T) {
		log := zap.NewNop()

		// Create base VM with stream module
		vm, err := engine.NewCVM(
			log,
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
			engine.WithPreloaded("stream", NewStreamModule(log).Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		// Create wrapped VM with async runner
		wrapped := engine.NewWrappedCVM(
			vm,
			engine.WithLayer(channel.NewChannelRunner()),
			engine.WithLayer(coroutine.NewCoroutineRunner()),
		)

		// Create test data and stream
		testData := []byte("chunk1chunk2chunk3")
		reader := newMockReadCloser(testData)
		stream, err := NewStream(context.Background(), reader, NewStreamConfig(6))
		require.NoError(t, err)

		// Register stream in Lua
		luaStream := &LuaStream{Stream: stream}
		ud := vm.State().NewUserData()
		ud.Value = luaStream
		vm.State().SetMetatable(ud, vm.State().GetTypeMetatable("Stream"))
		vm.State().SetGlobal("test_stream", ud)

		// Import test script with coroutines
		err = vm.Import(`
            function test_stream_read()
				local results = {}
				local sync1 = channel.new(1)
				local sync2 = channel.new(1)
			
				-- Main flow reads first
				local chunk = test_stream:read()
				results.first = chunk
				sync1:send(true) -- Signal first coroutine to start
			
				-- Start first coroutine
				coroutine.spawn(function()
					sync1:receive() -- Wait for main flow
					local chunk = test_stream:read()
					results.second = chunk
					sync2:send(true) -- Signal second coroutine
				end)
			
				-- Start second coroutine
				coroutine.spawn(function()
					sync2:receive() -- Wait for first coroutine
					local chunk = test_stream:read()
					results.third = chunk
				end)
			
				return results
			end
        `, "test", "test_stream_read")
		require.NoError(t, err)

		// Execute test and verify results
		result, err := wrapped.Execute(context.Background(), "test_stream_read")
		require.NoError(t, err)

		// Verify results
		resultTable := result.(*lua.LTable)
		first := resultTable.RawGetString("first").String()
		second := resultTable.RawGetString("second").String()
		third := resultTable.RawGetString("third").String()

		assert.Equal(t, "chunk1", first)
		assert.Equal(t, "chunk2", second)
		assert.Equal(t, "chunk3", third)
	})
}
