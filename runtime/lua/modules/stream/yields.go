// SPDX-License-Identifier: MPL-2.0

package stream

import (
	"context"
	"errors"
	"io"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime/resource"
	streamapi "github.com/wippyai/runtime/api/stream"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	streamsys "github.com/wippyai/runtime/system/stream"
)

// ReadYield is yielded to read a chunk from a stream.
type ReadYield struct {
	StreamID uint64
	Size     int64
}

var readYieldPool = sync.Pool{
	New: func() any { return &ReadYield{} },
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
	return streamapi.Read
}

func (y *ReadYield) ToCommand() dispatcher.Command {
	return streamapi.ReadCmd{StreamID: y.StreamID, Size: y.Size}
}

func (y *ReadYield) Release() { ReleaseReadYield(y) }

func (y *ReadYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "stream read").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if data == nil {
		return []lua.LValue{lua.LNil, lua.LNil}
	}
	if buf, ok := data.(*streamapi.Buffer); ok {
		result := lua.LString(buf.Bytes())
		buf.Release()
		return []lua.LValue{result, lua.LNil}
	}
	if bytes, ok := data.([]byte); ok {
		return []lua.LValue{lua.LString(bytes), lua.LNil}
	}
	luaErr := lua.NewLuaError(l, "invalid response type").
		WithKind(lua.Internal).
		WithRetryable(false)
	return []lua.LValue{lua.LNil, luaErr}
}

// CloseYield is yielded to close a stream.
type CloseYield struct {
	StreamID uint64
}

var closeYieldPool = sync.Pool{
	New: func() any { return &CloseYield{} },
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
	return streamapi.Close
}

func (y *CloseYield) ToCommand() dispatcher.Command {
	return streamapi.CloseCmd{StreamID: y.StreamID}
}

func (y *CloseYield) Release() { ReleaseCloseYield(y) }

func (y *CloseYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "stream close").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LFalse, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// WriteYield is yielded to write data to a stream.
type WriteYield struct {
	Data     []byte
	StreamID uint64
}

var writeYieldPool = sync.Pool{
	New: func() any { return &WriteYield{} },
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
	return streamapi.Write
}

func (y *WriteYield) ToCommand() dispatcher.Command {
	return streamapi.WriteCmd{StreamID: y.StreamID, Data: y.Data}
}

func (y *WriteYield) Release() { ReleaseWriteYield(y) }

func (y *WriteYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "stream write").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNumber(0), luaErr}
	}
	if n, ok := data.(int64); ok {
		return []lua.LValue{lua.LNumber(n), lua.LNil}
	}
	luaErr := lua.NewLuaError(l, "invalid response type").
		WithKind(lua.Internal).
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
	New: func() any { return &SeekYield{} },
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
	return streamapi.Seek
}

func (y *SeekYield) ToCommand() dispatcher.Command {
	return streamapi.SeekCmd{StreamID: y.StreamID, Offset: y.Offset, Whence: y.Whence}
}

func (y *SeekYield) Release() { ReleaseSeekYield(y) }

func (y *SeekYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "stream seek").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNumber(-1), luaErr}
	}
	if pos, ok := data.(int64); ok {
		return []lua.LValue{lua.LNumber(pos), lua.LNil}
	}
	luaErr := lua.NewLuaError(l, "invalid response type").
		WithKind(lua.Internal).
		WithRetryable(false)
	return []lua.LValue{lua.LNumber(-1), luaErr}
}

// FlushYield is yielded to flush a stream.
type FlushYield struct {
	StreamID uint64
}

var flushYieldPool = sync.Pool{
	New: func() any { return &FlushYield{} },
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
	return streamapi.Flush
}

func (y *FlushYield) ToCommand() dispatcher.Command {
	return streamapi.FlushCmd{StreamID: y.StreamID}
}

func (y *FlushYield) Release() { ReleaseFlushYield(y) }

func (y *FlushYield) HandleResult(l *lua.LState, _ any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "stream flush").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LFalse, luaErr}
	}
	return []lua.LValue{lua.LTrue, lua.LNil}
}

