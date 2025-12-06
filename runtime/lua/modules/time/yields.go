package time

import (
	"fmt"
	"sync"
	stdtime "time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const (
	tickerTypeName        = "time.Ticker"
	tickerChannelTypeName = "time.TickerChannel"
	timerTypeName         = "time.Timer"
	timerChannelTypeName  = "time.TimerChannel"
	timeTypeName          = "time.Time"
	durationTypeName      = "time.Duration"
	locationTypeName      = "time.Location"
)

// Error helpers for structured errors

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.KindInvalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.KindInternal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func wrapErrorValue(l *lua.LState, goErr error, context string) lua.LValue {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.KindInternal).
		WithRetryable(false)
	return err
}

func newErrorValue(l *lua.LState, msg string) lua.LValue {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.KindInvalid).
		WithRetryable(false)
	return err
}

var (
	moduleTable *lua.LTable
	yieldTypes  []luaapi.YieldType
)

func init() {
	// Register type metatables (uses value package's global cache)
	registerTimeMethods()
	registerDurationMethods()
	registerLocationMethods()
	registerTickerMethods()
	registerTickerChannelMethods()
	registerTimerMethods()
	registerTimerChannelMethods()

	// Create module table
	initModuleTable()

	// Setup yield types
	yieldTypes = []luaapi.YieldType{
		{Sample: &SleepYield{}, CmdID: clockapi.Sleep},
		{Sample: &TimerStartYield{}, CmdID: clockapi.TimerStart},
		{Sample: &AfterStartYield{}, CmdID: clockapi.TimerStart},
		{Sample: &TimerWaitYield{}, CmdID: clockapi.TimerWait},
		{Sample: &TimerStopYield{}, CmdID: clockapi.TimerStop},
		{Sample: &TimerResetYield{}, CmdID: clockapi.TimerReset},
		{Sample: &TickerStartYield{}, CmdID: clockapi.TickerStart},
		{Sample: &TickerNextYield{}, CmdID: clockapi.TickerNext},
		{Sample: &TickerStopYield{}, CmdID: clockapi.TickerStop},
	}
}

// Module is the time module definition.
var Module = &luaapi.ModuleDef{
	Name:        "time",
	Description: "Time operations, scheduling, and duration handling",
	Class:       []string{luaapi.ClassTime, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		return moduleTable, yieldTypes
	},
}

