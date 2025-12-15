package time

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	stdtime "time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const (
	tickerTypeName   = "time.Ticker"
	timerTypeName    = "time.Timer"
	timeTypeName     = "time.Time"
	durationTypeName = "time.Duration"
	locationTypeName = "time.Location"
)

// tickerCounter generates unique topic names for tickers.
var tickerCounter uint64

// timerCounter generates unique topic names for timers.
var timerCounter uint64

// Error helpers for structured errors

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func wrapErrorValue(l *lua.LState, goErr error, context string) lua.LValue {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.Internal).
		WithRetryable(false)
	return err
}

func newErrorValue(l *lua.LState, msg string) lua.LValue {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
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
	registerTimerMethods()

	// Create module table
	initModuleTable()

	// Setup yield types
	yieldTypes = []luaapi.YieldType{
		{Sample: &SleepYield{}, CmdID: clockapi.Sleep},
		{Sample: &TimerStartYield{}, CmdID: clockapi.TimerStart},
		{Sample: &AfterStartYield{}, CmdID: clockapi.TimerStart},
		{Sample: &TimerStopYield{}, CmdID: clockapi.TimerStop},
		{Sample: &TimerResetYield{}, CmdID: clockapi.TimerReset},
		{Sample: &TickerStartYield{}, CmdID: clockapi.TickerStart},
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
		"stop":     tickerStopMethod,
		"response": tickerResponseMethod,
		"channel":  tickerResponseMethod, // alias for backwards compatibility
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
// Uses topic-based delivery like Ticker - scheduler sends to channel when timer fires.
type TimerStartYield struct {
	Duration stdtime.Duration
	Channel  *engine.Channel
	PID      pid.PID
	Topic    string
}

var timerStartYieldPool = sync.Pool{
	New: func() interface{} { return &TimerStartYield{} },
}

func acquireTimerStartYield(d stdtime.Duration, ch *engine.Channel, p pid.PID, topic string) *TimerStartYield {
	y := timerStartYieldPool.Get().(*TimerStartYield)
	y.Duration = d
	y.Channel = ch
	y.PID = p
	y.Topic = topic
	return y
}

func ReleaseTimerStartYield(y *TimerStartYield) {
	y.Duration = 0
	y.Channel = nil
	y.PID = pid.PID{}
	y.Topic = ""
	timerStartYieldPool.Put(y)
}

func (y *TimerStartYield) Release()                    { ReleaseTimerStartYield(y) }
func (y *TimerStartYield) String() string              { return "<timer_start_yield>" }
func (y *TimerStartYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *TimerStartYield) CmdID() dispatcher.CommandID { return clockapi.TimerStart }
func (y *TimerStartYield) ToCommand() dispatcher.Command {
	return clockapi.TimerStartCmd{Duration: y.Duration, PID: y.PID, Topic: y.Topic}
}

// HandleResult implements HandledYield to set up topic subscription.
// Returns a Timer with an engine.Channel that works with channel.select.
func (y *TimerStartYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapErrorValue(l, err, "timer start")}
	}

	result, ok := data.(clockapi.TimerStartResult)
	if !ok {
		if id, ok := data.(uint64); ok {
			result = clockapi.TimerStartResult{ID: id}
		} else {
			return []lua.LValue{lua.LNil, newErrorValue(l, "invalid timer result type")}
		}
	}

	// Create channel userdata
	channelUD := engine.PushChannel(l, y.Channel)
	l.Pop(1)

	// Subscribe externally-owned channel to topic
	proc := engine.GetProcess(l)
	if proc != nil {
		if err := proc.SubscribeExisting(y.Topic, y.Channel); err != nil {
			return []lua.LValue{lua.LNil, wrapErrorValue(l, err, "timer subscribe")}
		}
		proc.SetTopicHandler(y.Topic, timerMessageHandler)
	}

	// Create timer with engine.Channel
	timer := &Timer{
		ID:        result.ID,
		channelUD: channelUD,
		channel:   y.Channel,
	}

	ud := l.NewUserData()
	ud.Value = timer
	ud.Metatable = value.GetTypeMetatable(l, timerTypeName)

	// Register cleanup to stop timer when frame is released
	if result.Stop != nil {
		ctx := l.Context()
		if ctx != nil {
			if store := resource.GetStore(ctx); store != nil {
				store.AddCleanup(func() error {
					result.Stop()
					return nil
				})
			}
		}
	}

	return []lua.LValue{ud}
}

// timerMessageHandler converts timer fire payloads to time userdata.
func timerMessageHandler(_ context.Context, l *lua.LState, _ pid.PID, _ string, payloads []payload.Payload) lua.LValue {
	if len(payloads) == 0 {
		return lua.LNil
	}

	p := payloads[0]
	nsec, ok := p.Data().(int64)
	if !ok {
		return lua.LNil
	}

	t := stdtime.Unix(0, nsec)
	ud := l.NewUserData()
	ud.Value = &Time{time: t}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	return ud
}

