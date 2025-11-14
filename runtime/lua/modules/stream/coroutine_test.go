package stream

import (
	"context"
	"testing"

	"github.com/wippyai/runtime/runtime/lua/engine/value"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestAsyncStreamRead(t *testing.T) {
	t.Run("async stream reading", func(t *testing.T) {
		log, _ := zap.NewDevelopment()

		// Spawn base VM with stream module
		vm, err := engine.NewCVM(
			log,
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
			engine.WithPreloaded("stream", NewStreamModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		// Spawn a wrapped VM with async runner
		wrapped := engine.NewRunner(
			vm,
			engine.WithLayer(channel.NewChannelLayer()),
			engine.WithLayer(coroutine.NewCoroutineLayer()),
		)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		// Spawn test data and stream
		testData := []byte("chunk1chunk2chunk3")
		reader := newMockReadCloser(testData)
		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		// Register stream in Lua
		luaStream := &LuaStream{Stream: stream}
		ud := vm.State().NewUserData()
		ud.Value = luaStream
		vm.State().SetMetatable(ud, value.GetTypeMetatable(nil, "Stream"))
		vm.State().SetGlobal("test_stream", ud)

		// Imports test script with coroutines
		err = vm.Import(`
            function test_stream_read()
				local results = {}
				local sync1 = channel.new(1)
				local sync2 = channel.new(1) 
				local done = channel.new(2) -- Track both coroutines completion
				
				-- Main flow reads first
				local chunk = test_stream:read(6)  -- Specify chunk size of 6
				results.first = chunk
				sync1:send(true)
				
				-- Both coroutines can run in parallel after first read
				coroutine.spawn(function()
					sync1:receive()
					local chunk = test_stream:read(6)  -- Specify chunk size of 6
					results.second = chunk
					sync2:send("next")
				end)
				
				coroutine.spawn(function()
					sync2:receive()
					local chunk = test_stream:read(6)  -- Specify chunk size of 6
					results.third = chunk
					done:send(true)
				end)
					
				done:receive()
				return results
			end
        `, "test", "test_stream_read")
		require.NoError(t, err)

		// execute test and verify results
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

func TestAsyncStreamIter(t *testing.T) {
	t.Run("async stream iteration", func(t *testing.T) {
		log := zap.NewNop()

		// Spawn base VM with stream module
		vm, err := engine.NewCVM(
			log,
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
			engine.WithPreloaded("stream", NewStreamModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		// Spawn wrapped VM with async runner
		wrapped := engine.NewRunner(
			vm,
			engine.WithLayer(channel.NewChannelLayer()),
			engine.WithLayer(coroutine.NewCoroutineLayer()),
		)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		// Spawn test data and stream
		testData := []byte("chunk1chunk2chunk3")
		reader := newMockReadCloser(testData)
		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		// Register stream in Lua
		luaStream := &LuaStream{Stream: stream}
		ud := vm.State().NewUserData()
		ud.Value = luaStream
		vm.State().SetMetatable(ud, value.GetTypeMetatable(nil, "Stream"))
		vm.State().SetGlobal("test_stream", ud)

		// Imports test script with coroutines
		err = vm.Import(`
            function test_stream_iter()
                local results = {}
                local sync = channel.new(1)
                
                -- First coroutine reads chunks with explicit chunk size of 6
                coroutine.spawn(function()
                    local chunks = {}
                    for chunk in test_stream(6) do
                        table.insert(chunks, chunk)
                    end
                    results.chunks = chunks
                    sync:send(true)
                end)

                -- wait for first coroutine to finish
                sync:receive()
                return results
            end
        `, "test", "test_stream_iter")
		require.NoError(t, err)

		// execute test and verify results
		result, err := wrapped.Execute(context.Background(), "test_stream_iter")
		require.NoError(t, err)

		// Verify results
		resultTable := result.(*lua.LTable)
		chunks := resultTable.RawGetString("chunks").(*lua.LTable)

		// Convert chunks from Lua table to Go slice
		var allChunks []string
		chunks.ForEach(func(_, value lua.LValue) {
			allChunks = append(allChunks, value.String())
		})

		assert.Equal(t, []string{"chunk1", "chunk2", "chunk3"}, allChunks)
	})
}
