package stream

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	streamapi "github.com/wippyai/runtime/api/dispatcher/stream"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// ReadYield is yielded to read a chunk from a stream.
type ReadYield struct {
	StreamID uint64
	Size     int64
}

var readYieldPool = sync.Pool{
	New: func() interface{} { return &ReadYield{} },
}

func AcquireReadYield(id uint64, size int64) *ReadYield {
	y := readYieldPool.Get().(*ReadYield)
	y.StreamID = id
	y.Size = size
	return y
}

func ReleaseReadYield(y *ReadYield) {
	y.StreamID = 0
	y.Size = 0
	readYieldPool.Put(y)
}

func (y *ReadYield) String() string       { return "<stream_read_yield>" }
func (y *ReadYield) Type() lua.LValueType { return lua.LTUserData }

func (y *ReadYield) CmdID() dispatcher.CommandID {
	return streamapi.CmdRead
}

func (y *ReadYield) ToCommand() dispatcher.Command {
	return streamapi.ReadCmd{StreamID: y.StreamID, Size: y.Size}
}

func (y *ReadYield) Release() { ReleaseReadYield(y) }

func (y *ReadYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "stream read").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if data == nil {
		return []lua.LValue{lua.LNil, lua.LNil}
	}
	if bytes, ok := data.([]byte); ok {
		return []lua.LValue{lua.LString(bytes), lua.LNil}
	}
	luaErr := lua.NewLuaError(l, "invalid response type").
		WithKind(lua.KindInternal).
		WithRetryable(false)
	return []lua.LValue{lua.LNil, luaErr}
}

// CloseYield is yielded to close a stream.
type CloseYield struct {
	StreamID uint64
}

var closeYieldPool = sync.Pool{
	New: func() interface{} { return &CloseYield{} },
}

func AcquireCloseYield(id uint64) *CloseYield {
	y := closeYieldPool.Get().(*CloseYield)
	y.StreamID = id
	return y
}

func ReleaseCloseYield(y *CloseYield) {
	y.StreamID = 0
	closeYieldPool.Put(y)
}

func (y *CloseYield) String() string       { return "<stream_close_yield>" }
func (y *CloseYield) Type() lua.LValueType { return lua.LTUserData }

func (y *CloseYield) CmdID() dispatcher.CommandID {
	return streamapi.CmdClose
}

func (y *CloseYield) ToCommand() dispatcher.Command {
	return streamapi.CloseCmd{StreamID: y.StreamID}
}

func (y *CloseYield) Release() { ReleaseCloseYield(y) }

func (y *CloseYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "stream close").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LFalse, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// WriteYield is yielded to write data to a stream.
type WriteYield struct {
	StreamID uint64
	Data     []byte
}

var writeYieldPool = sync.Pool{
	New: func() interface{} { return &WriteYield{} },
}

func AcquireWriteYield(id uint64, data []byte) *WriteYield {
	y := writeYieldPool.Get().(*WriteYield)
	y.StreamID = id
	y.Data = data
	return y
}

func ReleaseWriteYield(y *WriteYield) {
	y.StreamID = 0
	y.Data = nil
	writeYieldPool.Put(y)
}

func (y *WriteYield) String() string       { return "<stream_write_yield>" }
func (y *WriteYield) Type() lua.LValueType { return lua.LTUserData }

func (y *WriteYield) CmdID() dispatcher.CommandID {
	return streamapi.CmdWrite
}

func (y *WriteYield) ToCommand() dispatcher.Command {
	return streamapi.WriteCmd{StreamID: y.StreamID, Data: y.Data}
}

func (y *WriteYield) Release() { ReleaseWriteYield(y) }

func (y *WriteYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "stream write").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNumber(0), luaErr}
	}
	if n, ok := data.(int64); ok {
		return []lua.LValue{lua.LNumber(n), lua.LNil}
	}
	luaErr := lua.NewLuaError(l, "invalid response type").
		WithKind(lua.KindInternal).
		WithRetryable(false)
	return []lua.LValue{lua.LNumber(0), luaErr}
}

// SeekYield is yielded to seek within a stream.
type SeekYield struct {
	StreamID uint64
	Offset   int64
	Whence   int
}

var seekYieldPool = sync.Pool{
	New: func() interface{} { return &SeekYield{} },
}

func AcquireSeekYield(id uint64, offset int64, whence int) *SeekYield {
	y := seekYieldPool.Get().(*SeekYield)
	y.StreamID = id
	y.Offset = offset
	y.Whence = whence
	return y
}

func ReleaseSeekYield(y *SeekYield) {
	y.StreamID = 0
	y.Offset = 0
	y.Whence = 0
	seekYieldPool.Put(y)
}

func (y *SeekYield) String() string       { return "<stream_seek_yield>" }
func (y *SeekYield) Type() lua.LValueType { return lua.LTUserData }

func (y *SeekYield) CmdID() dispatcher.CommandID {
	return streamapi.CmdSeek
}

func (y *SeekYield) ToCommand() dispatcher.Command {
	return streamapi.SeekCmd{StreamID: y.StreamID, Offset: y.Offset, Whence: y.Whence}
}

func (y *SeekYield) Release() { ReleaseSeekYield(y) }

func (y *SeekYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "stream seek").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNumber(-1), luaErr}
	}
	if pos, ok := data.(int64); ok {
		return []lua.LValue{lua.LNumber(pos), lua.LNil}
	}
	luaErr := lua.NewLuaError(l, "invalid response type").
		WithKind(lua.KindInternal).
		WithRetryable(false)
	return []lua.LValue{lua.LNumber(-1), luaErr}
}

