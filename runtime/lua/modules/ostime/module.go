// SPDX-License-Identifier: MPL-2.0

package ostime

import (
	"fmt"
	"strings"
	"time"

	lua "github.com/wippyai/go-lua"
	clockapi "github.com/wippyai/runtime/api/clock"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

var startTime = time.Now()

// Module is the os time module definition.
var Module = &luaapi.ModuleDef{
	Name:        "os",
	Description: "Lua os.time, os.date, os.clock, os.difftime functions",
	Class:       []string{luaapi.ClassTime, luaapi.ClassWorkflow},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		tbl := &lua.LTable{}
		tbl.RawSetString("time", lua.LGoFunc(osTime))
		tbl.RawSetString("date", lua.LGoFunc(osDate))
		tbl.RawSetString("clock", lua.LGoFunc(osClock))
		tbl.RawSetString("difftime", lua.LGoFunc(osDifftime))
		tbl.RawSetString("platform", lua.LString("wippy"))
		tbl.Immutable = true
		return tbl, nil
	},
	Types: ModuleTypes,
}

// getNow returns the current time, using TimeReference if available for deterministic behavior.
func getNow(l *lua.LState) time.Time {
	if ref := clockapi.GetTimeReference(l.Context()); ref != nil {
		return ref.Now()
	}
	return time.Now()
}

func osClock(l *lua.LState) int {
	elapsed := getNow(l).Sub(startTime).Seconds()
	l.Push(lua.LNumber(elapsed))
	return 1
}

func osDifftime(l *lua.LState) int {
	t2 := l.CheckNumber(1)
	t1 := l.CheckNumber(2)
	l.Push(t2 - t1)
	return 1
}

func osTime(l *lua.LState) int {
	now := getNow(l)
	if l.GetTop() == 0 {
		l.Push(lua.LNumber(now.Unix()))
		return 1
	}

	tbl := l.CheckTable(1)

	year := getIntField(tbl, "year", now.Year())
	month := getIntField(tbl, "month", int(now.Month()))
	day := getIntField(tbl, "day", now.Day())
	hour := getIntField(tbl, "hour", 0)
	minute := getIntField(tbl, "min", 0)
	sec := getIntField(tbl, "sec", 0)

	t := time.Date(year, time.Month(month), day, hour, minute, sec, 0, time.Local)
	l.Push(lua.LNumber(t.Unix()))
	return 1
}

func getIntField(table *lua.LTable, key string, defaultValue int) int {
	v := table.RawGetString(key)
	switch n := v.(type) {
	case lua.LNumber:
		return int(n)
	case lua.LInteger:
		return int(n)
	default:
		return defaultValue
	}
}

func osDate(l *lua.LState) int {
	format := "%c"
	if l.GetTop() >= 1 {
		format = l.CheckString(1)
	}

	var t time.Time
	if l.GetTop() >= 2 {
		timestamp := l.CheckNumber(2)
		t = time.Unix(int64(timestamp), 0)
	} else {
		t = getNow(l)
	}

	utc := false
	if strings.HasPrefix(format, "!") {
		utc = true
		format = format[1:]
	}

	if utc {
		t = t.UTC()
	}

	if format == "*t" {
		return osDateTable(l, t)
	}

	result := formatDate(format, t)
	l.Push(lua.LString(result))
	return 1
}

func osDateTable(l *lua.LState, t time.Time) int {
	tbl := l.CreateTable(0, 9)

	tbl.RawSetString("year", lua.LNumber(t.Year()))
	tbl.RawSetString("month", lua.LNumber(t.Month()))
	tbl.RawSetString("day", lua.LNumber(t.Day()))
	tbl.RawSetString("hour", lua.LNumber(t.Hour()))
	tbl.RawSetString("min", lua.LNumber(t.Minute()))
	tbl.RawSetString("sec", lua.LNumber(t.Second()))
	tbl.RawSetString("wday", lua.LNumber(t.Weekday()+1))
	tbl.RawSetString("yday", lua.LNumber(t.YearDay()))

	_, isDST := t.Zone()
	tbl.RawSetString("isdst", lua.LBool(isDST != 0))

	l.Push(tbl)
	return 1
}

func formatDate(format string, t time.Time) string {
	switch format {
	case "%c":
		return t.Format("Mon Jan _2 15:04:05 2006")
	case "%x":
		return t.Format("01/02/06")
	case "%X":
		return t.Format("15:04:05")
	}

	var b strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] == '%' && i+1 < len(format) {
			i++
			b.WriteString(formatSpecifier(format[i], t))
		} else {
			b.WriteByte(format[i])
		}
	}
	return b.String()
}

func formatSpecifier(specifier byte, t time.Time) string {
	switch specifier {
	case 'a':
		return t.Format("Mon")
	case 'A':
		return t.Format("Monday")
	case 'b':
		return t.Format("Jan")
	case 'B':
		return t.Format("January")
	case 'c':
		return t.Format("Mon Jan _2 15:04:05 2006")
	case 'd':
		return t.Format("02")
	case 'H':
		return t.Format("15")
	case 'I':
		return t.Format("03")
	case 'j':
		return fmt.Sprintf("%03d", t.YearDay())
	case 'm':
		return t.Format("01")
	case 'M':
		return t.Format("04")
	case 'p':
		return t.Format("PM")
	case 'S':
		return t.Format("05")
	case 'U':
		_, week := t.ISOWeek()
		return fmt.Sprintf("%02d", week)
	case 'w':
		return fmt.Sprintf("%d", t.Weekday())
	case 'W':
		_, week := t.ISOWeek()
		return fmt.Sprintf("%02d", week)
	case 'x':
		return t.Format("01/02/06")
	case 'X':
		return t.Format("15:04:05")
	case 'y':
		return t.Format("06")
	case 'Y':
		return t.Format("2006")
	case 'z':
		return t.Format("-0700")
	case 'Z':
		zone, _ := t.Zone()
		return zone
	case '%':
		return "%"
	default:
		return "%" + string(specifier)
	}
}