// initModuleTable creates the immutable module table.
func initModuleTable() {
	mod := lua.CreateTable(0, 40)

	// Duration constants (nanoseconds)
	mod.RawSetString("NANOSECOND", lua.LNumber(stdtime.Nanosecond))
	mod.RawSetString("MICROSECOND", lua.LNumber(stdtime.Microsecond))
	mod.RawSetString("MILLISECOND", lua.LNumber(stdtime.Millisecond))
	mod.RawSetString("SECOND", lua.LNumber(stdtime.Second))
	mod.RawSetString("MINUTE", lua.LNumber(stdtime.Minute))
	mod.RawSetString("HOUR", lua.LNumber(stdtime.Hour))

	// Format constants
	mod.RawSetString("RFC3339", lua.LString(stdtime.RFC3339))
	mod.RawSetString("RFC3339NANO", lua.LString(stdtime.RFC3339Nano))
	mod.RawSetString("RFC822", lua.LString(stdtime.RFC822))
	mod.RawSetString("RFC822Z", lua.LString(stdtime.RFC822Z))
	mod.RawSetString("RFC850", lua.LString(stdtime.RFC850))
	mod.RawSetString("RFC1123", lua.LString(stdtime.RFC1123))
	mod.RawSetString("RFC1123Z", lua.LString(stdtime.RFC1123Z))
	mod.RawSetString("KITCHEN", lua.LString(stdtime.Kitchen))
	mod.RawSetString("STAMP", lua.LString(stdtime.Stamp))
	mod.RawSetString("STAMP_MILLI", lua.LString(stdtime.StampMilli))
	mod.RawSetString("STAMP_MICRO", lua.LString(stdtime.StampMicro))
	mod.RawSetString("STAMP_NANO", lua.LString(stdtime.StampNano))
	mod.RawSetString("DATE_TIME", lua.LString("2006-01-02 15:04:05"))
	mod.RawSetString("DATE_ONLY", lua.LString("2006-01-02"))
	mod.RawSetString("TIME_ONLY", lua.LString("15:04:05"))

	// Month constants
	mod.RawSetString("JANUARY", lua.LNumber(1))
	mod.RawSetString("FEBRUARY", lua.LNumber(2))
	mod.RawSetString("MARCH", lua.LNumber(3))
	mod.RawSetString("APRIL", lua.LNumber(4))
	mod.RawSetString("MAY", lua.LNumber(5))
	mod.RawSetString("JUNE", lua.LNumber(6))
	mod.RawSetString("JULY", lua.LNumber(7))
	mod.RawSetString("AUGUST", lua.LNumber(8))
	mod.RawSetString("SEPTEMBER", lua.LNumber(9))
	mod.RawSetString("OCTOBER", lua.LNumber(10))
	mod.RawSetString("NOVEMBER", lua.LNumber(11))
	mod.RawSetString("DECEMBER", lua.LNumber(12))

	// Weekday constants
	mod.RawSetString("SUNDAY", lua.LNumber(0))
	mod.RawSetString("MONDAY", lua.LNumber(1))
	mod.RawSetString("TUESDAY", lua.LNumber(2))
	mod.RawSetString("WEDNESDAY", lua.LNumber(3))
	mod.RawSetString("THURSDAY", lua.LNumber(4))
	mod.RawSetString("FRIDAY", lua.LNumber(5))
	mod.RawSetString("SATURDAY", lua.LNumber(6))

	// Location constants - direct struct allocation
	utcUD := &lua.LUserData{
		Value:     utcLocation,
		Metatable: value.GetTypeMetatable(nil, locationTypeName),
	}
	mod.RawSetString("utc", utcUD)

	localUD := &lua.LUserData{
		Value:     localLocation,
		Metatable: value.GetTypeMetatable(nil, locationTypeName),
	}
	mod.RawSetString("localtz", localUD)

	// Yielding functions - LGoFunc is stateless
	mod.RawSetString("sleep", lua.LGoFunc(sleepFunc))
	mod.RawSetString("timer", lua.LGoFunc(timerFunc))
	mod.RawSetString("after", lua.LGoFunc(afterFunc))
	mod.RawSetString("now", lua.LGoFunc(nowFunc))

	// Ticker function
	mod.RawSetString("ticker", lua.LGoFunc(tickerFunc))

	// Non-yielding functions
	mod.RawSetString("date", lua.LGoFunc(dateFunc))
	mod.RawSetString("unix", lua.LGoFunc(unixFunc))
	mod.RawSetString("parse", lua.LGoFunc(parseFunc))
	mod.RawSetString("parse_duration", lua.LGoFunc(parseDurationFunc))
	mod.RawSetString("load_location", lua.LGoFunc(loadLocationFunc))
	mod.RawSetString("fixed_zone", lua.LGoFunc(fixedZoneFunc))

	mod.Immutable = true
	moduleTable = mod
}

// Type registration using value package

func registerTimeMethods() {
	value.RegisterTypeMethods(nil, timeTypeName,
		map[string]lua.LGoFunc{
			"__tostring": timeToString,
		},
		map[string]lua.LGoFunc{
			"add":            timeAdd,
			"sub":            timeSub,
			"add_date":       timeAddDate,
			"after":          timeAfter,
			"before":         timeBefore,
			"equal":          timeEqual,
			"format":         timeFormat,
			"format_rfc3339": timeFormatRFC3339,
			"unix":           timeUnix,
			"unix_nano":      timeUnixNano,
			"date":           timeDate,
			"clock":          timeClock,
			"year":           timeYear,
			"month":          timeMonth,
			"day":            timeDay,
			"hour":           timeHour,
			"minute":         timeMinute,
			"second":         timeSecond,
			"nanosecond":     timeNanosecond,
			"weekday":        timeWeekday,
			"year_day":       timeYearDay,
			"is_zero":        timeIsZero,
			"in_location":    timeIn,
			"location":       timeLocation,
			"utc":            timeUTC,
			"in_local":       timeLocal,
			"round":          timeRound,
			"truncate":       timeTruncate,
		},
	)
}

func registerDurationMethods() {
	value.RegisterTypeMethods(nil, durationTypeName,
		map[string]lua.LGoFunc{
			"__tostring": durationToString,
		},
		map[string]lua.LGoFunc{
			"nanoseconds":  durationNanoseconds,
			"microseconds": durationMicroseconds,
			"milliseconds": durationMilliseconds,
			"seconds":      durationSeconds,
			"minutes":      durationMinutes,
			"hours":        durationHours,
		},
	)
}

func registerLocationMethods() {
	value.RegisterTypeMethods(nil, locationTypeName,
		map[string]lua.LGoFunc{
			"__tostring": locationString,
		},
		map[string]lua.LGoFunc{
			"string": locationString,
		},
	)
}

func registerTickerMethods() {
	value.RegisterMethods(nil, tickerTypeName, map[string]lua.LGoFunc{
		"stop":    tickerStopMethod,
		"channel": tickerChannelMethod,
	})
}

// Yield types - pooled for zero-allocation in hot paths

// SleepYield is yielded by time.sleep to pause execution.
type SleepYield struct {
	Duration stdtime.Duration
}

