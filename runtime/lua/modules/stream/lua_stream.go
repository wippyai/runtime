package stream

import (
	"fmt"
	"github.com/ponyruntime/go-lua"
	"io"
)

// LuaStream wraps Stream for Lua
type LuaStream struct {
	*Stream
}

// checkStream verifies and returns the Stream from Lua userdata
func checkStream(l *lua.LState) (*LuaStream, error) {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*LuaStream); ok {
		return v, nil
	}
	return nil, fmt.Errorf("expected Stream, got %T", ud.Value)
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
	if err == io.EOF {
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

	l.Push(l.NewFunction(func(L *lua.LState) int {
		data, err := s.ReadChunk()
		if err != nil {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(lua.LString(data))
		return 1
	}))

	return 1
}
