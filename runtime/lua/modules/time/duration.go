package time

import (
	"time"

	lua "github.com/yuin/gopher-lua"
)

// Module represents a time Lua module
type Module struct{}

// Duration represents a Lua userdata object wrapping time.Duration
type Duration struct {
	duration time.Duration
}

// Register duration methods
var durationMethods = map[string]lua.LGFunction{
	"nanoseconds":  durationNanoseconds,
	"microseconds": durationMicroseconds,
	"milliseconds": durationMilliseconds,
	"seconds":      durationSeconds,
	"minutes":      durationMinutes,
	"hours":        durationHours,
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
	str := l.CheckString(1)

	if str == "" {
		l.ArgError(1, "empty string")
		return 0
	}

	duration, err := time.ParseDuration(str)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	ud := l.NewUserData()
	ud.Value = &Duration{duration: duration}
	l.SetMetatable(ud, l.GetTypeMetatable("Duration"))
	l.Push(ud)
	return 1
}

func registerDuration(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("Duration")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), durationMethods))
	l.SetField(mt, "__tostring", l.NewFunction(durationToString))

	// Register duration constants
	l.SetField(mod, "NANOSECOND", lua.LNumber(Nanosecond))
	l.SetField(mod, "MICROSECOND", lua.LNumber(Microsecond))
	l.SetField(mod, "MILLISECOND", lua.LNumber(Millisecond))
	l.SetField(mod, "SECOND", lua.LNumber(Second))
	l.SetField(mod, "MINUTE", lua.LNumber(Minute))
	l.SetField(mod, "HOUR", lua.LNumber(Hour))

	l.SetFuncs(mod, map[string]lua.LGFunction{
		"parse_duration": parseDuration,
	})
}
