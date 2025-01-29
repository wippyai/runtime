package stream

import (
	"errors"
	"fmt"
	"io"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// LuaStream wraps Stream for Lua
type LuaStream struct {
	*Stream
}

// Module represents the Stream Lua module
type Module struct {
	log *zap.Logger
}

// NewStreamModule creates a new Stream module (internal)
func NewStreamModule(log *zap.Logger) *Module {
	return &Module{log: log}
}

// Name returns the module name
func (m *Module) Name() string {
	return "Stream"
}

// Loader registers the module functions and constants
func (m *Module) Loader(l *lua.LState) int {
	// Create module table
	mod := l.NewTable()

	RegisterStream(l)

	l.Push(mod)
	return 1
}

// RegisterStream registers the Stream type in Lua
func RegisterStream(l *lua.LState) {
	// check if the Stream type is already registered
	if _, ok := l.GetField(l.Get(lua.RegistryIndex), "Stream").(*lua.LTable); ok {
		return
	}

	// Create and register the Stream metatable
	mt := l.NewTypeMetatable("Stream")
	l.SetField(mt, "__index", mt)

	readFunc := streamRead
	if engine.IsCoroutineVM(l) {
		readFunc = streamReadAsync
	}

	// Register methods
	l.SetFuncs(mt, map[string]lua.LGFunction{
		"read":       readFunc,
		"close":      streamClose,
		"bytes_read": streamBytesRead,
		"__call":     streamIter,
	})
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

	coroutine.Wrap(l, func() *engine.Result {
		chunk, err := stream.ReadChunk()
		if errors.Is(err, io.EOF) {
			return engine.NewResult(nil, []lua.LValue{lua.LNil, lua.LNil}, nil)
		}

		if err != nil {
			return engine.NewResult(nil, []lua.LValue{lua.LNil, lua.LString(err.Error())}, nil)
		}

		return engine.NewResult(nil, []lua.LValue{lua.LString(chunk), lua.LNil}, nil)
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

	chunk, err := stream.ReadChunk()
	if errors.Is(err, io.EOF) {
		l.Push(lua.LNil)
		return 1
	}
	if err != nil {
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
	s, err := checkStream(l)
	if err != nil {
		l.RaiseError("stream iteration error: %v", err)
		return 0
	}

	l.Push(l.NewFunction(func(l *lua.LState) int {
		data, err := s.ReadChunk()
		if err != nil {
			l.Push(lua.LNil)
			return 1
		}
		l.Push(lua.LString(data))
		return 1
	}))

	return 1
}
