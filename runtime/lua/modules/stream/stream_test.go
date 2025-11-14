package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/wippyai/runtime/runtime/lua/engine/value"

	"github.com/wippyai/runtime/runtime/lua/engine"
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

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		// Use ReadChunk with explicit chunk size
		chunk1, err := stream.ReadChunk(5)
		require.NoError(t, err)
		assert.Equal(t, "Hello", string(chunk1))

		chunk2, err := stream.ReadChunk(5)
		require.NoError(t, err)
		assert.Equal(t, ", Wor", string(chunk2))

		chunk3, err := stream.ReadChunk(5)
		require.NoError(t, err)
		assert.Equal(t, "ld!", string(chunk3))

		_, err = stream.ReadChunk(5)
		assert.ErrorIs(t, err, io.EOF)

		assert.Equal(t, int64(13), stream.BytesRead())
	})

	t.Run("read with buffer size", func(t *testing.T) {
		testData := []byte("This is a test of different buffer sizes")
		reader := newMockReadCloser(testData)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		// Use default buffer size (too big for our test data)
		data, err := stream.ReadChunk(DefaultChunkSize)
		require.NoError(t, err)
		assert.Equal(t, string(testData), string(data))

		// EOF on next read
		_, err = stream.ReadChunk(1)
		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("direct use as io.Reader", func(t *testing.T) {
		testData := []byte("Testing io.Reader interface")
		reader := newMockReadCloser(testData)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		// Read directly using Read method
		buffer := make([]byte, 10)
		n, err := stream.Read(buffer)
		require.NoError(t, err)
		assert.Equal(t, 10, n)
		assert.Equal(t, "Testing io", string(buffer[:n]))

		// Read the rest
		buffer = make([]byte, 100)
		n, err = stream.Read(buffer)
		require.NoError(t, err)
		assert.Equal(t, ".Reader interface", string(buffer[:n]))
	})

	t.Run("close handling", func(t *testing.T) {
		reader := newMockReadCloser([]byte("test"))

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		err = stream.Close()
		require.NoError(t, err)
		assert.True(t, reader.closed)
	})

	t.Run("context cancellation during read", func(t *testing.T) {
		testData := []byte("context test data")
		reader := newMockReadCloser(testData)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		ctx, cancel := context.WithCancel(ctx)
		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		// Cancel context before read
		cancel()

		_, err = stream.ReadChunk(10)
		assert.ErrorContains(t, err, "context canceled")
	})
}

func TestStreamLua(t *testing.T) {
	logger := zap.NewNop()

	t.Run("Stream reading with mock reader", func(t *testing.T) {
		testData := []byte("Hello from mocked Stream!")
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
			-- Read data with default chunk size
			local chunk = test_stream:read()
			assert(chunk == "Hello from mocked Stream!")

			-- GetField bytes read
			local bytes = test_stream:bytes_read()
			assert(type(bytes) == "number")
			assert(bytes == #chunk)

			-- close Stream
			test_stream:close()
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)
	})

	t.Run("Stream with custom chunk size", func(t *testing.T) {
		testData := []byte("Hello from mocked Stream!")
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
			-- Read data with explicit chunk size of 5
			local chunk1 = test_stream:read(5)
			assert(chunk1 == "Hello", "Expected 'Hello', got '" .. tostring(chunk1) .. "'")
			
			local chunk2 = test_stream:read(5)
			assert(chunk2 == " from", "Expected ' from', got '" .. tostring(chunk2) .. "'")
			
			-- Read the rest with a large chunk size
			local chunk3 = test_stream:read(100)
			assert(chunk3 == " mocked Stream!", "Expected ' mocked Stream!', got '" .. tostring(chunk3) .. "'")
			
			-- Should be at EOF now
			local eofChunk = test_stream:read(1)
			assert(eofChunk == nil, "Expected nil at EOF")
		`

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)
	})

	t.Run("Stream iteration", func(t *testing.T) {
		testData := []byte("chunk1chunk2chunk3")
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
        local expected = {"chunk1chunk2chunk3"}
        local idx = 1
        
        -- Iterator with default chunk size (will read all in one go)
        for chunk in test_stream() do
            assert(chunk == expected[idx], string.format("chunk %d mismatch", idx))
            idx = idx + 1
        end
        
        assert(idx - 1 == #expected, "wrong number of iterations")
        
        -- Create a new stream with the same data for custom chunk size test
        `

		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		// Test with custom chunk size
		reader2 := newMockReadCloser(testData)
		stream2, err := NewStream(ctx, reader2)
		require.NoError(t, err)

		luaStream2 := &LuaStream{Stream: stream2}
		ud2 := l.NewUserData()
		ud2.Value = luaStream2
		l.SetMetatable(ud2, value.GetTypeMetatable(nil, "Stream"))
		l.SetGlobal("test_stream2", ud2)

		script2 := `
        local expected = {"chunk", "1chun", "k2chu", "nk3"}
        local idx = 1
        
        -- Iterator with chunk size 5
        for chunk in test_stream2(5) do
            assert(chunk == expected[idx], string.format("chunk %d mismatch: expected '%s', got '%s'", 
                idx, expected[idx], chunk))
            idx = idx + 1
        end
        
        assert(idx - 1 == #expected, "wrong number of iterations")
    	`

		err = vm.DoString(context.Background(), script2, "test")
		require.NoError(t, err)
	})
}

func TestStreamEdgeCases(t *testing.T) {
	t.Run("nil reader rejected", func(t *testing.T) {
		_, err := NewStream(context.Background(), nil)
		assert.ErrorIs(t, err, ErrInvalidReader)
	})

	t.Run("read after close", func(t *testing.T) {
		reader := newMockReadCloser([]byte("test"))
		stream, err := NewStream(context.Background(), reader)
		require.NoError(t, err)

		err = stream.Close()
		require.NoError(t, err)

		_, err = stream.ReadChunk(10)
		assert.ErrorContains(t, err, "stream closed")

		_, err = stream.Read(make([]byte, 10))
		assert.ErrorContains(t, err, "stream closed")
	})
}

func TestLuaStreamAsReadCloser(t *testing.T) {
	t.Run("LuaStream implements io.ReadCloser", func(t *testing.T) {
		testData := []byte("ReadCloser interface test")
		reader := newMockReadCloser(testData)

		uw, ctx := engine.NewUnitOfWork(context.Background(), nil)
		defer func() { _ = uw.Close() }()

		stream, err := NewStream(ctx, reader)
		require.NoError(t, err)

		luaStream := NewLuaStream(uw, stream, nil)

		// Test as io.Reader
		var readCloser io.ReadCloser = luaStream

		buffer := make([]byte, 10)
		n, err := readCloser.Read(buffer)
		require.NoError(t, err)
		assert.Equal(t, 10, n)
		assert.Equal(t, "ReadCloser", string(buffer[:n]))

		// Test close
		err = readCloser.Close()
		require.NoError(t, err)

		// Read after close should fail
		_, err = readCloser.Read(buffer)
		assert.ErrorIs(t, err, io.ErrClosedPipe)

		// Second close should be a no-op
		err = readCloser.Close()
		assert.NoError(t, err)
	})
}