var sleepYieldPool = sync.Pool{
	New: func() interface{} { return &SleepYield{} },
}

func acquireSleepYield(d stdtime.Duration) *SleepYield {
	y := sleepYieldPool.Get().(*SleepYield)
	y.Duration = d
	return y
}

func ReleaseSleepYield(y *SleepYield) {
	y.Duration = 0
	sleepYieldPool.Put(y)
}

func (y *SleepYield) Release()                      { ReleaseSleepYield(y) }
func (y *SleepYield) String() string                { return "<sleep_yield>" }
func (y *SleepYield) Type() lua.LValueType          { return lua.LTUserData }
func (y *SleepYield) CmdID() dispatcher.CommandID   { return clockapi.Sleep }
func (y *SleepYield) ToCommand() dispatcher.Command { return clockapi.SleepCmd{Duration: y.Duration} }

// TimerStartYield is yielded to create a new timer.
type TimerStartYield struct {
	Duration stdtime.Duration
}

var timerStartYieldPool = sync.Pool{
	New: func() interface{} { return &TimerStartYield{} },
}

func acquireTimerStartYield(d stdtime.Duration) *TimerStartYield {
	y := timerStartYieldPool.Get().(*TimerStartYield)
	y.Duration = d
	return y
}

func ReleaseTimerStartYield(y *TimerStartYield) {
	y.Duration = 0
	timerStartYieldPool.Put(y)
}

func (y *TimerStartYield) Release()                    { ReleaseTimerStartYield(y) }
func (y *TimerStartYield) String() string              { return "<timer_start_yield>" }
func (y *TimerStartYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *TimerStartYield) CmdID() dispatcher.CommandID { return clockapi.TimerStart }
func (y *TimerStartYield) ToCommand() dispatcher.Command {
	return clockapi.TimerStartCmd{Duration: y.Duration}
}

// HandleResult implements HandledYield to convert timer ID to Timer userdata.
func (y *TimerStartYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapErrorValue(l, err, "timer start")}
	}

	id, ok := data.(uint64)
	if !ok {
		return []lua.LValue{lua.LNil, newErrorValue(l, "invalid timer ID type")}
	}

	// Create timer channel that yields TimerWaitYield on receive
	timerCh := &TimerChannel{TimerID: id}
	timer := &Timer{ID: id, Channel: timerCh}

	ud := l.NewUserData()
	ud.Value = timer
	ud.Metatable = value.GetTypeMetatable(l, timerTypeName)
	return []lua.LValue{ud}
}

// AfterStartYield is yielded by time.after() to create a timer channel.
// Uses Sleep command and sends time.Now() to channel when complete.
// The channel is a standard engine.Channel that works with channel.select.
type AfterStartYield struct {
	Duration stdtime.Duration
	Channel  *engine.Channel
}

var afterStartYieldPool = sync.Pool{
	New: func() interface{} { return &AfterStartYield{} },
}

func acquireAfterStartYield(d stdtime.Duration, ch *engine.Channel) *AfterStartYield {
	y := afterStartYieldPool.Get().(*AfterStartYield)
	y.Duration = d
	y.Channel = ch
	return y
}

func ReleaseAfterStartYield(y *AfterStartYield) {
	y.Duration = 0
	y.Channel = nil
	afterStartYieldPool.Put(y)
}

func (y *AfterStartYield) Release()                    { ReleaseAfterStartYield(y) }
func (y *AfterStartYield) String() string              { return "<after_start_yield>" }
func (y *AfterStartYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *AfterStartYield) CmdID() dispatcher.CommandID { return clockapi.Sleep }
func (y *AfterStartYield) ToCommand() dispatcher.Command {
	return clockapi.SleepCmd{Duration: y.Duration}
}

// HandleResult implements HandledYield. Called when sleep completes.
// Sends time.Now() to the channel and returns the channel.
func (y *AfterStartYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapErrorValue(l, err, "time.after")}
	}

	// Get current time
	var t stdtime.Time
	if ref := clockapi.GetTimeReference(l.Context()); ref != nil {
		t = ref.Now()
	} else {
		t = stdtime.Now()
	}

	// Create time userdata
	timeUD := l.NewUserData()
	timeUD.Value = &Time{time: t}
	timeUD.Metatable = value.GetTypeMetatable(l, timeTypeName)

	// Send time to channel (buffered, won't block)
	y.Channel.Send(nil, timeUD, nil)

	// Return the channel
	return []lua.LValue{y.Channel.Value()}
}

// TimerWaitYield is yielded to wait for timer to fire.
type TimerWaitYield struct {
	TimerID uint64
}

var timerWaitYieldPool = sync.Pool{
	New: func() interface{} { return &TimerWaitYield{} },
}

