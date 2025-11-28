package engine2

import (
	"fmt"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	clockapi "github.com/wippyai/runtime/api/dispatcher/clock"
	lua "github.com/yuin/gopher-lua"
)

// SleepYield is yielded by time.sleep to pause execution.
type SleepYield struct {
	Duration time.Duration
}

var sleepYieldPool = sync.Pool{
	New: func() interface{} { return &SleepYield{} },
}

func acquireSleepYield(d time.Duration) *SleepYield {
	y := sleepYieldPool.Get().(*SleepYield)
	y.Duration = d
	return y
}

func ReleaseSleepYield(y *SleepYield) {
	y.Duration = 0
	sleepYieldPool.Put(y)
}

func (y *SleepYield) String() string       { return "<sleep_yield>" }
func (y *SleepYield) Type() lua.LValueType { return lua.LTUserData }

func (y *SleepYield) CmdID() dispatcher.CommandID {
	return clockapi.CmdSleep
}

func (y *SleepYield) ToCommand() dispatcher.Command {
	return clockapi.SleepCmd{Duration: y.Duration}
}

// TimerYield is yielded by time.timer for one-shot timer with time result.
type TimerYield struct {
	Duration time.Duration
}

var timerYieldPool = sync.Pool{
	New: func() interface{} { return &TimerYield{} },
}

func acquireTimerYield(d time.Duration) *TimerYield {
	y := timerYieldPool.Get().(*TimerYield)
	y.Duration = d
	return y
}

func ReleaseTimerYield(y *TimerYield) {
	y.Duration = 0
	timerYieldPool.Put(y)
}

func (y *TimerYield) String() string       { return "<timer_yield>" }
func (y *TimerYield) Type() lua.LValueType { return lua.LTUserData }

func (y *TimerYield) CmdID() dispatcher.CommandID {
	return clockapi.CmdTimer
}

func (y *TimerYield) ToCommand() dispatcher.Command {
	return clockapi.TimerCmd{Duration: y.Duration}
}

// TickerYield is the legacy streaming ticker (deprecated).
type TickerYield struct {
	Duration time.Duration
}

// TickerStartYield is yielded to create a new ticker.
type TickerStartYield struct {
	Duration time.Duration
}

var tickerStartYieldPool = sync.Pool{
	New: func() interface{} { return &TickerStartYield{} },
}

func acquireTickerStartYield(d time.Duration) *TickerStartYield {
	y := tickerStartYieldPool.Get().(*TickerStartYield)
	y.Duration = d
	return y
}

func ReleaseTickerStartYield(y *TickerStartYield) {
	y.Duration = 0
	tickerStartYieldPool.Put(y)
}

func (y *TickerStartYield) String() string       { return "<ticker_start_yield>" }
func (y *TickerStartYield) Type() lua.LValueType { return lua.LTUserData }

func (y *TickerStartYield) CmdID() dispatcher.CommandID {
	return clockapi.CmdTickerStart
}

func (y *TickerStartYield) ToCommand() dispatcher.Command {
	return clockapi.TickerStartCmd{Duration: y.Duration}
}

// TickerNextYield is yielded to wait for the next tick.
type TickerNextYield struct {
	TickerID uint64
}

var tickerNextYieldPool = sync.Pool{
	New: func() interface{} { return &TickerNextYield{} },
}

func acquireTickerNextYield(id uint64) *TickerNextYield {
	y := tickerNextYieldPool.Get().(*TickerNextYield)
	y.TickerID = id
	return y
}

func ReleaseTickerNextYield(y *TickerNextYield) {
	y.TickerID = 0
	tickerNextYieldPool.Put(y)
}

func (y *TickerNextYield) String() string       { return "<ticker_next_yield>" }
func (y *TickerNextYield) Type() lua.LValueType { return lua.LTUserData }

func (y *TickerNextYield) CmdID() dispatcher.CommandID {
	return clockapi.CmdTickerNext
}

func (y *TickerNextYield) ToCommand() dispatcher.Command {
	return clockapi.TickerNextCmd{TickerID: y.TickerID}
}

// TickerStopYield is yielded to stop a ticker.
type TickerStopYield struct {
	TickerID uint64
}

var tickerStopYieldPool = sync.Pool{
	New: func() interface{} { return &TickerStopYield{} },
}

func acquireTickerStopYield(id uint64) *TickerStopYield {
	y := tickerStopYieldPool.Get().(*TickerStopYield)
	y.TickerID = id
	return y
}

func ReleaseTickerStopYield(y *TickerStopYield) {
	y.TickerID = 0
	tickerStopYieldPool.Put(y)
}

func (y *TickerStopYield) String() string       { return "<ticker_stop_yield>" }
func (y *TickerStopYield) Type() lua.LValueType { return lua.LTUserData }

func (y *TickerStopYield) CmdID() dispatcher.CommandID {
	return clockapi.CmdTickerStop
}

func (y *TickerStopYield) ToCommand() dispatcher.Command {
	return clockapi.TickerStopCmd{TickerID: y.TickerID}
}

// NowYield is yielded by time.now to get current time from dispatcher.
// Allows Temporal to provide deterministic workflow time.
type NowYield struct{}

