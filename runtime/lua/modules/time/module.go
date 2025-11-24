package time

import (
	"sync"
	"time"

	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// Duration constants in nanoseconds
const (
	Nanosecond  = 1
	Microsecond = 1000 * Nanosecond
	Millisecond = 1000 * Microsecond
	Second      = 1000 * Millisecond
	Minute      = 60 * Second
	Hour        = 60 * Minute
)

// Module represents a time Lua module
type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

// NewTimeModule creates and returns a new instance of the time Module
func NewTimeModule() *Module {
	return &Module{}
}

// Name returns the module's name
func (m *Module) Name() string {
	return "time"
}

// Loader registers the module's functions and constants into Lua state
func (m *Module) Loader(l *lua.LState) int {
	// Create module table once and cache it
	m.once.Do(func() {
		mod := l.CreateTable(0, 50) // Large table with many functions and constants

		// === CONSTANTS ===

		// Duration constants
		mod.RawSetString("NANOSECOND", lua.LNumber(Nanosecond))
		mod.RawSetString("MICROSECOND", lua.LNumber(Microsecond))
		mod.RawSetString("MILLISECOND", lua.LNumber(Millisecond))
		mod.RawSetString("SECOND", lua.LNumber(Second))
		mod.RawSetString("MINUTE", lua.LNumber(Minute))
		mod.RawSetString("HOUR", lua.LNumber(Hour))

		// Time format constants
		mod.RawSetString("RFC3339", lua.LString(time.RFC3339))
		mod.RawSetString("RFC3339NANO", lua.LString(time.RFC3339Nano))
		mod.RawSetString("RFC822", lua.LString(time.RFC822))
		mod.RawSetString("RFC822Z", lua.LString(time.RFC822Z))
		mod.RawSetString("RFC850", lua.LString(time.RFC850))
		mod.RawSetString("RFC1123", lua.LString(time.RFC1123))
		mod.RawSetString("RFC1123Z", lua.LString(time.RFC1123Z))
		mod.RawSetString("KITCHEN", lua.LString(time.Kitchen))
		mod.RawSetString("STAMP", lua.LString(time.Stamp))
		mod.RawSetString("STAMP_MILLI", lua.LString(time.StampMilli))
		mod.RawSetString("STAMP_MICRO", lua.LString(time.StampMicro))
		mod.RawSetString("STAMP_NANO", lua.LString(time.StampNano))
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

		// Location constants (userdata objects)
		utcUD := l.NewUserData()
		utcUD.Value = &Location{location: time.UTC}
		utcUD.Metatable = value.GetTypeMetatable(l, "time.Location")
		mod.RawSetString("utc", utcUD)

		localUD := l.NewUserData()
		localUD.Value = &Location{location: time.Local}
		localUD.Metatable = value.GetTypeMetatable(l, "time.Location")
		mod.RawSetString("localtz", localUD)

		// === FUNCTIONS ===

		// Core time functions
		mod.RawSetString("now", l.NewFunction(now))
		mod.RawSetString("sleep", l.NewFunction(sleepCoroutine))
		mod.RawSetString("date", l.NewFunction(date))
		mod.RawSetString("unix", l.NewFunction(unix))
		mod.RawSetString("parse", l.NewFunction(parse))
		mod.RawSetString("after", l.NewFunction(after))

		// Duration functions
		mod.RawSetString("parse_duration", l.NewFunction(parseDuration))

		// Location functions
		mod.RawSetString("load_location", l.NewFunction(loadLocation))
		mod.RawSetString("fixed_zone", l.NewFunction(fixedZone))

		// Timer and Ticker constructors
		mod.RawSetString("timer", l.NewFunction(timer))
		mod.RawSetString("ticker", l.NewFunction(ticker))

		mod.Immutable = true
		m.moduleTable = mod
	})

	// Register type methods (per LState)
	registerDurationMethods(l)
	registerLocationMethods(l)
	registerTimeMethods(l)
	registerTimerMethods(l)
	registerTickerMethods(l)

	l.Push(m.moduleTable)
	return 1
}

func RegisterDurationMethods(l *lua.LState) {
	registerDurationMethods(l)
}

// registerDurationMethods registers Duration type methods
func registerDurationMethods(l *lua.LState) {
	value.RegisterTypeMethods(l, "time.Duration",
		map[string]lua.LGFunction{
			"__tostring": durationToString,
		}, map[string]lua.LGFunction{
			"nanoseconds":  durationNanoseconds,
			"microseconds": durationMicroseconds,
			"milliseconds": durationMilliseconds,
			"seconds":      durationSeconds,
			"minutes":      durationMinutes,
			"hours":        durationHours,
		},
	)
}

func RegisterLocationMethods(l *lua.LState) {
	registerLocationMethods(l)
}

// registerLocationMethods registers Location type methods
func registerLocationMethods(l *lua.LState) {
	value.RegisterTypeMethods(l, "time.Location",
		map[string]lua.LGFunction{
			"__tostring": locationString,
		},
		map[string]lua.LGFunction{
			"string": locationString,
		},
	)
}

func RegisterTimeMethods(l *lua.LState) {
	registerTimeMethods(l)
}

// registerTimeMethods registers Time type methods
func registerTimeMethods(l *lua.LState) {
	value.RegisterTypeMethods(l, "time.Time",
		map[string]lua.LGFunction{
			"__tostring": timeToString,
		},
		map[string]lua.LGFunction{
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

// registerTimerMethods registers Timer type methods
func registerTimerMethods(l *lua.LState) {
	value.RegisterMethods(l, "time.Timer", map[string]lua.LGFunction{
		"stop":    timerStop,
		"reset":   timerReset,
		"channel": timerChannel,
	})
}

// registerTickerMethods registers Ticker type methods
func registerTickerMethods(l *lua.LState) {
	value.RegisterMethods(l, "time.Ticker", map[string]lua.LGFunction{
		"stop":    tickerStop,
		"channel": tickerChannel,
	})
}
