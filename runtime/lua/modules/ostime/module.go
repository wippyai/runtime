package ostime

import (
	"fmt"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// NewOSTimeModule creates and returns a new instance of the ostime Module
func NewOSTimeModule() *Module {
	return &Module{
		startTime: time.Now(),
	}
}

// Module represents the ostime module
type Module struct {
	startTime time.Time
}

// Name returns the module's name
func (m *Module) Name() string {
	return "ostime"
}

// Loader registers the module's functions into Lua state
// It extends the os table if it exists, or creates a new one
func (m *Module) Loader(L *lua.LState) int {
	// Check if os table already exists
	L.GetGlobal("os")
	osTable := L.Get(-1)
	if osTable.Type() == lua.LTNil {
		// Create os table if it doesn't exist
		osTable = L.NewTable()
		L.SetGlobal("os", osTable)
	}

	// Register os.time and os.date functions
	osTab := L.GetGlobal("os").(*lua.LTable)
	L.SetField(osTab, "time", L.NewFunction(osTime))
	L.SetField(osTab, "date", L.NewFunction(osDate))
	L.SetField(osTab, "clock", L.NewFunction(m.osClock))

	// We don't push anything on the stack as we're extending the os global
	return 0
}

// osClock implements os.clock() function
// Returns the elapsed time since the module was loaded
func (m *Module) osClock(L *lua.LState) int {
	elapsed := time.Since(m.startTime).Seconds()
	L.Push(lua.LNumber(elapsed))
	return 1
}

// osTime implements os.time() function
// In standard Lua:
// - Without arguments: returns current time
// - With table argument: returns time for specified date/time
func osTime(L *lua.LState) int {
	// Case: no args - return current time as Unix timestamp
	if L.GetTop() == 0 {
		L.Push(lua.LNumber(time.Now().Unix()))
		return 1
	}

	// Case: table arg - convert table fields to time
	tbl := L.CheckTable(1)

	year := getIntField(L, tbl, "year", time.Now().Year())
	month := getIntField(L, tbl, "month", int(time.Now().Month()))
	day := getIntField(L, tbl, "day", time.Now().Day())
	hour := getIntField(L, tbl, "hour", 0)
	mn := getIntField(L, tbl, "min", 0)
	sec := getIntField(L, tbl, "sec", 0)
	// Ignore isdst field as Go handles DST automatically

	// Create time using provided fields
	t := time.Date(year, time.Month(month), day, hour, mn, sec, 0, time.Local)
	L.Push(lua.LNumber(t.Unix()))
	return 1
}

// Helper to get integer field from table with default value
func getIntField(L *lua.LState, table *lua.LTable, key string, defaultValue int) int {
	if v := table.RawGetString(key); v.Type() == lua.LTNumber {
		return int(v.(lua.LNumber))
	}
	return defaultValue
}

// osDate implements os.date() function
// Format:
// - "%a" - abbreviated weekday name (e.g., Wed)
// - "%A" - full weekday name (e.g., Wednesday)
// - "%b" - abbreviated month name (e.g., Sep)
// - "%B" - full month name (e.g., September)
// - "%c" - date and time (e.g., Wed Sep 14 17:45:30 2022)
// - "%d" - day of month (e.g., 14)
// - "%H" - hour 24-hour (e.g., 17)
// - "%I" - hour 12-hour (e.g., 05)
// - "%j" - day of year (e.g., 257)
// - "%m" - month (e.g., 09)
// - "%M" - minute (e.g., 45)
// - "%p" - AM/PM (e.g., PM)
// - "%S" - second (e.g., 30)
// - "%U" - week number (Sunday as first day) (e.g., 37)
// - "%w" - weekday (0-6, Sunday is 0) (e.g., 3)
// - "%W" - week number (Monday as first day) (e.g., 37)
// - "%x" - date (e.g., 09/14/22)
// - "%X" - time (e.g., 17:45:30)
// - "%y" - year two digits (e.g., 22)
// - "%Y" - year (e.g., 2022)
// - "%z" - timezone (e.g., -0700)
// - "%Z" - timezone name (e.g., MST)
// - "%%" - percent sign
func osDate(L *lua.LState) int {
	// Get format string (default "%c")
	format := "%c"
	if L.GetTop() >= 1 {
		format = L.CheckString(1)
	}

	// Get time (default now)
	var t time.Time
	if L.GetTop() >= 2 {
		timestamp := L.CheckNumber(2)
		t = time.Unix(int64(timestamp), 0)
	} else {
		t = time.Now()
	}

	// Check for UTC flag (*) at start of format
	utc := false
	if strings.HasPrefix(format, "!") {
		utc = true
		format = format[1:]
	}

	// Use UTC if requested
	if utc {
		t = t.UTC()
	}

	// If format is "*t", return a table with date/time components
	if format == "*t" {
		return osDateTable(L, t)
	}

	// Otherwise format the date/time string
	result := formatDate(format, t)
	L.Push(lua.LString(result))
	return 1
}

// osDateTable returns a table with date/time components
func osDateTable(L *lua.LState, t time.Time) int {
	tbl := L.NewTable()

	// Set all date/time fields
	L.SetField(tbl, "year", lua.LNumber(t.Year()))
	L.SetField(tbl, "month", lua.LNumber(t.Month()))
	L.SetField(tbl, "day", lua.LNumber(t.Day()))
	L.SetField(tbl, "hour", lua.LNumber(t.Hour()))
	L.SetField(tbl, "min", lua.LNumber(t.Minute()))
	L.SetField(tbl, "sec", lua.LNumber(t.Second()))
	L.SetField(tbl, "wday", lua.LNumber(t.Weekday()+1)) // Lua uses 1-7 for weekdays
	L.SetField(tbl, "yday", lua.LNumber(t.YearDay()))

	// Set isdst (Daylight Saving Time flag)
	_, isDST := t.Zone()
	L.SetField(tbl, "isdst", lua.LBool(isDST != 0))

	L.Push(tbl)
	return 1
}

// formatDate implements simplified Lua os.date() formatting
func formatDate(format string, t time.Time) string {
	// Handle special case for standard formats
	if format == "%c" {
		return t.Format("Mon Jan _2 15:04:05 2006")
	} else if format == "%x" {
		return t.Format("01/02/06")
	} else if format == "%X" {
		return t.Format("15:04:05")
	}

	// Otherwise, handle each format specifier
	result := ""
	for i := 0; i < len(format); i++ {
		if format[i] == '%' && i+1 < len(format) {
			i++
			result += formatSpecifier(format[i], t)
		} else {
			result += string(format[i])
		}
	}
	return result
}

// formatSpecifier handles a single format specifier
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