func acquireTimerWaitYield(id uint64) *TimerWaitYield {
	y := timerWaitYieldPool.Get().(*TimerWaitYield)
	y.TimerID = id
	return y
}

func ReleaseTimerWaitYield(y *TimerWaitYield) {
	y.TimerID = 0
	timerWaitYieldPool.Put(y)
}

func (y *TimerWaitYield) Release()                    { ReleaseTimerWaitYield(y) }
func (y *TimerWaitYield) String() string              { return "<timer_wait_yield>" }
func (y *TimerWaitYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *TimerWaitYield) CmdID() dispatcher.CommandID { return clockapi.TimerWait }
func (y *TimerWaitYield) ToCommand() dispatcher.Command {
	return clockapi.TimerWaitCmd{TimerID: y.TimerID}
}

// TimerStopYield is yielded to stop a timer.
type TimerStopYield struct {
	TimerID uint64
}

var timerStopYieldPool = sync.Pool{
	New: func() interface{} { return &TimerStopYield{} },
}

func acquireTimerStopYield(id uint64) *TimerStopYield {
	y := timerStopYieldPool.Get().(*TimerStopYield)
	y.TimerID = id
	return y
}

func ReleaseTimerStopYield(y *TimerStopYield) {
	y.TimerID = 0
	timerStopYieldPool.Put(y)
}

func (y *TimerStopYield) Release()                    { ReleaseTimerStopYield(y) }
func (y *TimerStopYield) String() string              { return "<timer_stop_yield>" }
func (y *TimerStopYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *TimerStopYield) CmdID() dispatcher.CommandID { return clockapi.TimerStop }
func (y *TimerStopYield) ToCommand() dispatcher.Command {
	return clockapi.TimerStopCmd{TimerID: y.TimerID}
}

// HandleResult converts bool result to Lua boolean.
func (y *TimerStopYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LFalse}
	}
	if stopped, ok := data.(bool); ok && stopped {
		return []lua.LValue{lua.LTrue}
	}
	return []lua.LValue{lua.LFalse}
}

// TimerResetYield is yielded to reset a timer with a new duration.
type TimerResetYield struct {
	TimerID  uint64
	Duration stdtime.Duration
}

var timerResetYieldPool = sync.Pool{
	New: func() interface{} { return &TimerResetYield{} },
}

func acquireTimerResetYield(id uint64, d stdtime.Duration) *TimerResetYield {
	y := timerResetYieldPool.Get().(*TimerResetYield)
	y.TimerID = id
	y.Duration = d
	return y
}

func ReleaseTimerResetYield(y *TimerResetYield) {
	y.TimerID = 0
	y.Duration = 0
	timerResetYieldPool.Put(y)
}

func (y *TimerResetYield) Release()                    { ReleaseTimerResetYield(y) }
func (y *TimerResetYield) String() string              { return "<timer_reset_yield>" }
func (y *TimerResetYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *TimerResetYield) CmdID() dispatcher.CommandID { return clockapi.TimerReset }
func (y *TimerResetYield) ToCommand() dispatcher.Command {
	return clockapi.TimerResetCmd{TimerID: y.TimerID, Duration: y.Duration}
}

// TickerStartYield is yielded to create a new ticker.
type TickerStartYield struct {
	Duration stdtime.Duration
}

var tickerStartYieldPool = sync.Pool{
	New: func() interface{} { return &TickerStartYield{} },
}

func acquireTickerStartYield(d stdtime.Duration) *TickerStartYield {
	y := tickerStartYieldPool.Get().(*TickerStartYield)
	y.Duration = d
	return y
}

func ReleaseTickerStartYield(y *TickerStartYield) {
	y.Duration = 0
	tickerStartYieldPool.Put(y)
}

func (y *TickerStartYield) Release()                    { ReleaseTickerStartYield(y) }
func (y *TickerStartYield) String() string              { return "<ticker_start_yield>" }
func (y *TickerStartYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *TickerStartYield) CmdID() dispatcher.CommandID { return clockapi.TickerStart }
func (y *TickerStartYield) ToCommand() dispatcher.Command {
	return clockapi.TickerStartCmd{Duration: y.Duration}
}

// HandleResult implements HandledYield to convert ticker ID to Ticker userdata.
// Returns a Ticker with a TickerChannel that yields on receive operations.
func (y *TickerStartYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapErrorValue(l, err, "ticker start")}
	}

	id, ok := data.(uint64)
	if !ok {
		return []lua.LValue{lua.LNil, newErrorValue(l, "invalid ticker ID type")}
	}

	// Create ticker channel that yields TickerNextYield on receive
	tickerCh := &TickerChannel{TickerID: id}
	ticker := &Ticker{ID: id, Channel: tickerCh}

	ud := l.NewUserData()
	ud.Value = ticker
	ud.Metatable = value.GetTypeMetatable(l, tickerTypeName)
	return []lua.LValue{ud}
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