// FlushYield is yielded to flush a stream.
type FlushYield struct {
	StreamID uint64
}

var flushYieldPool = sync.Pool{
	New: func() interface{} { return &FlushYield{} },
}

func AcquireFlushYield(id uint64) *FlushYield {
	y := flushYieldPool.Get().(*FlushYield)
	y.StreamID = id
	return y
}

func ReleaseFlushYield(y *FlushYield) {
	y.StreamID = 0
	flushYieldPool.Put(y)
}

func (y *FlushYield) String() string       { return "<stream_flush_yield>" }
func (y *FlushYield) Type() lua.LValueType { return lua.LTUserData }

func (y *FlushYield) CmdID() dispatcher.CommandID {
	return streamapi.CmdFlush
}

func (y *FlushYield) ToCommand() dispatcher.Command {
	return streamapi.FlushCmd{StreamID: y.StreamID}
}

func (y *FlushYield) Release() { ReleaseFlushYield(y) }

func (y *FlushYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "stream flush").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LFalse, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// StatYield is yielded to get stream info.
type StatYield struct {
	StreamID uint64
}

var statYieldPool = sync.Pool{
	New: func() interface{} { return &StatYield{} },
}

func AcquireStatYield(id uint64) *StatYield {
	y := statYieldPool.Get().(*StatYield)
	y.StreamID = id
	return y
}

func ReleaseStatYield(y *StatYield) {
	y.StreamID = 0
	statYieldPool.Put(y)
}

func (y *StatYield) String() string       { return "<stream_stat_yield>" }
func (y *StatYield) Type() lua.LValueType { return lua.LTUserData }

func (y *StatYield) CmdID() dispatcher.CommandID {
	return streamapi.CmdStat
}

func (y *StatYield) ToCommand() dispatcher.Command {
	return streamapi.StatCmd{StreamID: y.StreamID}
}

func (y *StatYield) Release() { ReleaseStatYield(y) }

func (y *StatYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "stream stat").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		lua.SetErrorMetatable(l, luaErr)
		return []lua.LValue{lua.LNil, luaErr}
	}
	info, ok := data.(streamapi.Info)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid response type").
			WithKind(lua.KindInternal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	result := l.CreateTable(0, 5)
	result.RawSetString("size", lua.LNumber(info.Size))
	result.RawSetString("position", lua.LNumber(info.Position))
	result.RawSetString("readable", lua.LBool(info.Readable))
	result.RawSetString("writable", lua.LBool(info.Writable))
	result.RawSetString("seekable", lua.LBool(info.Seekable))
	return []lua.LValue{result, lua.LNil}
}

// Stream is the Lua userdata for stream operations.
type Stream struct {
	ID uint64
}

const streamTypeName = "stream.Stream"

var (
	streamMetatableOnce sync.Once
	streamMetatable     *lua.LTable
)

// registerStreamMetatable registers the shared stream metatable once.
func registerStreamMetatable() {
	streamMetatableOnce.Do(func() {
		streamMetatable = value.RegisterTypeMethods(nil, streamTypeName, nil, streamMethods)
	})
}

// NewStream creates a Stream userdata from an ID.
func NewStream(l *lua.LState, id uint64) lua.LValue {
	registerStreamMetatable()
	ud := l.NewUserData()
	ud.Value = &Stream{ID: id}
	ud.Metatable = streamMetatable
	return ud
}

var streamMethods = map[string]lua.LGFunction{
	"read":  streamReadMethod,
	"write": streamWriteMethod,
	"seek":  streamSeekMethod,
	"flush": streamFlushMethod,
	"stat":  streamStatMethod,
	"close": streamCloseMethod,
}

func checkStream(l *lua.LState, idx int) *Stream {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Stream); ok {
		return v
	}
	l.ArgError(idx, "Stream expected")
	return nil
}

func streamReadMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	size := int64(0)
	if l.GetTop() >= 2 {
		size = int64(l.CheckNumber(2))
	}
	yield := AcquireReadYield(stream.ID, size)
	l.Push(yield)
	return -1
}

func streamCloseMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	yield := AcquireCloseYield(stream.ID)
	l.Push(yield)
	return -1
}

func streamWriteMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	data := l.CheckString(2)
	yield := AcquireWriteYield(stream.ID, []byte(data))
	l.Push(yield)
	return -1
}

func streamSeekMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	whence := streamapi.SeekStart
	offset := int64(0)

	if l.GetTop() >= 2 {
		whenceStr := l.CheckString(2)
		switch whenceStr {
		case "set":
			whence = streamapi.SeekStart
		case "cur":
			whence = streamapi.SeekCurrent
		case "end":
			whence = streamapi.SeekEnd
		default:
			l.ArgError(2, "invalid whence: must be 'set', 'cur', or 'end'")
			return 0
		}
	}
	if l.GetTop() >= 3 {
		offset = int64(l.CheckNumber(3))
	}

	yield := AcquireSeekYield(stream.ID, offset, whence)
	l.Push(yield)
	return -1
}

func streamFlushMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	yield := AcquireFlushYield(stream.ID)
	l.Push(yield)
	return -1
}

func streamStatMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	yield := AcquireStatYield(stream.ID)
	l.Push(yield)
	return -1
}