var nowYieldSingleton = &NowYield{}

func (y *NowYield) String() string       { return "<now_yield>" }
func (y *NowYield) Type() lua.LValueType { return lua.LTUserData }

func (y *NowYield) CmdID() dispatcher.CommandID {
	return clockapi.CmdNow
}

func (y *NowYield) ToCommand() dispatcher.Command {
	return clockapi.NowCmd{}
}

var tickerYieldPool = sync.Pool{
	New: func() interface{} { return &TickerYield{} },
}

func acquireTickerYield(d time.Duration) *TickerYield {
	y := tickerYieldPool.Get().(*TickerYield)
	y.Duration = d
	return y
}

func ReleaseTickerYield(y *TickerYield) {
	y.Duration = 0
	tickerYieldPool.Put(y)
}

func (y *TickerYield) String() string       { return "<ticker_yield>" }
func (y *TickerYield) Type() lua.LValueType { return lua.LTUserData }

func (y *TickerYield) CmdID() dispatcher.CommandID {
	return clockapi.CmdTicker
}

func (y *TickerYield) ToCommand() dispatcher.Command {
	return clockapi.TickerCmd{Duration: y.Duration}
}

// YieldConverter converts Lua yield values to scheduler commands.
type YieldConverter interface {
	ToCommand() dispatcher.Command
}

// ConvertYieldToCommand attempts to convert a Lua yield value to a scheduler command.
func ConvertYieldToCommand(value lua.LValue) dispatcher.Command {
	if converter, ok := value.(YieldConverter); ok {
		return converter.ToCommand()
	}
	return nil
}

// BindTimeSleep binds the time module with sleep, timer, and now functions.
func BindTimeSleep(l *lua.LState) {
	timeModule := l.GetGlobal("time")
	if timeModule == lua.LNil {
		timeModule = l.NewTable()
		l.SetGlobal("time", timeModule)
	}
	timeTbl := timeModule.(*lua.LTable)

	// Duration constants (nanoseconds)
	l.SetField(timeTbl, "NANOSECOND", lua.LNumber(time.Nanosecond))
	l.SetField(timeTbl, "MICROSECOND", lua.LNumber(time.Microsecond))
	l.SetField(timeTbl, "MILLISECOND", lua.LNumber(time.Millisecond))
	l.SetField(timeTbl, "SECOND", lua.LNumber(time.Second))
	l.SetField(timeTbl, "MINUTE", lua.LNumber(time.Minute))
	l.SetField(timeTbl, "HOUR", lua.LNumber(time.Hour))

	// time.sleep(duration) - yields to scheduler
	l.SetField(timeTbl, "sleep", l.NewFunction(func(l *lua.LState) int {
		duration, err := parseDuration(l, 1)
		if err != nil {
			l.RaiseError("time.sleep: %s", err.Error())
			return 0
		}
		yield := acquireSleepYield(duration)
		l.Push(yield)
		return -1
	}))

	// time.timer(duration) - yields to scheduler, returns fire time
	l.SetField(timeTbl, "timer", l.NewFunction(func(l *lua.LState) int {
		duration, err := parseDuration(l, 1)
		if err != nil {
			l.RaiseError("time.timer: %s", err.Error())
			return 0
		}
		yield := acquireTimerYield(duration)
		l.Push(yield)
		return -1
	}))

	// time.now() - yields to get current time (supports deterministic workflow time)
	l.SetField(timeTbl, "now", l.NewFunction(func(l *lua.LState) int {
		l.Push(nowYieldSingleton)
		return -1
	}))

}

// parseDuration parses a duration from Lua argument.
func parseDuration(l *lua.LState, idx int) (time.Duration, error) {
	arg := l.Get(idx)
	switch v := arg.(type) {
	case lua.LNumber:
		return time.Duration(v), nil
	case lua.LString:
		return time.ParseDuration(string(v))
	default:
		return 0, fmt.Errorf("duration: number or string expected")
	}
}

// Ticker is the Lua userdata for streaming ticker operations.
type Ticker struct {
	ID uint64
}

const tickerTypeName = "Ticker"

// BindTimeTicker binds the streaming ticker functions to the time module.
func BindTimeTicker(l *lua.LState) {
	timeModule := l.GetGlobal("time")
	if timeModule == lua.LNil {
		timeModule = l.NewTable()
		l.SetGlobal("time", timeModule)
	}
	timeTbl := timeModule.(*lua.LTable)

	// Register Ticker metatable
	mt := l.NewTypeMetatable(tickerTypeName)
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), tickerMethods))

	// Helper: __ticker_start_yield(duration) - returns yield object
	l.SetGlobal("__ticker_start_yield", l.NewFunction(func(l *lua.LState) int {
		duration, err := parseDuration(l, 1)
		if err != nil {
			l.RaiseError("time.ticker: %s", err.Error())
			return 0
		}
		yield := acquireTickerStartYield(duration)
		l.Push(yield)
		return 1
	}))

	// Helper: __ticker_new(id) - creates Ticker userdata
	l.SetGlobal("__ticker_new", l.NewFunction(func(l *lua.LState) int {
		id := uint64(l.CheckNumber(1))
		ticker := &Ticker{ID: id}
		ud := l.NewUserData()
		ud.Value = ticker
		l.SetMetatable(ud, l.GetTypeMetatable(tickerTypeName))
		l.Push(ud)
		return 1
	}))

	// time.ticker(duration) - Lua wrapper that yields and creates ticker
	err := l.DoString(`
		function time.ticker(duration)
			local yield = __ticker_start_yield(duration)
			local id = coroutine.yield(yield)
			return __ticker_new(id)
		end
	`)
	if err != nil {
		panic(fmt.Sprintf("failed to load ticker wrapper: %v", err))
	}

	// Also expose low-level functions for advanced use
	l.SetField(timeTbl, "ticker_next", l.NewFunction(tickerNextFunc))
	l.SetField(timeTbl, "ticker_stop", l.NewFunction(tickerStopFunc))
}