func (y *TickerNextYield) Release()                    { ReleaseTickerNextYield(y) }
func (y *TickerNextYield) String() string              { return "<ticker_next_yield>" }
func (y *TickerNextYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *TickerNextYield) CmdID() dispatcher.CommandID { return clockapi.TickerNext }
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

func (y *TickerStopYield) Release()                    { ReleaseTickerStopYield(y) }
func (y *TickerStopYield) String() string              { return "<ticker_stop_yield>" }
func (y *TickerStopYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *TickerStopYield) CmdID() dispatcher.CommandID { return clockapi.TickerStop }
func (y *TickerStopYield) ToCommand() dispatcher.Command {
	return clockapi.TickerStopCmd{TickerID: y.TickerID}
}

// Ticker is the Lua userdata for ticker operations.
type Ticker struct {
	ID      uint64
	Channel *TickerChannel
}

// TickerChannel is a channel-like type that yields TickerNextYield on receive.
type TickerChannel struct {
	TickerID uint64
}

// tickerChannelReceive yields TickerNextYield to wait for next tick.
func tickerChannelReceive(l *lua.LState) int {
	ud := l.CheckUserData(1)
	ch, ok := ud.Value.(*TickerChannel)
	if !ok {
		l.ArgError(1, "ticker channel expected")
		return 0
	}
	yield := acquireTickerNextYield(ch.TickerID)
	l.Push(yield)
	return -1
}

func registerTickerChannelMethods() {
	value.RegisterMethods(nil, tickerChannelTypeName, map[string]lua.LGoFunc{
		"receive": tickerChannelReceive,
	})
}

// Timer is the Lua userdata for timer operations.
type Timer struct {
	ID      uint64
	Channel *TimerChannel
}

// TimerChannel is a channel-like type that yields TimerWaitYield on receive.
type TimerChannel struct {
	TimerID uint64
}

// timerChannelReceive yields TimerWaitYield to wait for timer to fire.
func timerChannelReceive(l *lua.LState) int {
	ud := l.CheckUserData(1)
	ch, ok := ud.Value.(*TimerChannel)
	if !ok {
		l.ArgError(1, "timer channel expected")
		return 0
	}
	yield := acquireTimerWaitYield(ch.TimerID)
	l.Push(yield)
	return -1
}

func registerTimerChannelMethods() {
	value.RegisterMethods(nil, timerChannelTypeName, map[string]lua.LGoFunc{
		"receive": timerChannelReceive,
	})
}

func registerTimerMethods() {
	value.RegisterMethods(nil, timerTypeName, map[string]lua.LGoFunc{
		"channel": timerChannelMethod,
		"stop":    timerStopMethod,
		"reset":   timerResetMethod,
	})
}

func checkTimer(l *lua.LState, idx int) *Timer {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Timer); ok {
		return v
	}
	l.ArgError(idx, "Timer expected")
	return nil
}

func timerChannelMethod(l *lua.LState) int {
	timer := checkTimer(l, 1)
	if timer.Channel == nil {
		l.RaiseError("timer has no channel")
		return 0
	}
	ud := l.NewUserData()
	ud.Value = timer.Channel
	ud.Metatable = value.GetTypeMetatable(nil, timerChannelTypeName)
	l.Push(ud)
	return 1
}

func timerStopMethod(l *lua.LState) int {
	timer := checkTimer(l, 1)
	yield := acquireTimerStopYield(timer.ID)
	l.Push(yield)
	return -1
}

func timerResetMethod(l *lua.LState) int {
	timer := checkTimer(l, 1)
	duration, err := ParseDuration(l, 2)
	if err != nil {
		l.RaiseError("timer:reset: %s", err.Error())
		return 0
	}
	if duration <= 0 {
		l.RaiseError("timer:reset: duration must be > 0")
		return 0
	}
	yield := acquireTimerResetYield(timer.ID, duration)
	l.Push(yield)
	return -1
}

// Yielding Lua functions

func sleepFunc(l *lua.LState) int {
	duration, err := ParseDuration(l, 1)
	if err != nil {
		l.RaiseError("time.sleep: %s", err.Error())
		return 0
	}
	yield := acquireSleepYield(duration)
	l.Push(yield)
	return -1
}

func timerFunc(l *lua.LState) int {
	duration, err := ParseDuration(l, 1)
	if err != nil {
		l.RaiseError("time.timer: %s", err.Error())
		return 0
	}
	if duration <= 0 {
		l.RaiseError("time.timer: duration must be > 0")
		return 0
	}
	yield := acquireTimerStartYield(duration)
	l.Push(yield)
	return -1
}

