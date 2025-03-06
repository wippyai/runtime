package stream

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
	"io"
)

// LuaStream wraps Stream for Lua and implements io.ReadCloser interface
type LuaStream struct {
	*Stream
	onRelease context.CancelFunc
	closer    context.CancelFunc
	closed    bool
}

// Read implements io.Reader
func (ls *LuaStream) Read(p []byte) (n int, err error) {
	if ls.closed {
		return 0, io.ErrClosedPipe
	}
	return ls.Stream.Read(p)
}

// Close implements io.Closer
func (ls *LuaStream) Close() error {
	if ls.closed {
		return nil
	}

	ls.closed = true
	if ls.onRelease != nil {
		ls.onRelease()
		ls.onRelease = nil
	}

	return nil
}

// Module represents the Stream Lua module
type Module struct {
}

// NewStreamModule creates a new Stream module
func NewStreamModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "stream"
}

// Loader registers the module functions and constants
func (m *Module) Loader(l *lua.LState) int {
	// Spawn module table
	mod := l.NewTable()

	RegisterStream(l)

	l.Push(mod)
	return 1
}

// RegisterStream registers the Stream type in Lua
func RegisterStream(l *lua.LState) {
	// Check if type is already registered by directly accessing registry
	registry := l.Get(lua.RegistryIndex)
	if regTable, ok := registry.(*lua.LTable); ok {
		if mt := regTable.RawGetString("Stream"); mt != lua.LNil {
			return // Already registered
		}
	}

	// Determine which read function to use based on VM type
	readFunc := streamRead
	if engine.IsCoroutineVM(l) {
		readFunc = streamReadAsync
	}

	// Register both method sets at once
	value.RegisterTypeMethods(
		l,
		"Stream",
		map[string]lua.LGFunction{
			"__call":  streamIter,
			"__index": nil, // The RegisterTypeMethods function will handle this
		},
		map[string]lua.LGFunction{
			"read":       readFunc,
			"close":      streamClose,
			"bytes_read": streamBytesRead,
		},
	)
}

// NewLuaStream creates a new LuaStream with UoW integration
func NewLuaStream(uw engine.UnitOfWork, stream *Stream, closer context.CancelFunc) *LuaStream {
	luaStream := &LuaStream{
		Stream: stream,
		closer: closer,
		closed: false,
	}

	// Register unconditional cleanup in UoW
	luaStream.onRelease = uw.AddCleanup(func() error {
		if luaStream.closer != nil {
			luaStream.closer()
			luaStream.closer = nil
		}

		return luaStream.Stream.Close()
	})

	return luaStream
}

// checkStream verifies and returns the Stream from Lua userdata
func checkStream(l *lua.LState) (*LuaStream, error) {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*LuaStream); ok {
		return v, nil
	}
	return nil, fmt.Errorf("expected Stream, got %T", ud.Value)
}

func streamReadAsync(l *lua.LState) int {
	stream, err := checkStream(l)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Get chunk size from argument or use default
	var chunkSize int64 = DefaultChunkSize
	if l.GetTop() >= 2 {
		size := l.CheckNumber(2)
		chunkSize = int64(size)
	}

	coroutine.Wrap(l, func() *engine.Update {
		chunk, err := stream.ReadChunk(chunkSize)
		if errors.Is(err, io.EOF) {
			_ = stream.Close()
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LNil}, nil)
		}

		if err != nil {
			_ = stream.Close()
			return engine.NewUpdate(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		return engine.NewUpdate(nil, []lua.LValue{lua.LString(chunk), lua.LNil}, nil)
	})

	return -1
}

// streamRead implements the read() method in Lua
func streamRead(l *lua.LState) int {
	stream, err := checkStream(l)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Get chunk size from argument or use default
	var chunkSize int64 = DefaultChunkSize
	if l.GetTop() >= 2 {
		size := l.CheckNumber(2)
		chunkSize = int64(size)
	}

	chunk, err := stream.ReadChunk(chunkSize)
	if errors.Is(err, io.EOF) {
		_ = stream.Close()

		l.Push(lua.LNil)
		return 1
	}

	if err != nil {
		_ = stream.Close()

		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(chunk))
	return 1
}

// streamClose implements the close() method in Lua
func streamClose(l *lua.LState) int {
	stream, err := checkStream(l)
	if err != nil {
		l.Push(lua.LString(err.Error()))
		return 1
	}

	if err := stream.Close(); err != nil {
		l.Push(lua.LString(err.Error()))
		return 1
	}

	return 0
}

// streamBytesRead implements the bytes_read() method in Lua
func streamBytesRead(l *lua.LState) int {
	stream, err := checkStream(l)
	if err != nil {
		l.Push(lua.LNumber(-1))
		return 1
	}

	l.Push(lua.LNumber(stream.BytesRead()))
	return 1
}

// streamIter implements the __call metamethod for iteration in Lua
func streamIter(l *lua.LState) int {
	stream, err := checkStream(l)
	if err != nil {
		l.RaiseError("stream iteration error: %v", err)
		return 0
	}

	// Get optional chunk size for iteration
	var chunkSize int64 = DefaultChunkSize
	if l.GetTop() >= 2 {
		size := l.CheckNumber(2)
		chunkSize = int64(size)
	}

	// Capture chunk size in closure
	iterSize := chunkSize

	l.Push(l.NewFunction(func(l *lua.LState) int {
		data, err := stream.ReadChunk(iterSize)
		if err != nil {
			_ = stream.Close()
			l.Push(lua.LNil)
			return 1
		}
		l.Push(lua.LString(data))
		return 1
	}))

	return 1
}