// StatYield is yielded to get stream info.
type StatYield struct {
	StreamID uint64
}

var statYieldPool = sync.Pool{
	New: func() any { return &StatYield{} },
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
	return streamapi.Stat
}

func (y *StatYield) ToCommand() dispatcher.Command {
	return streamapi.StatCmd{StreamID: y.StreamID}
}

func (y *StatYield) Release() { ReleaseStatYield(y) }

func (y *StatYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "stream stat").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	info, ok := data.(streamapi.Info)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
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

// GetReader implements fs.ReaderProvider interface.
func (s *Stream) GetReader(ctx context.Context) (io.Reader, error) {
	table := resource.GetTable(ctx)
	if table == nil {
		return nil, errors.New("no resource table available")
	}
	entry, err := streamsys.Get(table, s.ID)
	if err != nil {
		return nil, err
	}
	if !entry.Caps().Readable {
		return nil, errors.New("stream is not readable")
	}
	return entry.Reader(), nil
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

var streamMethods = map[string]lua.LGoFunc{
	"read":    streamReadMethod,
	"write":   streamWriteMethod,
	"seek":    streamSeekMethod,
	"flush":   streamFlushMethod,
	"stat":    streamStatMethod,
	"close":   streamCloseMethod,
	"scanner": streamScannerMethod,
}

func checkStream(l *lua.LState, _ int) *Stream {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Stream); ok {
		return v
	}
	l.ArgError(1, "Stream expected")
	return nil
}

func streamReadMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	if stream == nil {
		return 0
	}
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
	if stream == nil {
		return 0
	}
	yield := AcquireCloseYield(stream.ID)
	l.Push(yield)
	return -1
}

func streamWriteMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	if stream == nil {
		return 0
	}
	data := l.CheckString(2)
	yield := AcquireWriteYield(stream.ID, []byte(data))
	l.Push(yield)
	return -1
}

func streamSeekMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	if stream == nil {
		return 0
	}
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
	if stream == nil {
		return 0
	}
	yield := AcquireFlushYield(stream.ID)
	l.Push(yield)
	return -1
}

func streamStatMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	if stream == nil {
		return 0
	}
	yield := AcquireStatYield(stream.ID)
	l.Push(yield)
	return -1
}

func streamScannerMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	if stream == nil {
		return 0
	}

	splitType := streamapi.SplitLines
	if l.GetTop() >= 2 {
		splitStr := l.CheckString(2)
		switch splitStr {
		case "lines":
			splitType = streamapi.SplitLines
		case "words":
			splitType = streamapi.SplitWords
		case "bytes":
			splitType = streamapi.SplitBytes
		case "runes":
			splitType = streamapi.SplitRunes
		default:
			l.ArgError(2, "invalid split type: must be 'lines', 'words', 'bytes', or 'runes'")
			return 0
		}
	}

	yield := AcquireScannerCreateYield(stream.ID, splitType)
	l.Push(yield)
	return -1
}

// ScannerCreateYield is yielded to create a scanner from a stream.
type ScannerCreateYield struct {
	StreamID  uint64
	SplitType int
}

var scannerCreateYieldPool = sync.Pool{
	New: func() any { return &ScannerCreateYield{} },
}

func AcquireScannerCreateYield(streamID uint64, splitType int) *ScannerCreateYield {
	y := scannerCreateYieldPool.Get().(*ScannerCreateYield)
	y.StreamID = streamID
	y.SplitType = splitType
	return y
}

func ReleaseScannerCreateYield(y *ScannerCreateYield) {
	y.StreamID = 0
	y.SplitType = 0
	scannerCreateYieldPool.Put(y)
}

func (y *ScannerCreateYield) String() string       { return "<scanner_create_yield>" }
func (y *ScannerCreateYield) Type() lua.LValueType { return lua.LTUserData }

func (y *ScannerCreateYield) CmdID() dispatcher.CommandID {
	return streamapi.ScannerCreate
}