var tickerMethods = map[string]lua.LGFunction{
	"next": tickerNextMethod,
	"stop": tickerStopMethod,
}

func checkTicker(l *lua.LState, idx int) *Ticker {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Ticker); ok {
		return v
	}
	l.ArgError(idx, "Ticker expected")
	return nil
}

// ticker:next() - yields to wait for next tick
func tickerNextMethod(l *lua.LState) int {
	ticker := checkTicker(l, 1)
	yield := acquireTickerNextYield(ticker.ID)
	l.Push(yield)
	return -1
}

// ticker:stop() - yields to stop the ticker
func tickerStopMethod(l *lua.LState) int {
	ticker := checkTicker(l, 1)
	yield := acquireTickerStopYield(ticker.ID)
	l.Push(yield)
	return -1
}

// time.ticker_next(id) - low-level function
func tickerNextFunc(l *lua.LState) int {
	id := uint64(l.CheckNumber(1))
	yield := acquireTickerNextYield(id)
	l.Push(yield)
	return -1
}

// time.ticker_stop(id) - low-level function
func tickerStopFunc(l *lua.LState) int {
	id := uint64(l.CheckNumber(1))
	yield := acquireTickerStopYield(id)
	l.Push(yield)
	return -1
}

// StreamReadYield is yielded to read a chunk from a stream.
type StreamReadYield struct {
	StreamID uint64
	Size     int64
}

var streamReadYieldPool = sync.Pool{
	New: func() interface{} { return &StreamReadYield{} },
}

func acquireStreamReadYield(id uint64, size int64) *StreamReadYield {
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
	return dispatcher.CommandID(50) // CmdStreamRead
}

func (y *StreamReadYield) ToCommand() dispatcher.Command {
	return streamReadCmd{StreamID: y.StreamID, Size: y.Size}
}

// streamReadCmd mirrors the API command for yield conversion.
type streamReadCmd struct {
	StreamID uint64
	Size     int64
}

func (c streamReadCmd) CmdID() dispatcher.CommandID {
	return dispatcher.CommandID(50)
}

// StreamCloseYield is yielded to close a stream.
type StreamCloseYield struct {
	StreamID uint64
}

var streamCloseYieldPool = sync.Pool{
	New: func() interface{} { return &StreamCloseYield{} },
}

func acquireStreamCloseYield(id uint64) *StreamCloseYield {
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
	return dispatcher.CommandID(51) // CmdStreamClose
}

func (y *StreamCloseYield) ToCommand() dispatcher.Command {
	return streamCloseCmd{StreamID: y.StreamID}
}

// streamCloseCmd mirrors the API command for yield conversion.
type streamCloseCmd struct {
	StreamID uint64
}

func (c streamCloseCmd) CmdID() dispatcher.CommandID {
	return dispatcher.CommandID(51)
}

// Stream is the Lua userdata for stream operations.
type Stream struct {
	ID uint64
}

const streamTypeName = "Stream"

// BindStream binds stream functions to Lua.
func BindStream(l *lua.LState) {
	// Register Stream metatable
	mt := l.NewTypeMetatable(streamTypeName)
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), streamMethods))

	// Helper: __stream_new(id) - creates Stream userdata
	l.SetGlobal("__stream_new", l.NewFunction(func(l *lua.LState) int {
		id := uint64(l.CheckNumber(1))
		stream := &Stream{ID: id}
		ud := l.NewUserData()
		ud.Value = stream
		l.SetMetatable(ud, l.GetTypeMetatable(streamTypeName))
		l.Push(ud)
		return 1
	}))
}

var streamMethods = map[string]lua.LGFunction{
	"read":  streamReadMethod,
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

// stream:read(size) - yields to read chunk, returns bytes or nil on EOF
func streamReadMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	size := int64(0)
	if l.GetTop() >= 2 {
		size = int64(l.CheckNumber(2))
	}
	yield := acquireStreamReadYield(stream.ID, size)
	l.Push(yield)
	return -1
}

// stream:close() - yields to close stream
func streamCloseMethod(l *lua.LState) int {
	stream := checkStream(l, 1)
	yield := acquireStreamCloseYield(stream.ID)
	l.Push(yield)
	return -1
}