// AfterStartYield is yielded by time.after() to create a timer channel.
// Uses TimerStart command with topic-based delivery - returns channel immediately,
// channel receives when timer fires asynchronously.
// This is essentially TimerStartYield but returns only the channel (not Timer object).
type AfterStartYield struct {
	TimerStartYield
}

func acquireAfterStartYield(d stdtime.Duration, ch *engine.Channel, p pid.PID, topic string) *AfterStartYield {
	return &AfterStartYield{
		TimerStartYield: TimerStartYield{
			Duration: d,
			Channel:  ch,
			PID:      p,
			Topic:    topic,
		},
	}
}

func (y *AfterStartYield) String() string { return "<after_start_yield>" }

// HandleResult returns just the channel, not a Timer object.
func (y *AfterStartYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	results := y.TimerStartYield.HandleResult(l, data, err)
	if len(results) == 1 {
		// Got Timer userdata, extract channel
		if ud, ok := results[0].(*lua.LUserData); ok {
			if timer, ok := ud.Value.(*Timer); ok {
				return []lua.LValue{timer.channelUD}
			}
		}
	}
	return results
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
func (y *TimerStopYield) HandleResult(_ *lua.LState, data any, err error) []lua.LValue {
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
// Uses topic-based delivery like events/websocket.
type TickerStartYield struct {
	Duration stdtime.Duration
	Channel  *engine.Channel
	PID      pid.PID
	Topic    string
}

var tickerStartYieldPool = sync.Pool{
	New: func() interface{} { return &TickerStartYield{} },
}

func acquireTickerStartYield(d stdtime.Duration, ch *engine.Channel, p pid.PID, topic string) *TickerStartYield {
	y := tickerStartYieldPool.Get().(*TickerStartYield)
	y.Duration = d
	y.Channel = ch
	y.PID = p
	y.Topic = topic
	return y
}

func ReleaseTickerStartYield(y *TickerStartYield) {
	y.Duration = 0
	y.Channel = nil
	y.PID = pid.PID{}
	y.Topic = ""
	tickerStartYieldPool.Put(y)
}

func (y *TickerStartYield) Release()                    { ReleaseTickerStartYield(y) }
func (y *TickerStartYield) String() string              { return "<ticker_start_yield>" }
func (y *TickerStartYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *TickerStartYield) CmdID() dispatcher.CommandID { return clockapi.TickerStart }
func (y *TickerStartYield) ToCommand() dispatcher.Command {
	return clockapi.TickerStartCmd{Duration: y.Duration, PID: y.PID, Topic: y.Topic}
}

// HandleResult implements HandledYield to set up topic subscription.
// Returns a Ticker with an engine.Channel that works with channel.select.
func (y *TickerStartYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, wrapErrorValue(l, err, "ticker start")}
	}

	result, ok := data.(clockapi.TickerStartResult)
	if !ok {
		if id, ok := data.(uint64); ok {
			result = clockapi.TickerStartResult{ID: id}
		} else {
			return []lua.LValue{lua.LNil, newErrorValue(l, "invalid ticker result type")}
		}
	}

	// Create channel userdata
	channelUD := engine.PushChannel(l, y.Channel)
	l.Pop(1)

	// Subscribe externally-owned channel to topic
	proc := engine.GetProcess(l)
	if proc != nil {
		if err := proc.SubscribeExisting(y.Topic, y.Channel); err != nil {
			return []lua.LValue{lua.LNil, wrapErrorValue(l, err, "ticker subscribe")}
		}
		proc.SetTopicHandler(y.Topic, tickerMessageHandler)
	}

	// Create ticker with cached channel
	ticker := &Ticker{
		ID:        result.ID,
		channelUD: channelUD,
		channel:   y.Channel,
	}

	ud := l.NewUserData()
	ud.Value = ticker
	ud.Metatable = value.GetTypeMetatable(l, tickerTypeName)

	// Register cleanup to stop ticker when frame is released
	if result.Stop != nil {
		ctx := l.Context()
		if ctx != nil {
			if store := resource.GetStore(ctx); store != nil {
				store.AddCleanup(func() error {
					result.Stop()
					return nil
				})
			}
		}
	}

	return []lua.LValue{ud}
}