// afterFunc returns a channel that receives once after the duration.
// Returns a standard engine.Channel that works with channel.select.
func afterFunc(l *lua.LState) int {
	duration, err := ParseDuration(l, 1)
	if err != nil {
		l.RaiseError("time.after: %s", err.Error())
		return 0
	}
	if duration <= 0 {
		l.RaiseError("time.after: duration must be > 0")
		return 0
	}

	// Create buffered channel (size 1) for the timer result
	ch := engine.NewChannel(1)
	engine.PushChannel(l, ch)
	l.Pop(1)

	yield := acquireAfterStartYield(duration, ch)
	l.Push(yield)
	return -1
}

func nowFunc(l *lua.LState) int {
	var t stdtime.Time

	if ref := clockapi.GetTimeReference(l.Context()); ref != nil {
		t = ref.Now()
	} else {
		t = stdtime.Now()
	}

	ud := l.NewUserData()
	ud.Value = &Time{time: t}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	l.Push(ud)
	return 1
}

// tickerFunc creates a ticker and returns Ticker userdata with channel.
func tickerFunc(l *lua.LState) int {
	duration, err := ParseDuration(l, 1)
	if err != nil {
		l.RaiseError("time.ticker: %s", err.Error())
		return 0
	}
	if duration <= 0 {
		l.RaiseError("time.ticker: duration must be > 0")
		return 0
	}
	yield := acquireTickerStartYield(duration)
	l.Push(yield)
	return -1
}

// Ticker method functions

func checkTicker(l *lua.LState, idx int) *Ticker {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Ticker); ok {
		return v
	}
	l.ArgError(idx, "Ticker expected")
	return nil
}

func tickerStopMethod(l *lua.LState) int {
	ticker := checkTicker(l, 1)
	yield := acquireTickerStopYield(ticker.ID)
	l.Push(yield)
	return -1
}

func tickerChannelMethod(l *lua.LState) int {
	ticker := checkTicker(l, 1)
	if ticker.Channel == nil {
		l.RaiseError("ticker has no channel")
		return 0
	}
	ud := l.NewUserData()
	ud.Value = ticker.Channel
	ud.Metatable = value.GetTypeMetatable(nil, tickerChannelTypeName)
	l.Push(ud)
	return 1
}

// Non-yielding Lua functions

func dateFunc(l *lua.LState) int {
	year := l.CheckInt(1)
	month := stdtime.Month(l.CheckInt(2))
	day := l.CheckInt(3)
	hour := l.CheckInt(4)
	min := l.CheckInt(5)
	sec := l.CheckInt(6)
	nsec := l.CheckInt(7)

	var loc *stdtime.Location
	if l.GetTop() >= 8 {
		if location, ok := isLocation(l, 8); ok {
			loc = location.location
		} else {
			l.ArgError(8, "location expected")
			return 0
		}
	} else {
		loc = stdtime.Local
	}

	t := stdtime.Date(year, month, day, hour, min, sec, nsec, loc)
	ud := l.NewUserData()
	ud.Value = &Time{time: t}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	l.Push(ud)
	return 1
}

func unixFunc(l *lua.LState) int {
	sec := l.CheckInt64(1)
	nsec := l.CheckInt64(2)
	t := stdtime.Unix(sec, nsec)
	ud := l.NewUserData()
	ud.Value = &Time{time: t}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	l.Push(ud)
	return 1
}

func parseFunc(l *lua.LState) int {
	layout := l.CheckString(1)
	v := l.CheckString(2)

	var loc *stdtime.Location
	if l.GetTop() >= 3 {
		if location, ok := isLocation(l, 3); ok {
			loc = location.location
		} else {
			l.ArgError(3, "location expected")
			return 0
		}
	} else {
		loc = stdtime.Local
	}

	t, err := stdtime.ParseInLocation(layout, v, loc)
	if err != nil {
		return internalError(l, err, "time.parse")
	}

	ud := l.NewUserData()
	ud.Value = &Time{time: t}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	l.Push(ud)
	return 1
}

func parseDurationFunc(l *lua.LState) int {
	duration, err := parseDurationValue(l.Get(1))
	if err != nil {
		return internalError(l, err, "time.parse_duration")
	}

	ud := l.NewUserData()
	ud.Value = &Duration{duration: duration}
	ud.Metatable = value.GetTypeMetatable(l, durationTypeName)
	l.Push(ud)
	return 1
}

func loadLocationFunc(l *lua.LState) int {
	name := l.CheckString(1)
	if name == "" {
		return invalidError(l, "empty location name")
	}

	loc, err := stdtime.LoadLocation(name)
	if err != nil {
		return internalError(l, err, "time.load_location")
	}

	ud := l.NewUserData()
	ud.Value = &Location{location: loc}
	ud.Metatable = value.GetTypeMetatable(l, locationTypeName)
	l.Push(ud)
	return 1
}

