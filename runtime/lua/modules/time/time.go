package time

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// Time represents a Lua userdata object wrapping time.Time
type Time struct {
	time time.Time
}

// Time methods map
var timeMethods = map[string]lua.LGFunction{
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
			return loc, ok
		}
	}
	return nil, false
}

// Core time functions
func now(l *lua.LState) int {
	t := time.Now()
	ud := l.NewUserData()
	ud.Value = &Time{time: t}
	l.SetMetatable(ud, l.GetTypeMetatable("Time"))
	l.Push(ud)
	return 1
}

// Helper function that encapsulates sleep logic with context handling
func performSleep(ctx context.Context, duration time.Duration) error {
	if ctx != nil {
		timer := time.NewTimer(duration)
		defer timer.Stop()

		select {
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	time.Sleep(duration)
	return nil
}
func sleep(l *lua.LState) int {
	duration, err := parseDurationValue(l.Get(1))
	if err != nil {
		if _, ok := l.Get(1).(*lua.LNumber); ok {
			l.RaiseError("duration or string expected")
			return 0
		}
		l.RaiseError(err.Error())
		return 1
	}

	if err := performSleep(l.Context(), duration); err != nil {
		l.RaiseError(err.Error())
		return 1
	}
	return 0
}

func sleepCoroutine(l *lua.LState) int {
	duration, err := parseDurationValue(l.Get(1))
	if err != nil {
		if _, ok := l.Get(1).(*lua.LNumber); ok {
			l.RaiseError("duration or string expected")
			return 0
		}
		l.RaiseError(err.Error())
		return 1
	}

	coroutine.Wrap(l, func() engine.Result {
		if err := performSleep(l.Context(), duration); err != nil {
			return engine.Result{
				State:  l,
				Result: []lua.LValue{lua.LNil},
				Error:  err,
			}
		}
		return engine.Result{
			State:  l,
			Result: []lua.LValue{lua.LString("ok")},
		}
	})
	return -1
}

func date(l *lua.LState) int {
	year := l.CheckInt(1)
	month := time.Month(l.CheckInt(2))
	day := l.CheckInt(3)
	hour := l.CheckInt(4)
	mn := l.CheckInt(5)
	sec := l.CheckInt(6)
	nsec := l.CheckInt(7)

	var loc *time.Location
	if l.GetTop() >= 8 {
		if location, ok := isLocation(l, 8); ok {
			loc = location.location
		} else {
			l.ArgError(8, "location expected")
			return 0
		}
	} else {
		loc = time.Local
	}

	t := time.Date(year, month, day, hour, mn, sec, nsec, loc)
	ud := l.NewUserData()
	ud.Value = &Time{time: t}
	l.SetMetatable(ud, l.GetTypeMetatable("Time"))
	l.Push(ud)
	return 1
}

func unix(l *lua.LState) int {
	sec := l.CheckInt64(1)
	nsec := l.CheckInt64(2)
	t := time.Unix(sec, nsec)
	ud := l.NewUserData()
	ud.Value = &Time{time: t}
	l.SetMetatable(ud, l.GetTypeMetatable("Time"))
	l.Push(ud)
	return 1
}

func parse(l *lua.LState) int {
	layout := l.CheckString(1)
	value := l.CheckString(2)

	var loc *time.Location
	if l.GetTop() >= 3 {
		if location, ok := isLocation(l, 3); ok {
			loc = location.location
		} else {
			l.ArgError(3, "location expected")
			return 0
		}
	} else {
		loc = time.Local
	}

	t, err := time.ParseInLocation(layout, value, loc)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	ud := l.NewUserData()
	ud.Value = &Time{time: t}
	l.SetMetatable(ud, l.GetTypeMetatable("Time"))
	l.Push(ud)
	return 1
}

// Time methods implementations
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
	result := l.NewUserData()
	result.Value = &Time{time: newTime}
	l.SetMetatable(result, l.GetTypeMetatable("Time"))
	l.Push(result)
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
	result := l.NewUserData()
	result.Value = &Duration{duration: duration}
	l.SetMetatable(result, l.GetTypeMetatable("Duration"))
	l.Push(result)
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
	result := l.NewUserData()
	result.Value = &Time{time: newTime}
	l.SetMetatable(result, l.GetTypeMetatable("Time"))
	l.Push(result)
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

	l.Push(lua.LString(t.time.Format(time.RFC3339)))
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

	hour, mn, sec := t.time.Clock()
	l.Push(lua.LNumber(hour))
	l.Push(lua.LNumber(mn))
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
	result := l.NewUserData()
	result.Value = &Time{time: newTime}
	l.SetMetatable(result, l.GetTypeMetatable("Time"))
	l.Push(result)
	return 1
}

func timeLocation(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	loc := t.time.Location()
	result := l.NewUserData()
	result.Value = &Location{location: loc}
	l.SetMetatable(result, l.GetTypeMetatable("Location"))
	l.Push(result)
	return 1
}

func timeUTC(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	newTime := t.time.UTC()
	result := l.NewUserData()
	result.Value = &Time{time: newTime}
	l.SetMetatable(result, l.GetTypeMetatable("Time"))
	l.Push(result)
	return 1
}

func timeLocal(l *lua.LState) int {
	t, ok := isTime(l, 1)
	if !ok {
		l.ArgError(1, "time expected")
		return 0
	}

	newTime := t.time.Local()
	result := l.NewUserData()
	result.Value = &Time{time: newTime}
	l.SetMetatable(result, l.GetTypeMetatable("Time"))
	l.Push(result)
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
	result := l.NewUserData()
	result.Value = &Time{time: newTime}
	l.SetMetatable(result, l.GetTypeMetatable("Time"))
	l.Push(result)
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
	result := l.NewUserData()
	result.Value = &Time{time: newTime}
	l.SetMetatable(result, l.GetTypeMetatable("Time"))
	l.Push(result)
	return 1
}

// timeToString implements the __tostring metamethod for Time
func timeToString(l *lua.LState) int {
	if t, ok := isTime(l, 1); ok {
		l.Push(lua.LString(t.time.String()))
		return 1
	}
	l.ArgError(1, "time expected")
	return 0
}

// Register time-related functionality
func registerTime(l *lua.LState, mod *lua.LTable) {
	// Register Time metatable
	mt := l.NewTypeMetatable("Time")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), timeMethods))
	l.SetField(mt, "__tostring", l.NewFunction(timeToString))

	l.SetField(mod, "RFC3339", lua.LString(time.RFC3339))
	l.SetField(mod, "RFC3339Nano", lua.LString(time.RFC3339Nano))
	l.SetField(mod, "RFC822", lua.LString(time.RFC822))
	l.SetField(mod, "RFC822Z", lua.LString(time.RFC822Z))
	l.SetField(mod, "RFC850", lua.LString(time.RFC850))
	l.SetField(mod, "RFC1123", lua.LString(time.RFC1123))
	l.SetField(mod, "RFC1123Z", lua.LString(time.RFC1123Z))
	l.SetField(mod, "Kitchen", lua.LString(time.Kitchen))
	l.SetField(mod, "Stamp", lua.LString(time.Stamp))
	l.SetField(mod, "StampMilli", lua.LString(time.StampMilli))
	l.SetField(mod, "StampMicro", lua.LString(time.StampMicro))
	l.SetField(mod, "StampNano", lua.LString(time.StampNano))
	l.SetField(mod, "DateTime", lua.LString("2006-01-02 15:04:05"))
	l.SetField(mod, "DateOnly", lua.LString("2006-01-02"))
	l.SetField(mod, "TimeOnly", lua.LString("15:04:05"))

	// Register month constants
	l.SetField(mod, "JANUARY", lua.LNumber(1))
	l.SetField(mod, "FEBRUARY", lua.LNumber(2))
	l.SetField(mod, "MARCH", lua.LNumber(3))
	l.SetField(mod, "APRIL", lua.LNumber(4))
	l.SetField(mod, "MAY", lua.LNumber(5))
	l.SetField(mod, "JUNE", lua.LNumber(6))
	l.SetField(mod, "JULY", lua.LNumber(7))
	l.SetField(mod, "AUGUST", lua.LNumber(8))
	l.SetField(mod, "SEPTEMBER", lua.LNumber(9))
	l.SetField(mod, "OCTOBER", lua.LNumber(10))
	l.SetField(mod, "NOVEMBER", lua.LNumber(11))
	l.SetField(mod, "DECEMBER", lua.LNumber(12))

	// Register weekday constants
	l.SetField(mod, "SUNDAY", lua.LNumber(0))
	l.SetField(mod, "MONDAY", lua.LNumber(1))
	l.SetField(mod, "TUESDAY", lua.LNumber(2))
	l.SetField(mod, "WEDNESDAY", lua.LNumber(3))
	l.SetField(mod, "THURSDAY", lua.LNumber(4))
	l.SetField(mod, "FRIDAY", lua.LNumber(5))
	l.SetField(mod, "SATURDAY", lua.LNumber(6))

	sleepFunc := sleep
	if engine.IsCoroutineVM(l) {
		sleepFunc = sleepCoroutine
	}

	// Register time functions
	l.SetFuncs(mod, map[string]lua.LGFunction{
		"now":   now,
		"sleep": sleepFunc,
		"date":  date,
		"unix":  unix,
		"parse": parse,

		// requires async layer!
		"after": after,
	})
}
