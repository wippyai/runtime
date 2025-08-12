package stream

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestScanner(t *testing.T) {
	t.Run("basic line scanning", func(t *testing.T) {
		testData := []byte("line1\nline2\nline3")
		reader := newMockReadCloser(testData)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		scanner, err := NewScanner(stream, SplitLines)
		require.NoError(t, err)

		// First line
		assert.True(t, scanner.Scan())
		assert.Equal(t, "line1", scanner.Text())
		assert.NoError(t, scanner.Err())

		// Second line
		assert.True(t, scanner.Scan())
		assert.Equal(t, "line2", scanner.Text())
		assert.NoError(t, scanner.Err())

		// Third line
		assert.True(t, scanner.Scan())
		assert.Equal(t, "line3", scanner.Text())
		assert.NoError(t, scanner.Err())

		// EOF
		assert.False(t, scanner.Scan())
		assert.NoError(t, scanner.Err())
	})

	t.Run("word scanning", func(t *testing.T) {
		testData := []byte("hello world test")
		reader := newMockReadCloser(testData)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		scanner, err := NewScanner(stream, SplitWords)
		require.NoError(t, err)

		expected := []string{"hello", "world", "test"}
		var words []string

		for scanner.Scan() {
			words = append(words, scanner.Text())
		}

		assert.NoError(t, scanner.Err())
		assert.Equal(t, expected, words)
	})

	t.Run("byte scanning", func(t *testing.T) {
		testData := []byte("abc")
		reader := newMockReadCloser(testData)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		scanner, err := NewScanner(stream, SplitBytes)
		require.NoError(t, err)

		expected := []string{"a", "b", "c"}
		var bytes []string

		for scanner.Scan() {
			bytes = append(bytes, scanner.Text())
		}

		assert.NoError(t, scanner.Err())
		assert.Equal(t, expected, bytes)
	})

	t.Run("default split type", func(t *testing.T) {
		testData := []byte("line1\nline2")
		reader := newMockReadCloser(testData)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		scanner, err := NewScanner(stream) // No split type specified
		require.NoError(t, err)

		assert.Equal(t, SplitLines, scanner.SplitType())

		assert.True(t, scanner.Scan())
		assert.Equal(t, "line1", scanner.Text())

		assert.True(t, scanner.Scan())
		assert.Equal(t, "line2", scanner.Text())

		assert.False(t, scanner.Scan())
	})

	t.Run("nil stream", func(t *testing.T) {
		_, err := NewScanner(nil)
		assert.ErrorContains(t, err, "stream cannot be nil")
	})

	t.Run("invalid split type", func(t *testing.T) {
		testData := []byte("test")
		reader := newMockReadCloser(testData)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		_, err = NewScanner(stream, SplitType("invalid"))
		assert.ErrorContains(t, err, "unsupported split type")
	})
}

func TestScannerLua(t *testing.T) {
	logger := zap.NewNop()

	t.Run("Scanner line reading with mock reader", func(t *testing.T) {
		testData := []byte("line1\nline2\nline3")
		reader := newMockReadCloser(testData)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		mod := NewStreamModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		l := vm.State()
		RegisterStream(l)

		luaStream := &LuaStream{Stream: stream}
		ud := l.NewUserData()
		ud.Value = luaStream
		l.SetMetatable(ud, value.GetTypeMetatable(nil, "Stream"))
		l.SetGlobal("test_stream", ud)

		script := `
			-- Create scanner with default (lines) split
			local scanner = test_stream:scanner()
			
			local lines = {}
			while scanner:scan() do
				table.insert(lines, scanner:text())
			end
			
			-- Check for errors
			local err = scanner:err()
			assert(err == nil, "Scanner should not have errors")
			
			-- Verify lines
			assert(#lines == 3, "Expected 3 lines, got " .. #lines)
			assert(lines[1] == "line1", "Expected 'line1', got '" .. lines[1] .. "'")
			assert(lines[2] == "line2", "Expected 'line2', got '" .. lines[2] .. "'")
			assert(lines[3] == "line3", "Expected 'line3', got '" .. lines[3] .. "'")
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)
	})

	t.Run("Scanner with explicit split type", func(t *testing.T) {
		testData := []byte("hello world test")
		reader := newMockReadCloser(testData)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		mod := NewStreamModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		l := vm.State()
		RegisterStream(l)

		luaStream := &LuaStream{Stream: stream}
		ud := l.NewUserData()
		ud.Value = luaStream
		l.SetMetatable(ud, value.GetTypeMetatable(nil, "Stream"))
		l.SetGlobal("test_stream", ud)

		script := `
			-- Create scanner with words split
			local scanner = test_stream:scanner("words")
			
			local words = {}
			while scanner:scan() do
				table.insert(words, scanner:text())
			end
			
			-- Verify words
			assert(#words == 3, "Expected 3 words, got " .. #words)
			assert(words[1] == "hello", "Expected 'hello', got '" .. words[1] .. "'")
			assert(words[2] == "world", "Expected 'world', got '" .. words[2] .. "'")
			assert(words[3] == "test", "Expected 'test', got '" .. words[3] .. "'")
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)
	})

	t.Run("Scanner with invalid split type", func(t *testing.T) {
		testData := []byte("test")
		reader := newMockReadCloser(testData)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		mod := NewStreamModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		l := vm.State()
		RegisterStream(l)

		luaStream := &LuaStream{Stream: stream}
		ud := l.NewUserData()
		ud.Value = luaStream
		l.SetMetatable(ud, value.GetTypeMetatable(nil, "Stream"))
		l.SetGlobal("test_stream", ud)

		script := `
			-- This should raise an error
			local scanner = test_stream:scanner("invalid")
		`

		err = vm.DoString(context.Background(), script, "test")
		assert.ErrorContains(t, err, "unsupported split type")
	})
}