func (y *ScannerCreateYield) ToCommand() dispatcher.Command {
	return streamapi.ScannerCreateCmd{StreamID: y.StreamID, SplitType: y.SplitType}
}

func (y *ScannerCreateYield) Release() { ReleaseScannerCreateYield(y) }

func (y *ScannerCreateYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "scanner create").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	scannerID, ok := data.(uint64)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{NewScanner(l, scannerID), lua.LNil}
}

// ScannerScanYield is yielded to scan next token.
type ScannerScanYield struct {
	scanner   *Scanner
	ScannerID uint64
}

var scannerScanYieldPool = sync.Pool{
	New: func() any { return &ScannerScanYield{} },
}

func AcquireScannerScanYield(scannerID uint64, scanner *Scanner) *ScannerScanYield {
	y := scannerScanYieldPool.Get().(*ScannerScanYield)
	y.ScannerID = scannerID
	y.scanner = scanner
	return y
}

func ReleaseScannerScanYield(y *ScannerScanYield) {
	y.ScannerID = 0
	y.scanner = nil
	scannerScanYieldPool.Put(y)
}

func (y *ScannerScanYield) String() string       { return "<scanner_scan_yield>" }
func (y *ScannerScanYield) Type() lua.LValueType { return lua.LTUserData }

func (y *ScannerScanYield) CmdID() dispatcher.CommandID {
	return streamapi.ScannerScan
}

func (y *ScannerScanYield) ToCommand() dispatcher.Command {
	return streamapi.ScannerScanCmd{ScannerID: y.ScannerID}
}

func (y *ScannerScanYield) Release() { ReleaseScannerScanYield(y) }

func (y *ScannerScanYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "scanner scan").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LFalse, luaErr}
	}
	result, ok := data.(streamapi.ScanResult)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid response type").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LFalse, luaErr}
	}
	// Update scanner state for text() and err() methods
	if y.scanner != nil {
		y.scanner.lastText = result.Text
		y.scanner.lastErr = result.Error
	}
	return []lua.LValue{lua.LBool(result.HasToken), lua.LNil}
}

// Scanner is the Lua userdata for scanner operations.
type Scanner struct {
	lastText string
	lastErr  string
	ID       uint64
}

const scannerTypeName = "stream.Scanner"

var (
	scannerMetatableOnce sync.Once
	scannerMetatable     *lua.LTable
)

func registerScannerMetatable() {
	scannerMetatableOnce.Do(func() {
		scannerMetatable = value.RegisterTypeMethods(nil, scannerTypeName, nil, scannerMethods)
	})
}

// NewScanner creates a Scanner userdata from an ID.
func NewScanner(l *lua.LState, id uint64) lua.LValue {
	registerScannerMetatable()
	ud := l.NewUserData()
	ud.Value = &Scanner{ID: id}
	ud.Metatable = scannerMetatable
	return ud
}

var scannerMethods = map[string]lua.LGoFunc{
	"scan": scannerScanMethod,
	"text": scannerTextMethod,
	"err":  scannerErrMethod,
}

func checkScanner(l *lua.LState, _ int) *Scanner {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Scanner); ok {
		return v
	}
	l.ArgError(1, "Scanner expected")
	return nil
}

func scannerScanMethod(l *lua.LState) int {
	scanner := checkScanner(l, 1)
	if scanner == nil {
		return 0
	}
	yield := AcquireScannerScanYield(scanner.ID, scanner)
	l.Push(yield)
	return -1
}

func scannerTextMethod(l *lua.LState) int {
	scanner := checkScanner(l, 1)
	if scanner == nil {
		return 0
	}
	l.Push(lua.LString(scanner.lastText))
	return 1
}

func scannerErrMethod(l *lua.LState) int {
	scanner := checkScanner(l, 1)
	if scanner == nil {
		return 0
	}
	if scanner.lastErr == "" {
		l.Push(lua.LNil)
	} else {
		l.Push(lua.LString(scanner.lastErr))
	}
	return 1
}
