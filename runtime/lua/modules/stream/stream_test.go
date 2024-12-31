package stream

import (
	"bytes"
	"context"
	"errors"
	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockReadCloser implements io.ReadCloser for testing
type mockReadCloser struct {
	reader    *bytes.Reader
	mu        sync.Mutex
	closed    bool
	delay     time.Duration
	errAfter  int
	bytesRead int
	injectErr error
}

func newMockReadCloser(data []byte, delay time.Duration) *mockReadCloser {
	return &mockReadCloser{
		reader:    bytes.NewReader(data),
		delay:     delay,
		injectErr: errors.New("mock error"),
	}
}

func (m *mockReadCloser) Read(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, io.ErrClosedPipe
	}

	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	if m.errAfter > 0 && m.bytesRead >= m.errAfter {
		return 0, m.injectErr
	}

	n, err = m.reader.Read(p)
	m.bytesRead += n
	return n, err
}

func (m *mockReadCloser) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true
	return nil
}

func TestStream(t *testing.T) {
	t.Run("basic read operations", func(t *testing.T) {
		testData := []byte("Hello, World!")
		reader := newMockReadCloser(testData, 0)

		cfg, err := NewStreamConfig(5, 0, 0)
		require.NoError(t, err)

		stream, err := NewStream(context.Background(), reader, cfg)
		require.NoError(t, err)

		chunk1, err := stream.ReadChunk()
		require.NoError(t, err)
		assert.Equal(t, "Hello", string(chunk1))

		chunk2, err := stream.ReadChunk()
		require.NoError(t, err)
		assert.Equal(t, ", Wor", string(chunk2))

		chunk3, err := stream.ReadChunk()
		require.NoError(t, err)
		assert.Equal(t, "ld!", string(chunk3))

		_, err = stream.ReadChunk()
		assert.ErrorContains(t, err, "EOF")

		assert.Equal(t, int64(13), stream.BytesRead())
	})

	t.Run("max size limit", func(t *testing.T) {
		testData := []byte("Hello, World!")
		reader := newMockReadCloser(testData, 0)

		cfg, err := NewStreamConfig(32*1024, 5, 0)
		require.NoError(t, err)

		stream, err := NewStream(context.Background(), reader, cfg)
		require.NoError(t, err)

		_, err = stream.ReadChunk()
		require.Error(t, err)
		assert.Equal(t, ErrMaxSizeExceeded, err)
	})

	t.Run("close handling", func(t *testing.T) {
		reader := newMockReadCloser([]byte("test"), 0)

		cfg, err := NewStreamConfig(32*1024, 0, 0)
		require.NoError(t, err)

		stream, err := NewStream(context.Background(), reader, cfg)
		require.NoError(t, err)

		err = stream.Close()
		require.NoError(t, err)
		assert.True(t, reader.closed)
	})
}