// tickerMessageHandler converts tick payloads to time userdata.
func tickerMessageHandler(_ context.Context, l *lua.LState, _ pid.PID, _ string, payloads []payload.Payload) lua.LValue {
	if len(payloads) == 0 {
		return lua.LNil
	}

	p := payloads[0]
	nsec, ok := p.Data().(int64)
	if !ok {
		return lua.LNil
	}

	t := stdtime.Unix(0, nsec)
	ud := l.NewUserData()
	ud.Value = &Time{time: t}
	ud.Metatable = value.GetTypeMetatable(l, timeTypeName)
	return ud
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
// Uses engine.Channel for receiving ticks via topic subscription.
type Ticker struct {
	ID        uint64
	channelUD *lua.LUserData  // Cached channel userdata
	channel   *engine.Channel // The underlying channel
}

// Timer is the Lua userdata for timer operations.
// Uses engine.Channel for receiving timer fires via topic subscription.
type Timer struct {
	ID        uint64
	channelUD *lua.LUserData  // Cached channel userdata
	channel   *engine.Channel // The underlying channel
}

func registerTimerMethods() {
	value.RegisterMethods(nil, timerTypeName, map[string]lua.LGoFunc{
		"response": timerResponseMethod,
		"channel":  timerResponseMethod, // alias for backwards compatibility
		"stop":     timerStopMethod,
		"reset":    timerResetMethod,
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

func timerResponseMethod(l *lua.LState) int {
	timer := checkTimer(l, 1)
	if timer == nil {
		return 0
	}
	if timer.channelUD == nil {
		l.RaiseError("timer has no channel")
		return 0
	}
	l.Push(timer.channelUD)
	return 1
}

func timerStopMethod(l *lua.LState) int {
	timer := checkTimer(l, 1)
	if timer == nil {
		return 0
	}
	yield := acquireTimerStopYield(timer.ID)
	l.Push(yield)
	return -1
}

func timerResetMethod(l *lua.LState) int {
	timer := checkTimer(l, 1)
	if timer == nil {
		return 0
	}
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
	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("time.timer: no context")
		return 0
	}

	duration, err := ParseDuration(l, 1)
	if err != nil {
		l.RaiseError("time.timer: %s", err.Error())
		return 0
	}
	if duration <= 0 {
		l.RaiseError("time.timer: duration must be > 0")
		return 0
	}

	p, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.RaiseError("time.timer: no process PID")
		return 0
	}

	// Create channel and unique topic
	ch := engine.NewChannel(1)
	timerID := atomic.AddUint64(&timerCounter, 1)
	topic := fmt.Sprintf("timer@%d", timerID)

	yield := acquireTimerStartYield(duration, ch, p, topic)
	l.Push(yield)
	return -1
}

// afterFunc returns a channel that receives once after the duration.
// Returns a standard engine.Channel that works with channel.select.
func afterFunc(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("time.after: no context")
		return 0
	}

	duration, err := ParseDuration(l, 1)
	if err != nil {
		l.RaiseError("time.after: %s", err.Error())
		return 0
	}
	if duration <= 0 {
		l.RaiseError("time.after: duration must be > 0")
		return 0
	}

	p, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.RaiseError("time.after: no process PID")
		return 0
	}

	// Create channel and unique topic
	ch := engine.NewChannel(1)
	timerID := atomic.AddUint64(&timerCounter, 1)
	topic := fmt.Sprintf("after@%d", timerID)

	yield := acquireAfterStartYield(duration, ch, p, topic)
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
	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("time.ticker: no context")
		return 0
	}

	duration, err := ParseDuration(l, 1)
	if err != nil {
		l.RaiseError("time.ticker: %s", err.Error())
		return 0
	}
	if duration <= 0 {
		l.RaiseError("time.ticker: duration must be > 0")
		return 0
	}

	p, ok := runtime.GetFramePID(ctx)
	if !ok {
		l.RaiseError("time.ticker: no process PID")
		return 0
	}

	// Create channel and unique topic
	ch := engine.NewChannel(16)
	tickerID := atomic.AddUint64(&tickerCounter, 1)
	topic := fmt.Sprintf("ticker@%d", tickerID)

	yield := acquireTickerStartYield(duration, ch, p, topic)
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
	if ticker == nil {
		return 0
	}
	yield := acquireTickerStopYield(ticker.ID)
	l.Push(yield)
	return -1
}

func tickerResponseMethod(l *lua.LState) int {
	ticker := checkTicker(l, 1)
	if ticker == nil {
		return 0
	}
	if ticker.channelUD == nil {
		l.RaiseError("ticker has no channel")
		return 0
	}
	l.Push(ticker.channelUD)
	return 1
}

// Non-yielding Lua functions

func dateFunc(l *lua.LState) int {
	year := l.CheckInt(1)
	month := stdtime.Month(l.CheckInt(2))
	day := l.CheckInt(3)
	hour := l.CheckInt(4)
	minute := l.CheckInt(5)
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

	t := stdtime.Date(year, month, day, hour, minute, sec, nsec, loc)
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

	hour, minute, sec := t.time.Clock()
	l.Push(lua.LNumber(hour))
	l.Push(lua.LNumber(minute))
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