func TestAsyncScannerRead(t *testing.T) {
	t.Run("async scanner line reading", func(t *testing.T) {
		log := zap.NewNop()

		vm, err := engine.NewCVM(
			log,
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
			engine.WithPreloaded("stream", NewStreamModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		wrapped := engine.NewRunner(
			vm,
			engine.WithLayer(channel.NewChannelLayer()),
			engine.WithLayer(coroutine.NewCoroutineLayer()),
		)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		testData := []byte("first line\nsecond line\nthird line")
		reader := newMockReadCloser(testData)
		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		luaStream := &LuaStream{Stream: stream}
		ud := vm.State().NewUserData()
		ud.Value = luaStream
		vm.State().SetMetatable(ud, value.GetTypeMetatable(nil, "Stream"))
		vm.State().SetGlobal("test_stream", ud)

		err = vm.Import(`
            function test_scanner_async()
				local results = {}
				local done = channel.new(1)
				
				coroutine.spawn(function()
					local scanner = test_stream:scanner()
					local lines = {}
					
					while scanner:scan() do -- This will be async
						table.insert(lines, scanner:text())
					end
					
					results.lines = lines
					results.err = scanner:err()
					done:send(true)
				end)
				
				done:receive()
				return results
			end
        `, "test", "test_scanner_async")
		require.NoError(t, err)

		result, err := wrapped.Execute(context.Background(), "test_scanner_async")
		require.NoError(t, err)

		resultTable := result.(*lua.LTable)
		lines := resultTable.RawGetString("lines").(*lua.LTable)
		errorValue := resultTable.RawGetString("err")

		// Verify no error
		assert.Equal(t, lua.LNil, errorValue)

		// Convert lines from Lua table to Go slice
		var allLines []string
		lines.ForEach(func(_, value lua.LValue) {
			allLines = append(allLines, value.String())
		})

		assert.Equal(t, []string{"first line", "second line", "third line"}, allLines)
	})

	t.Run("async scanner with multiple coroutines", func(t *testing.T) {
		log := zap.NewNop()

		vm, err := engine.NewCVM(
			log,
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
			engine.WithPreloaded("stream", NewStreamModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		wrapped := engine.NewRunner(
			vm,
			engine.WithLayer(channel.NewChannelLayer()),
			engine.WithLayer(coroutine.NewCoroutineLayer()),
		)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		testData := []byte("line1\nline2\nline3")
		reader := newMockReadCloser(testData)
		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		luaStream := &LuaStream{Stream: stream}
		ud := vm.State().NewUserData()
		ud.Value = luaStream
		vm.State().SetMetatable(ud, value.GetTypeMetatable(nil, "Stream"))
		vm.State().SetGlobal("test_stream", ud)

		err = vm.Import(`
            function test_scanner_multi_async()
				local results = {}
				local sync = channel.new(1)
				local done = channel.new(1)
				
				-- First coroutine reads first line
				coroutine.spawn(function()
					local scanner = test_stream:scanner()
					
					if scanner:scan() then -- Async scan
						results.first = scanner:text()
					end
					
					sync:send(scanner) -- Pass scanner to next coroutine
				end)
				
				-- Second coroutine reads remaining lines
				coroutine.spawn(function()
					local scanner = sync:receive()
					local remaining = {}
					
					while scanner:scan() do -- Continue scanning
						table.insert(remaining, scanner:text())
					end
					
					results.remaining = remaining
					done:send(true)
				end)
				
				done:receive()
				return results
			end
        `, "test", "test_scanner_multi_async")
		require.NoError(t, err)

		result, err := wrapped.Execute(context.Background(), "test_scanner_multi_async")
		require.NoError(t, err)

		resultTable := result.(*lua.LTable)
		first := resultTable.RawGetString("first").String()
		remaining := resultTable.RawGetString("remaining").(*lua.LTable)

		assert.Equal(t, "line1", first)

		var remainingLines []string
		remaining.ForEach(func(_, value lua.LValue) {
			remainingLines = append(remainingLines, value.String())
		})

		assert.Equal(t, []string{"line2", "line3"}, remainingLines)
	})
}
