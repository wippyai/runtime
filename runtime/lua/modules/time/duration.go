package time

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// Module represents a time Lua module
type Module struct{}

// Duration represents a Lua userdata object wrapping time.Duration
type Duration struct {
	duration time.Duration
}

// Duration methods implementations
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

// durationToString implements the __tostring metamethod for Duration
func durationToString(l *lua.LState) int {
	ud := l.CheckUserData(1)
	if d, ok := ud.Value.(*Duration); ok {
		l.Push(lua.LString(d.duration.String()))
		return 1
	}
	l.ArgError(1, "duration expected")
	return 0
}

// parseDuration implements time.ParseDuration for Lua
func parseDuration(l *lua.LState) int {
	duration, err := parseDurationValue(l.Get(1))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	ud := l.NewUserData()
	ud.Value = &Duration{duration: duration}
	l.SetMetatable(ud, l.GetTypeMetatable("time.Duration"))
	l.Push(ud)
	return 1
}

func parseDurationValue(value lua.LValue) (time.Duration, error) {
	switch v := value.(type) {
	case *lua.LUserData:
		if d, ok := v.Value.(*Duration); ok {
			return d.duration, nil
		}
		return 0, fmt.Errorf("duration expected, got %T", v.Value)

	case lua.LString:
		return time.ParseDuration(string(v))

	case lua.LNumber:
		// Treat raw numbers as milliseconds for compatibility
		return time.Duration(float64(v) * float64(time.Millisecond)), nil
	}

	return 0, fmt.Errorf("duration, string, or number expected, got %T", value)
}

func registerDuration(l *lua.LState, mod *lua.LTable) {
	// Use the efficient registration method
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

	// Register duration constants
	mod.RawSetString("NANOSECOND", lua.LNumber(Nanosecond))
	mod.RawSetString("MICROSECOND", lua.LNumber(Microsecond))
	mod.RawSetString("MILLISECOND", lua.LNumber(Millisecond))
	mod.RawSetString("SECOND", lua.LNumber(Second))
	mod.RawSetString("MINUTE", lua.LNumber(Minute))
	mod.RawSetString("HOUR", lua.LNumber(Hour))

	// Register duration function
	mod.RawSetString("parse_duration", l.NewFunction(parseDuration))
}