func fixedZoneFunc(l *lua.LState) int {
	name := l.CheckString(1)
	offset := l.CheckInt(2)
	loc := stdtime.FixedZone(name, offset)

	ud := l.NewUserData()
	ud.Value = &Location{location: loc}
	ud.Metatable = value.GetTypeMetatable(l, locationTypeName)
	l.Push(ud)
	return 1
}

// Duration methods

func durationToString(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if d, ok := ud.Value.(*Duration); ok {
		l.Push(lua.LString(d.duration.String()))
		return 1
	}
	l.ArgError(1, "duration expected")
	return 0
}

func durationNanoseconds(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if d, ok := ud.Value.(*Duration); ok {
		l.Push(lua.LNumber(d.duration.Nanoseconds()))
		return 1
	}
	l.ArgError(1, "duration expected")
	return 0
}

func durationMicroseconds(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if d, ok := ud.Value.(*Duration); ok {
		l.Push(lua.LNumber(d.duration.Microseconds()))
		return 1
	}
	l.ArgError(1, "duration expected")
	return 0
}

func durationMilliseconds(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if d, ok := ud.Value.(*Duration); ok {
		l.Push(lua.LNumber(d.duration.Milliseconds()))
		return 1
	}
	l.ArgError(1, "duration expected")
	return 0
}

func durationSeconds(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if d, ok := ud.Value.(*Duration); ok {
		l.Push(lua.LNumber(d.duration.Seconds()))
		return 1
	}
	l.ArgError(1, "duration expected")
	return 0
}

func durationMinutes(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if d, ok := ud.Value.(*Duration); ok {
		l.Push(lua.LNumber(d.duration.Minutes()))
		return 1
	}
	l.ArgError(1, "duration expected")
	return 0
}

func durationHours(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if d, ok := ud.Value.(*Duration); ok {
		l.Push(lua.LNumber(d.duration.Hours()))
		return 1
	}
	l.ArgError(1, "duration expected")
	return 0
}

// Location methods

func locationString(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if loc, ok := ud.Value.(*Location); ok {
		l.Push(lua.LString(loc.location.String()))
		return 1
	}
	l.ArgError(1, "location expected")
	return 0
}

// Time methods

func timeToString(l *lua.LState) int {
	if t, ok := isTime(l, 1); ok {
		l.Push(lua.LString(t.time.String()))
		return 1
	}
	l.ArgError(1, "time expected")
	return 0
}

func timeAdd(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	duration, err := parseDurationValue(l.Get(2))
	if err != nil {
		l.ArgError(2, err.Error())
		return 0
	}

	newTime := t.time.Add(duration)
	ud := l.NewUserData()
	ud.Value = &Time{time: newTime}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	l.Push(ud)
	return 1
}

func timeSub(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	other, ok := isTime(l, 2)
	if !ok {
		l.ArgError(2, "time expected")
		return 0
	}

	duration := t.time.Sub(other.time)
	ud := l.NewUserData()
	ud.Value = &Duration{duration: duration}
	ud.Metatable = value.GetTypeMetatable(l, durationTypeName)
	l.Push(ud)
	return 1
}

func timeAddDate(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	years := l.CheckInt(2)
	months := l.CheckInt(3)
	days := l.CheckInt(4)

	newTime := t.time.AddDate(years, months, days)
	ud := l.NewUserData()
	ud.Value = &Time{time: newTime}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	l.Push(ud)
	return 1
}

func timeAfter(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	other, ok := isTime(l, 2)
	if !ok {
		l.ArgError(2, "time expected")
		return 0
	}

	l.Push(lua.LBool(t.time.After(other.time)))
	return 1
}

func timeBefore(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	other, ok := isTime(l, 2)
	if !ok {
		l.ArgError(2, "time expected")
		return 0
	}

	l.Push(lua.LBool(t.time.Before(other.time)))
	return 1
}

func timeEqual(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	other, ok := isTime(l, 2)
	if !ok {
		l.ArgError(2, "time expected")
		return 0
	}

	l.Push(lua.LBool(t.time.Equal(other.time)))
	return 1
}

func timeFormat(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	layout := l.CheckString(2)
	l.Push(lua.LString(t.time.Format(layout)))
	return 1
}

func timeFormatRFC3339(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LString(t.time.Format(stdtime.RFC3339)))
	return 1
}

func timeUnix(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LNumber(t.time.Unix()))
	return 1
}

func timeUnixNano(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LNumber(t.time.UnixNano()))
	return 1
}

