package stream

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	streamapi "github.com/wippyai/runtime/api/dispatcher/stream"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// StreamReadYield is yielded to read a chunk from a stream.
type StreamReadYield struct {
	StreamID uint64
	Size     int64
}

var streamReadYieldPool = sync.Pool{
	New: func() interface{} { return &StreamReadYield{} },
}

func AcquireStreamReadYield(id uint64, size int64) *StreamReadYield {
	y := streamReadYieldPool.Get().(*StreamReadYield)
	y.StreamID = id
	y.Size = size
	return y
}

func ReleaseStreamReadYield(y *StreamReadYield) {
	y.StreamID = 0
	y.Size = 0
	streamReadYieldPool.Put(y)
}

func (y *StreamReadYield) String() string       { return "<stream_read_yield>" }
func (y *StreamReadYield) Type() lua.LValueType { return lua.LTUserData }

func (y *StreamReadYield) CmdID() dispatcher.CommandID {
	return streamapi.CmdStreamRead
}

func (y *StreamReadYield) ToCommand() dispatcher.Command {
	return streamapi.StreamReadCmd{StreamID: y.StreamID, Size: y.Size}
}

func (y *StreamReadYield) Release() { ReleaseStreamReadYield(y) }

func (y *StreamReadYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	if data == nil {
		return []lua.LValue{lua.LNil, lua.LNil}
	}
	if bytes, ok := data.([]byte); ok {
		return []lua.LValue{lua.LString(bytes), lua.LNil}
	}
	return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
}

// StreamCloseYield is yielded to close a stream.
type StreamCloseYield struct {
	StreamID uint64
}

var streamCloseYieldPool = sync.Pool{
	New: func() interface{} { return &StreamCloseYield{} },
}

func AcquireStreamCloseYield(id uint64) *StreamCloseYield {
	y := streamCloseYieldPool.Get().(*StreamCloseYield)
	y.StreamID = id
	return y
}

func ReleaseStreamCloseYield(y *StreamCloseYield) {
	y.StreamID = 0
	streamCloseYieldPool.Put(y)
}

func (y *StreamCloseYield) String() string       { return "<stream_close_yield>" }
func (y *StreamCloseYield) Type() lua.LValueType { return lua.LTUserData }

func (y *StreamCloseYield) CmdID() dispatcher.CommandID {
	return streamapi.CmdStreamClose
}

func (y *StreamCloseYield) ToCommand() dispatcher.Command {
	return streamapi.StreamCloseCmd{StreamID: y.StreamID}
}

func (y *StreamCloseYield) Release() { ReleaseStreamCloseYield(y) }

func (y *StreamCloseYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LFalse, lua.LString(err.Error())}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// StreamWriteYield is yielded to write data to a stream.
type StreamWriteYield struct {
	StreamID uint64
	Data     []byte
}

var streamWriteYieldPool = sync.Pool{
	New: func() interface{} { return &StreamWriteYield{} },
}

func AcquireStreamWriteYield(id uint64, data []byte) *StreamWriteYield {
	y := streamWriteYieldPool.Get().(*StreamWriteYield)
	y.StreamID = id
	y.Data = data
	return y
}

func ReleaseStreamWriteYield(y *StreamWriteYield) {
	y.StreamID = 0
	y.Data = nil
	streamWriteYieldPool.Put(y)
}

func (y *StreamWriteYield) String() string       { return "<stream_write_yield>" }
func (y *StreamWriteYield) Type() lua.LValueType { return lua.LTUserData }

func (y *StreamWriteYield) CmdID() dispatcher.CommandID {
	return streamapi.CmdStreamWrite
}

func (y *StreamWriteYield) ToCommand() dispatcher.Command {
	return streamapi.StreamWriteCmd{StreamID: y.StreamID, Data: y.Data}
}

func (y *StreamWriteYield) Release() { ReleaseStreamWriteYield(y) }

func (y *StreamWriteYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNumber(0), lua.LString(err.Error())}
	}
	if n, ok := data.(int64); ok {
		return []lua.LValue{lua.LNumber(n), lua.LNil}
	}
	return []lua.LValue{lua.LNumber(0), lua.LString("invalid response type")}
}

// StreamSeekYield is yielded to seek within a stream.
type StreamSeekYield struct {
	StreamID uint64
	Offset   int64
	Whence   int
}

var streamSeekYieldPool = sync.Pool{
	New: func() interface{} { return &StreamSeekYield{} },
}

func AcquireStreamSeekYield(id uint64, offset int64, whence int) *StreamSeekYield {
	y := streamSeekYieldPool.Get().(*StreamSeekYield)
	y.StreamID = id
	y.Offset = offset
	y.Whence = whence
	return y
}