func TestStreamLua(t *testing.T) {
	logger := zap.NewNop()

	t.Run("Stream reading with mock reader", func(t *testing.T) {
		testData := []byte("Hello from mocked Stream!")
		reader := newMockReadCloser(testData, 0)

		cfg, err := NewStreamConfig(int64(len(testData)), 0, 0)
		require.NoError(t, err)

		stream, err := NewStream(context.Background(), reader, cfg)
		require.NoError(t, err)

		mod := NewStreamModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		l := vm.State()
		RegisterStream(l, l.NewTable())

		luaStream := &LuaStream{Stream: stream}
		ud := l.NewUserData()
		ud.Value = luaStream
		l.SetMetatable(ud, l.GetTypeMetatable("Stream"))
		l.SetGlobal("test_stream", ud)

		script := `
			-- Read data
			local chunk = test_stream:read()
			assert(chunk == "Hello from mocked Stream!")

			-- Get bytes read
			local bytes = test_stream:bytes_read()
			assert(type(bytes) == "number")
			assert(bytes == #chunk)

			-- Close Stream
			test_stream:close()
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)
	})

	t.Run("Stream iteration", func(t *testing.T) {
		testData := []byte("chunk1chunk2chunk3")
		reader := newMockReadCloser(testData, 0)

		cfg, err := NewStreamConfig(6, 0, 0)
		require.NoError(t, err)

		stream, err := NewStream(context.Background(), reader, cfg)
		require.NoError(t, err)

		mod := NewStreamModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		l := vm.State()
		RegisterStream(l, l.NewTable())

		luaStream := &LuaStream{Stream: stream}
		ud := l.NewUserData()
		ud.Value = luaStream
		l.SetMetatable(ud, l.GetTypeMetatable("Stream"))
		l.SetGlobal("test_stream", ud)

		script := `
        local expected = {"chunk1", "chunk2", "chunk3"}
        local idx = 1
        
        -- Using Lua's generic for loop which handles nil end condition
        for chunk in test_stream() do
            assert(chunk == expected[idx], string.format("chunk %d mismatch", idx))
            idx = idx + 1
        end
        
        -- Verify we got all expected chunks
        assert(idx - 1 == #expected, "wrong number of iterations")
        
        -- Try one more iteration to ensure proper EOF handling
        local iter = test_stream()
        local final = iter()
        assert(final == nil, "expected nil after all chunks read")
    	`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)
	})
}

func TestStreamTimeoutBehavior(t *testing.T) {
	t.Run("read with timeout success", func(t *testing.T) {
		testData := []byte("timeout test data")
		reader := newMockReadCloser(testData, 50*time.Millisecond)

		cfg, err := NewStreamConfig(32*1024, 0, 100*time.Millisecond)
		require.NoError(t, err)

		stream, err := NewStream(context.Background(), reader, cfg)
		require.NoError(t, err)

		chunk, err := stream.ReadChunk()
		require.NoError(t, err)
		assert.Equal(t, testData, chunk)
	})

	t.Run("read timeout exceeded", func(t *testing.T) {
		testData := []byte("timeout test data")
		reader := newMockReadCloser(testData, 200*time.Millisecond)

		cfg, err := NewStreamConfig(32*1024, 0, 100*time.Millisecond)
		require.NoError(t, err)

		stream, err := NewStream(context.Background(), reader, cfg)
		require.NoError(t, err)

		_, err = stream.ReadChunk()
		assert.ErrorIs(t, err, ErrReadTimeout)
	})
}

func TestStreamContextCancellation(t *testing.T) {
	t.Run("context cancellation during read", func(t *testing.T) {
		testData := []byte("context test data")
		reader := newMockReadCloser(testData, 200*time.Millisecond)

		ctx, cancel := context.WithCancel(context.Background())
		cfg, err := NewStreamConfig(32*1024, 0, 400*time.Millisecond)
		require.NoError(t, err)

		stream, err := NewStream(ctx, reader, cfg)
		require.NoError(t, err)

		// Cancel context after a short delay
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		_, err = stream.ReadChunk()
		assert.ErrorContains(t, err, "context canceled")
	})
}

func TestStreamEdgeCases(t *testing.T) {
	t.Run("zero buffer size defaults to 32KB", func(t *testing.T) {
		cfg, err := NewStreamConfig(0, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, int64(32*1024), cfg.bufferSize)
	})

	t.Run("negative buffer size handled", func(t *testing.T) {
		cfg, err := NewStreamConfig(-1, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, int64(32*1024), cfg.bufferSize)
	})

	t.Run("negative max size rejected", func(t *testing.T) {
		_, err := NewStreamConfig(0, -1, 0)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})

	t.Run("negative timeout rejected", func(t *testing.T) {
		_, err := NewStreamConfig(0, 0, -1*time.Second)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})

	t.Run("nil reader rejected", func(t *testing.T) {
		cfg, _ := NewStreamConfig(0, 0, 0)
		_, err := NewStream(context.Background(), nil, cfg)
		assert.ErrorIs(t, err, ErrInvalidConfig)
	})
}

func TestStreamLuaEdgeCases(t *testing.T) {
	logger := zap.NewNop()

	t.Run("invalid userdata type", func(t *testing.T) {
		mod := NewStreamModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		l := vm.State()
		mt := l.NewTypeMetatable("Stream")
		l.SetField(mt, "__index", l.NewFunction(func(L *lua.LState) int {
			method := L.ToString(2)
			if method == "read" {
				L.Push(l.NewFunction(streamRead))
				return 1
			}
			return 0
		}))

		// Create userdata with wrong type
		ud := l.NewUserData()
		ud.Value = "not a Stream"
		l.SetMetatable(ud, mt)
		l.SetGlobal("test_stream", ud)

		script := `
			local success, err = test_stream:read()
			assert(success == nil, "Expected nil result for invalid Stream")
			assert(type(err) == "string", "Expected error message")
			assert(string.find(err, "expected Stream"), "Error should mention expected Stream type")
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)
	})

	t.Run("read after close", func(t *testing.T) {
		testData := []byte("test data")
		reader := newMockReadCloser(testData, 0)

		cfg, err := NewStreamConfig(32*1024, 0, 0)
		require.NoError(t, err)

		stream, err := NewStream(context.Background(), reader, cfg)
		require.NoError(t, err)

		mod := NewStreamModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		l := vm.State()
		RegisterStream(l, l.NewTable())

		luaStream := &LuaStream{Stream: stream}
		ud := l.NewUserData()
		ud.Value = luaStream
		l.SetMetatable(ud, l.GetTypeMetatable("Stream"))
		l.SetGlobal("test_stream", ud)

		script := `
			-- Close the Stream
			local ok = test_stream:close()
			assert(ok == nil, "Close should return nil on success")

			-- Try to read after close
			local data, err = test_stream:read()
			assert(data == nil, "Expected nil data from closed Stream")
			assert(type(err) == "string", "Expected error string")
			assert(string.find(err, "closed"), "Error should mention Stream is closed")
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)
	})

	t.Run("Stream iteration with error", func(t *testing.T) {
		testData := []byte("chunk1chunk2")
		reader := newMockReadCloser(testData, 0)

		// Introduce an error after reading 6 bytes (first chunk)
		reader.errAfter = 6

		cfg, err := NewStreamConfig(6, 0, 0) // Buffer size of 6
		require.NoError(t, err)

		stream, err := NewStream(context.Background(), reader, cfg)
		require.NoError(t, err)

		mod := NewStreamModule(logger)
		vm, err := engine.NewVM(
			logger,
			engine.WithLoader(mod.Name(), mod.Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		l := vm.State()
		RegisterStream(l, l.NewTable())

		luaStream := &LuaStream{Stream: stream}
		ud := l.NewUserData()
		ud.Value = luaStream
		l.SetMetatable(ud, l.GetTypeMetatable("Stream"))
		l.SetGlobal("test_stream", ud)

		script := `
        local chunks = {}
        local err_msg = nil

        for chunk in test_stream() do
            if err then
                err_msg = err
                break
            end
            table.insert(chunks, chunk)
        end
		
        assert(#chunks == 1, "Expected 1 chunk before error")
        assert(chunks[1] == "chunk1", "First chunk mismatch")
    	`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)
	})
}
