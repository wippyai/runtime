package time

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	origtime "github.com/wippyai/runtime/runtime/lua/modules/time"
	lua "github.com/yuin/gopher-lua"
)

type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

func NewTimeModule() *Module {
	return &Module{}
}

// Info returns module metadata
func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "workflow.time",
		Description: "Workflow-safe time operations",
		Class:       []string{luaapi.ClassWorkflow, luaapi.ClassTime},
	}
}

func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		mod := l.CreateTable(0, 50)

		// Duration constants (reuse from original)
		mod.RawSetString("NANOSECOND", lua.LNumber(origtime.Nanosecond))
		mod.RawSetString("MICROSECOND", lua.LNumber(origtime.Microsecond))
		mod.RawSetString("MILLISECOND", lua.LNumber(origtime.Millisecond))
		mod.RawSetString("SECOND", lua.LNumber(origtime.Second))
		mod.RawSetString("MINUTE", lua.LNumber(origtime.Minute))
		mod.RawSetString("HOUR", lua.LNumber(origtime.Hour))

		// Time format constants (from Go time package)
		mod.RawSetString("RFC3339", lua.LString("2006-01-02T15:04:05Z07:00"))
		mod.RawSetString("RFC3339NANO", lua.LString("2006-01-02T15:04:05.999999999Z07:00"))
		mod.RawSetString("RFC822", lua.LString("02 Jan 06 15:04 MST"))
		mod.RawSetString("RFC822Z", lua.LString("02 Jan 06 15:04 -0700"))
		mod.RawSetString("RFC850", lua.LString("Monday, 02-Jan-06 15:04:05 MST"))
		mod.RawSetString("RFC1123", lua.LString("Mon, 02 Jan 2006 15:04:05 MST"))
		mod.RawSetString("RFC1123Z", lua.LString("Mon, 02 Jan 2006 15:04:05 -0700"))
		mod.RawSetString("KITCHEN", lua.LString("3:04PM"))
		mod.RawSetString("STAMP", lua.LString("Jan _2 15:04:05"))
		mod.RawSetString("STAMP_MILLI", lua.LString("Jan _2 15:04:05.000"))
		mod.RawSetString("STAMP_MICRO", lua.LString("Jan _2 15:04:05.000000"))
		mod.RawSetString("STAMP_NANO", lua.LString("Jan _2 15:04:05.000000000"))
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

		// Location constants
		utcUD := l.NewUserData()
		utcUD.Value = origtime.NewUTCLocation()
		utcUD.Metatable = value.GetTypeMetatable(l, "time.Location")
		mod.RawSetString("utc", utcUD)

		localUD := l.NewUserData()
		localUD.Value = origtime.NewLocalLocation()
		localUD.Metatable = value.GetTypeMetatable(l, "time.Location")
		mod.RawSetString("localtz", localUD)

		// Workflow-specific time-dependent functions
		mod.RawSetString("now", l.NewFunction(now))
		mod.RawSetString("sleep", l.NewFunction(sleep))
		mod.RawSetString("after", l.NewFunction(after))
		mod.RawSetString("timer", l.NewFunction(timer))
		mod.RawSetString("ticker", l.NewFunction(ticker)) // Raises error - not supported

		// Pure functions delegated to original module
		mod.RawSetString("date", l.NewFunction(origtime.Date))
		mod.RawSetString("unix", l.NewFunction(origtime.Unix))
		mod.RawSetString("parse", l.NewFunction(origtime.Parse))
		mod.RawSetString("parse_duration", l.NewFunction(origtime.ParseDuration))
		mod.RawSetString("load_location", l.NewFunction(origtime.LoadLocation))
		mod.RawSetString("fixed_zone", l.NewFunction(origtime.FixedZone))

		mod.Immutable = true
		m.moduleTable = mod
	})

	// Register type methods (per LState)
	origtime.RegisterDurationMethods(l)
	origtime.RegisterLocationMethods(l)
	origtime.RegisterTimeMethods(l)
	registerTimerMethods(l)

	l.Push(m.moduleTable)
	return 1
}

func registerTimerMethods(l *lua.LState) {
	value.RegisterMethods(l, "time.Timer", map[string]lua.LGFunction{
		"stop":    timerStop,
		"reset":   timerReset,
		"channel": timerChannel,
	})
}
