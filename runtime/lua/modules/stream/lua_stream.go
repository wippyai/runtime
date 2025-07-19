package stream

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// LuaStream wraps Stream for Lua and implements io.ReadCloser interface
type LuaStream struct {
	*Stream
	release    context.CancelFunc
	onComplete context.CancelFunc
	closed     bool
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
	if ls.release != nil {
		ls.release()
		ls.release = nil
	}

	return nil
}

// LuaScanner wraps Scanner for Lua integration
type LuaScanner struct {
	*Scanner
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
	mod := l.NewTable()
	RegisterStream(l)
	l.Push(mod)
	return 1
}

// RegisterStream registers the Stream and Scanner types in Lua
func RegisterStream(l *lua.LState) {
	registry := l.Get(lua.RegistryIndex)
	if regTable, ok := registry.(*lua.LTable); ok {
		if mt := regTable.RawGetString("Stream"); mt != lua.LNil {
			return
		}
	}

	readFunc := streamRead
	scanFunc := scannerScan
	if engine.IsCoroutineVM(l) {
		readFunc = streamReadAsync
		scanFunc = scannerScanAsync
	}

	value.RegisterTypeMethods(
		l,
		"Stream",
		map[string]lua.LGFunction{
			"__call":  streamIter,
			"__index": nil,
		},
		map[string]lua.LGFunction{
			"read":       readFunc,
			"close":      streamClose,
			"bytes_read": streamBytesRead,
			"scanner":    streamScanner,
		},
	)

	value.RegisterTypeMethods(
		l,
		"Scanner",
		map[string]lua.LGFunction{
			"__index": nil,
		},
		map[string]lua.LGFunction{
			"scan": scanFunc,
			"text": scannerText,
			"err":  scannerErr,
		},
	)
}

// NewLuaStream creates a new LuaStream with UoW integration
func NewLuaStream(uw engine.UnitOfWork, stream *Stream, onComplete context.CancelFunc) *LuaStream {
	luaStream := &LuaStream{
		Stream:     stream,
		onComplete: onComplete,
		closed:     false,
	}

	luaStream.release = uw.AddCleanup(func() error {
		if luaStream.onComplete != nil {
			luaStream.onComplete()
			luaStream.onComplete = nil
		}
		return luaStream.Stream.Close()
	})

	return luaStream
}

// NewLuaScanner creates a new LuaScanner
func NewLuaScanner(scanner *Scanner) *LuaScanner {
	return &LuaScanner{
		Scanner: scanner,
	}
}

// checkStream verifies and returns the Stream from Lua userdata
func checkStream(l *lua.LState) (*LuaStream, error) {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*LuaStream); ok {
		return v, nil
	}
	return nil, fmt.Errorf("expected Stream, got %T", ud.Value)
}

// checkScanner verifies and returns the Scanner from Lua userdata
func checkScanner(l *lua.LState) (*LuaScanner, error) {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*LuaScanner); ok {
		return v, nil
	}
	return nil, fmt.Errorf("expected Scanner, got %T", ud.Value)
}

func streamReadAsync(l *lua.LState) int {
	stream, err := checkStream(l)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	var chunkSize = DefaultChunkSize
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

func streamRead(l *lua.LState) int {
	stream, err := checkStream(l)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	var chunkSize = DefaultChunkSize
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

func streamClose(l *lua.LState) int {
	stream, err := checkStream(l)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if err := stream.Close(); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LBool(true))
	return 1
}

func streamBytesRead(l *lua.LState) int {
	stream, err := checkStream(l)
	if err != nil {
		l.Push(lua.LNumber(-1))
		return 1
	}

	l.Push(lua.LNumber(stream.BytesRead()))
	return 1
}

func streamIter(l *lua.LState) int {
	stream, err := checkStream(l)
	if err != nil {
		l.RaiseError("stream iteration error: %v", err)
		return 0
	}

	var chunkSize = DefaultChunkSize
	if l.GetTop() >= 2 {
		size := l.CheckNumber(2)
		chunkSize = int64(size)
	}

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

func streamScanner(l *lua.LState) int {
	stream, err := checkStream(l)
	if err != nil {
		l.RaiseError("scanner creation error: %v", err)
		return 0
	}

	var splitType SplitType = SplitLines
	if l.GetTop() >= 2 {
		splitStr := l.CheckString(2)
		switch splitStr {
		case "lines":
			splitType = SplitLines
		case "words":
			splitType = SplitWords
		case "bytes":
			splitType = SplitBytes
		case "runes":
			splitType = SplitRunes
		default:
			l.RaiseError("unsupported split type: %s", splitStr)
			return 0
		}
	}

	scanner, err := NewScanner(stream.Stream, splitType)
	if err != nil {
		l.RaiseError("failed to create scanner: %v", err)
		return 0
	}

	luaScanner := NewLuaScanner(scanner)

	ud := l.NewUserData()
	ud.Value = luaScanner
	ud.Metatable = value.GetTypeMetatable(l, "Scanner")
	l.Push(ud)
	return 1
}

func scannerScanAsync(l *lua.LState) int {
	scanner, err := checkScanner(l)
	if err != nil {
		l.Push(lua.LBool(false))
		return 1
	}

	coroutine.Wrap(l, func() *engine.Update {
		result := scanner.Scan()
		return engine.NewUpdate(nil, []lua.LValue{lua.LBool(result)}, nil)
	})

	return -1
}

func scannerScan(l *lua.LState) int {
	scanner, err := checkScanner(l)
	if err != nil {
		l.Push(lua.LBool(false))
		return 1
	}

	result := scanner.Scan()
	l.Push(lua.LBool(result))
	return 1
}

func scannerText(l *lua.LState) int {
	scanner, err := checkScanner(l)
	if err != nil {
		l.Push(lua.LString(""))
		return 1
	}

	text := scanner.Text()
	l.Push(lua.LString(text))
	return 1
}

func scannerErr(l *lua.LState) int {
	scanner, err := checkScanner(l)
	if err != nil {
		l.Push(lua.LString(err.Error()))
		return 1
	}

	scanErr := scanner.Err()
	if scanErr != nil {
		l.Push(lua.LString(scanErr.Error()))
	} else {
		l.Push(lua.LNil)
	}
	return 1
}