func timeDate(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	year, month, day := t.time.Date()
	l.Push(lua.LNumber(year))
	l.Push(lua.LNumber(month))
	l.Push(lua.LNumber(day))
	return 3
}

func timeClock(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	hour, min, sec := t.time.Clock()
	l.Push(lua.LNumber(hour))
	l.Push(lua.LNumber(min))
	l.Push(lua.LNumber(sec))
	return 3
}

func timeYear(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LNumber(t.time.Year()))
	return 1
}

func timeMonth(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LNumber(t.time.Month()))
	return 1
}

func timeDay(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LNumber(t.time.Day()))
	return 1
}

func timeHour(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LNumber(t.time.Hour()))
	return 1
}

func timeMinute(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LNumber(t.time.Minute()))
	return 1
}

func timeSecond(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LNumber(t.time.Second()))
	return 1
}

func timeNanosecond(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LNumber(t.time.Nanosecond()))
	return 1
}

func timeWeekday(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LNumber(t.time.Weekday()))
	return 1
}

func timeYearDay(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LNumber(t.time.YearDay()))
	return 1
}

func timeIsZero(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	l.Push(lua.LBool(t.time.IsZero()))
	return 1
}

func timeIn(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	loc, ok := isLocation(l, 2)
	if !ok {
		l.ArgError(2, "location expected")
		return 0
	}

	newTime := t.time.In(loc.location)
	ud := l.NewUserData()
	ud.Value = &Time{time: newTime}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	l.Push(ud)
	return 1
}

func timeLocation(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	loc := t.time.Location()
	ud := l.NewUserData()
	ud.Value = &Location{location: loc}
	ud.Metatable = value.GetTypeMetatable(l, locationTypeName)
	l.Push(ud)
	return 1
}

func timeUTC(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	newTime := t.time.UTC()
	ud := l.NewUserData()
	ud.Value = &Time{time: newTime}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	l.Push(ud)
	return 1
}

func timeLocal(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	newTime := t.time.Local()
	ud := l.NewUserData()
	ud.Value = &Time{time: newTime}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	l.Push(ud)
	return 1
}

func timeRound(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	d, ok := isDuration(l, 2)
	if !ok {
		l.ArgError(2, "duration expected")
		return 0
	}

	newTime := t.time.Round(d.duration)
	ud := l.NewUserData()
	ud.Value = &Time{time: newTime}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	l.Push(ud)
	return 1
}

func timeTruncate(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	d, ok := isDuration(l, 2)
	if !ok {
		l.ArgError(2, "duration expected")
		return 0
	}

	newTime := t.time.Truncate(d.duration)
	ud := l.NewUserData()
	ud.Value = &Time{time: newTime}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	l.Push(ud)
	return 1
}

// ParseDuration parses a duration from Lua argument.
// Accepts: number (nanoseconds), string ("1h30m"), or Duration userdata.
func ParseDuration(l *lua.LState, idx int) (stdtime.Duration, error) {
	arg := l.Get(idx)
	switch v := arg.(type) {
	case lua.LNumber:
		return stdtime.Duration(v), nil
	case lua.LInteger:
		return stdtime.Duration(v), nil
	case lua.LString:
		return stdtime.ParseDuration(string(v))
	case *lua.LUserData:
		if d, ok := v.Value.(*Duration); ok {
			return d.duration, nil
		}
		return 0, ErrDurationNumberOrStringExpected
	default:
		return 0, ErrDurationNumberOrStringExpected
	}
}

func parseDurationValue(value lua.LValue) (stdtime.Duration, error) {
	switch v := value.(type) {
	case *lua.LUserData:
		if d, ok := v.Value.(*Duration); ok {
			return d.duration, nil
		}
		return 0, NewInvalidDurationType(fmt.Sprintf("%T", v.Value))
	case lua.LString:
		return stdtime.ParseDuration(string(v))
	case lua.LNumber:
		return stdtime.Duration(v), nil
	case lua.LInteger:
		return stdtime.Duration(v), nil
	}
	return 0, NewInvalidValueType(fmt.Sprintf("%T", value))
}

func isTime(l *lua.LState, n int) (*Time, bool) {
	if ud, ok := l.Get(n).(*lua.LUserData); ok {
		if t, ok := ud.Value.(*Time); ok {
			return t, true
		}
	}
	return nil, false
}

func isDuration(l *lua.LState, n int) (*Duration, bool) {
	if ud, ok := l.Get(n).(*lua.LUserData); ok {
		if d, ok := ud.Value.(*Duration); ok {
			return d, true
		}
	}
	return nil, false
}

func isLocation(l *lua.LState, n int) (*Location, bool) {
	if ud, ok := l.Get(n).(*lua.LUserData); ok {
		if loc, ok := ud.Value.(*Location); ok {
			return loc, true
		}
	}
	return nil, false
}
