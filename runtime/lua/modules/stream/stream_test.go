package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockReadCloser implements io.ReadCloser for testing
type mockReadCloser struct {
	reader    *bytes.Reader
	mu        sync.Mutex
	closed    bool
	errAfter  int
	bytesRead int
	injectErr error
}

func newMockReadCloser(data []byte) *mockReadCloser {
	return &mockReadCloser{
		reader:    bytes.NewReader(data),
		injectErr: errors.New("mock error"),
	}
}

func (m *mockReadCloser) Read(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, io.ErrClosedPipe
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
		reader := newMockReadCloser(testData)

		cfg := NewStreamConfig(5) // Set buffer size to 5 bytes

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
		assert.ErrorIs(t, err, io.EOF)

		assert.Equal(t, int64(13), stream.BytesRead())
	})

	t.Run("close handling", func(t *testing.T) {
		reader := newMockReadCloser([]byte("test"))
		stream, err := NewStream(context.Background(), reader, nil) // Test with default config
		require.NoError(t, err)

		err = stream.Close()
		require.NoError(t, err)
		assert.True(t, reader.closed)
	})

	t.Run("context cancellation during read", func(t *testing.T) {
		testData := []byte("context test data")
		reader := newMockReadCloser(testData)

		ctx, cancel := context.WithCancel(context.Background())
		stream, err := NewStream(ctx, reader, nil)
		require.NoError(t, err)

		// Cancel context before read
		cancel()

		_, err = stream.ReadChunk()
		assert.ErrorContains(t, err, "context canceled")
	})
}

func TestStreamLua(t *testing.T) {
	logger := zap.NewNop()

	t.Run("Stream reading with mock reader", func(t *testing.T) {
		testData := []byte("Hello from mocked Stream!")
		reader := newMockReadCloser(testData)

		stream, err := NewStream(context.Background(), reader, nil)
		require.NoError(t, err)

		mod := NewStreamModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		l := vm.State()
		RegisterStream(l)

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
		reader := newMockReadCloser(testData)

		cfg := NewStreamConfig(6) // 6-byte chunks
		stream, err := NewStream(context.Background(), reader, cfg)
		require.NoError(t, err)

		mod := NewStreamModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		l := vm.State()
		RegisterStream(l)

		luaStream := &LuaStream{Stream: stream}
		ud := l.NewUserData()
		ud.Value = luaStream
		l.SetMetatable(ud, l.GetTypeMetatable("Stream"))
		l.SetGlobal("test_stream", ud)

		script := `
        local expected = {"chunk1", "chunk2", "chunk3"}
        local idx = 1
        
        for chunk in test_stream() do
            assert(chunk == expected[idx], string.format("chunk %d mismatch", idx))
            idx = idx + 1
        end
        
        assert(idx - 1 == #expected, "wrong number of iterations")
        
        local iter = test_stream()
        local final = iter()
        assert(final == nil, "expected nil after all chunks read")
    	`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)
	})
}

func TestStreamEdgeCases(t *testing.T) {
	t.Run("zero buffer size defaults to 32KB", func(t *testing.T) {
		cfg := NewStreamConfig(0)
		assert.Equal(t, int64(32*1024), cfg.bufferSize)
	})

	t.Run("negative buffer size handled", func(t *testing.T) {
		cfg := NewStreamConfig(-1)
		assert.Equal(t, int64(32*1024), cfg.bufferSize)
	})

	t.Run("nil reader rejected", func(t *testing.T) {
		_, err := NewStream(context.Background(), nil, nil)
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
		l.SetField(mt, "__index", l.NewFunction(func(l *lua.LState) int {
			method := l.ToString(2)
			if method == "read" {
				l.Push(l.NewFunction(streamRead))
				return 1
			}
			return 0
		}))

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
		reader := newMockReadCloser(testData)

		stream, err := NewStream(context.Background(), reader, nil)
		require.NoError(t, err)

		mod := NewStreamModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		l := vm.State()
		RegisterStream(l)

		luaStream := &LuaStream{Stream: stream}
		ud := l.NewUserData()
		ud.Value = luaStream
		l.SetMetatable(ud, l.GetTypeMetatable("Stream"))
		l.SetGlobal("test_stream", ud)

		script := `
			local ok = test_stream:close()
			assert(ok == nil, "Close should return nil on success")

			local data, err = test_stream:read()
			assert(data == nil, "Expected nil data from closed Stream")
			assert(type(err) == "string", "Expected error string")
			assert(string.find(err, "closed"), "Error should mention Stream is closed")
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)
	})
}