func ReleaseStreamSeekYield(y *StreamSeekYield) {
	y.StreamID = 0
	y.Offset = 0
	y.Whence = 0
	streamSeekYieldPool.Put(y)
}

func (y *StreamSeekYield) String() string       { return "<stream_seek_yield>" }
func (y *StreamSeekYield) Type() lua.LValueType { return lua.LTUserData }

func (y *StreamSeekYield) CmdID() dispatcher.CommandID {
	return streamapi.CmdStreamSeek
}

func (y *StreamSeekYield) ToCommand() dispatcher.Command {
	return streamapi.StreamSeekCmd{StreamID: y.StreamID, Offset: y.Offset, Whence: y.Whence}
}

func (y *StreamSeekYield) Release() { ReleaseStreamSeekYield(y) }

func (y *StreamSeekYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNumber(-1), lua.LString(err.Error())}
	}
	if pos, ok := data.(int64); ok {
		return []lua.LValue{lua.LNumber(pos), lua.LNil}
	}
	return []lua.LValue{lua.LNumber(-1), lua.LString("invalid response type")}
}

// StreamFlushYield is yielded to flush a stream.
type StreamFlushYield struct {
	StreamID uint64
}

var streamFlushYieldPool = sync.Pool{
	New: func() interface{} { return &StreamFlushYield{} },
}

func AcquireStreamFlushYield(id uint64) *StreamFlushYield {
	y := streamFlushYieldPool.Get().(*StreamFlushYield)
	y.StreamID = id
	return y
}

func ReleaseStreamFlushYield(y *StreamFlushYield) {
	y.StreamID = 0
	streamFlushYieldPool.Put(y)
}

func (y *StreamFlushYield) String() string       { return "<stream_flush_yield>" }
func (y *StreamFlushYield) Type() lua.LValueType { return lua.LTUserData }

func (y *StreamFlushYield) CmdID() dispatcher.CommandID {
	return streamapi.CmdStreamFlush
}

func (y *StreamFlushYield) ToCommand() dispatcher.Command {
	return streamapi.StreamFlushCmd{StreamID: y.StreamID}
}

func (y *StreamFlushYield) Release() { ReleaseStreamFlushYield(y) }

func (y *StreamFlushYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LFalse, lua.LString(err.Error())}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// StreamStatYield is yielded to get stream info.
type StreamStatYield struct {
	StreamID uint64
}

var streamStatYieldPool = sync.Pool{
	New: func() interface{} { return &StreamStatYield{} },
}

func AcquireStreamStatYield(id uint64) *StreamStatYield {
	y := streamStatYieldPool.Get().(*StreamStatYield)
	y.StreamID = id
	return y
}

func ReleaseStreamStatYield(y *StreamStatYield) {
	y.StreamID = 0
	streamStatYieldPool.Put(y)
}

func (y *StreamStatYield) String() string       { return "<stream_stat_yield>" }
func (y *StreamStatYield) Type() lua.LValueType { return lua.LTUserData }

func (y *StreamStatYield) CmdID() dispatcher.CommandID {
	return streamapi.CmdStreamStat
}

func (y *StreamStatYield) ToCommand() dispatcher.Command {
	return streamapi.StreamStatCmd{StreamID: y.StreamID}
}

func (y *StreamStatYield) Release() { ReleaseStreamStatYield(y) }

func (y *StreamStatYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}
	info, ok := data.(streamapi.StreamInfo)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid response type")}
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

// BindStream binds stream functions to Lua.
func BindStream(l *lua.LState) {
	registerStreamMetatable()

	l.SetGlobal("__stream_new", lua.LGoFunc(func(l *lua.LState) int {
		id := uint64(l.CheckNumber(1))
		value.PushUserData(l, &Stream{ID: id}, streamMetatable)
		return 1
	}))
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
	yield := AcquireStreamReadYield(stream.ID, size)
	l.Push(yield)
	return -1
}

func streamCloseMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	yield := AcquireStreamCloseYield(stream.ID)
	l.Push(yield)
	return -1
}

func streamWriteMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	data := l.CheckString(2)
	yield := AcquireStreamWriteYield(stream.ID, []byte(data))
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

	yield := AcquireStreamSeekYield(stream.ID, offset, whence)
	l.Push(yield)
	return -1
}

func streamFlushMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	yield := AcquireStreamFlushYield(stream.ID)
	l.Push(yield)
	return -1
}

func streamStatMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	yield := AcquireStreamStatYield(stream.ID)
	l.Push(yield)
	return -1
}
