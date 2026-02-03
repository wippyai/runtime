package time

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	mod := l.GetGlobal("time")
	if mod.Type() != lua.LTTable {
		t.Fatal("time module not registered")
	}

	modTbl := mod.(*lua.LTable)

	// Duration constants
	constants := []string{"NANOSECOND", "MICROSECOND", "MILLISECOND", "SECOND", "MINUTE", "HOUR"}
	for _, c := range constants {
		if modTbl.RawGetString(c).Type() != lua.LTNumber {
			t.Errorf("%s constant not registered", c)
		}
	}

	// Format constants
	formats := []string{"RFC3339", "RFC3339NANO", "RFC822", "RFC822Z", "RFC850", "RFC1123", "RFC1123Z",
		"KITCHEN", "STAMP", "STAMP_MILLI", "STAMP_MICRO", "STAMP_NANO", "DATE_TIME", "DATE_ONLY", "TIME_ONLY"}
	for _, f := range formats {
		if modTbl.RawGetString(f).Type() != lua.LTString {
			t.Errorf("%s format constant not registered", f)
		}
	}

	// Month constants
	months := []string{"JANUARY", "FEBRUARY", "MARCH", "APRIL", "MAY", "JUNE",
		"JULY", "AUGUST", "SEPTEMBER", "OCTOBER", "NOVEMBER", "DECEMBER"}
	for _, m := range months {
		if modTbl.RawGetString(m).Type() != lua.LTNumber {
			t.Errorf("%s month constant not registered", m)
		}
	}

	// Weekday constants
	weekdays := []string{"SUNDAY", "MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY"}
	for _, w := range weekdays {
		if modTbl.RawGetString(w).Type() != lua.LTNumber {
			t.Errorf("%s weekday constant not registered", w)
		}
	}

	// Functions
	funcs := []string{"now", "date", "unix", "parse", "parse_duration", "load_location", "fixed_zone", "sleep", "timer", "ticker"}
	for _, fn := range funcs {
		if modTbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}

	// Location userdata
	if modTbl.RawGetString("utc").Type() != lua.LTUserData {
		t.Error("utc location not registered")
	}
	if modTbl.RawGetString("localtz").Type() != lua.LTUserData {
		t.Error("localtz location not registered")
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	tbl, _ := Module.Build()
	l1.SetGlobal(Module.Name, tbl)
	l2.SetGlobal(Module.Name, tbl)

	mod1 := l1.GetGlobal("time").(*lua.LTable)
	mod2 := l2.GetGlobal("time").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestNow(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.now()
		if type(t) ~= "userdata" then
			error("now() should return userdata")
		end
		if t:hour() < 0 or t:hour() > 23 then
			error("invalid hour")
		end
	`)
	if err != nil {
		t.Errorf("now test failed: %v", err)
	}
}

func TestDate(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(2024, 12, 29, 15, 4, 5, 0, time.utc)
		if t:year() ~= 2024 then error("year mismatch") end
		if t:month() ~= 12 then error("month mismatch") end
		if t:day() ~= 29 then error("day mismatch") end
		if t:hour() ~= 15 then error("hour mismatch") end
		if t:minute() ~= 4 then error("minute mismatch") end
		if t:second() ~= 5 then error("second mismatch") end
		if t:nanosecond() ~= 0 then error("nanosecond mismatch") end
	`)
	if err != nil {
		t.Errorf("date test failed: %v", err)
	}
}

func TestUnix(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.unix(1735484645, 0)
		local utc_t = t:utc()
		if utc_t:year() ~= 2024 then error("year mismatch: " .. utc_t:year()) end
		if utc_t:month() ~= 12 then error("month mismatch") end
		if utc_t:day() ~= 29 then error("day mismatch") end
		if utc_t:hour() ~= 15 then error("hour mismatch: " .. utc_t:hour()) end
		if utc_t:minute() ~= 4 then error("minute mismatch") end
		if utc_t:second() ~= 5 then error("second mismatch") end
	`)
	if err != nil {
		t.Errorf("unix test failed: %v", err)
	}
}

func TestParse(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t, err = time.parse("2006-01-02 15:04:05", "2024-12-29 15:04:05")
		if err then error(tostring(err)) end
		if t:year() ~= 2024 then error("year mismatch") end
		if t:month() ~= 12 then error("month mismatch") end
		if t:day() ~= 29 then error("day mismatch") end
	`)
	if err != nil {
		t.Errorf("parse test failed: %v", err)
	}
}

func TestParseError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t, err = time.parse("2006-01-02", "invalid-date")
		if t ~= nil then error("expected nil for invalid parse") end
		if err == nil then error("expected error") end
	`)
	if err != nil {
		t.Errorf("parse error test failed: %v", err)
	}
}

func TestParseDuration(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local d, err = time.parse_duration("1h30m")
		if err then error(tostring(err)) end
		if d:hours() < 1.4 or d:hours() > 1.6 then error("hours mismatch") end
		if d:minutes() < 89 or d:minutes() > 91 then error("minutes mismatch") end
	`)
	if err != nil {
		t.Errorf("parse_duration test failed: %v", err)
	}
}

func TestParseDurationFromNumber(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local d, err = time.parse_duration(time.SECOND)
		if err then error(tostring(err)) end
		if d:seconds() ~= 1 then error("seconds mismatch: " .. d:seconds()) end
	`)
	if err != nil {
		t.Errorf("parse_duration from number test failed: %v", err)
	}
}

func TestLoadLocation(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local loc, err = time.load_location("America/New_York")
		if err then error(tostring(err)) end
		if loc:string() ~= "America/New_York" then error("location mismatch") end
	`)
	if err != nil {
		t.Errorf("load_location test failed: %v", err)
	}
}

func TestLoadLocationError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local loc, err = time.load_location("Invalid/Location")
		if loc ~= nil then error("expected nil for invalid location") end
		if err == nil then error("expected error") end
	`)
	if err != nil {
		t.Errorf("load_location error test failed: %v", err)
	}
}

func TestFixedZone(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local loc = time.fixed_zone("EST", -5*3600)
		if loc:string() ~= "EST" then error("zone name mismatch") end
	`)
	if err != nil {
		t.Errorf("fixed_zone test failed: %v", err)
	}
}

func TestTimeAdd(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(2024, 12, 29, 15, 0, 0, 0, time.utc)
		local d = time.parse_duration("1h")
		local new_t = t:add(d)
		if new_t:hour() ~= 16 then error("add hour mismatch: " .. new_t:hour()) end
	`)
	if err != nil {
		t.Errorf("time add test failed: %v", err)
	}
}

func TestTimeAddString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(2024, 12, 29, 15, 0, 0, 0, time.utc)
		local new_t = t:add("30m")
		if new_t:minute() ~= 30 then error("add string mismatch") end
	`)
	if err != nil {
		t.Errorf("time add string test failed: %v", err)
	}
}

func TestTimeAddNumber(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(2024, 12, 29, 15, 0, 0, 0, time.utc)
		local new_t = t:add(time.HOUR)
		if new_t:hour() ~= 16 then error("add number mismatch") end
	`)
	if err != nil {
		t.Errorf("time add number test failed: %v", err)
	}
}

func TestTimeSub(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t1 = time.date(2024, 12, 29, 16, 0, 0, 0, time.utc)
		local t2 = time.date(2024, 12, 29, 15, 0, 0, 0, time.utc)
		local d = t1:sub(t2)
		if d:hours() ~= 1 then error("sub hours mismatch: " .. d:hours()) end
	`)
	if err != nil {
		t.Errorf("time sub test failed: %v", err)
	}
}

func TestTimeAddDate(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)
		local new_t = t:add_date(1, 2, 3)
		if new_t:year() ~= 2025 then error("add_date year mismatch") end
		if new_t:month() ~= 3 then error("add_date month mismatch") end
		if new_t:day() ~= 4 then error("add_date day mismatch") end
	`)
	if err != nil {
		t.Errorf("time add_date test failed: %v", err)
	}
}

func TestTimeComparisons(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t1 = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)
		local t2 = time.date(2024, 1, 2, 0, 0, 0, 0, time.utc)
		local t3 = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)

		if not t1:before(t2) then error("before mismatch") end
		if not t2:after(t1) then error("after mismatch") end
		if not t1:equal(t3) then error("equal mismatch") end
		if t1:equal(t2) then error("should not be equal") end
	`)
	if err != nil {
		t.Errorf("time comparisons test failed: %v", err)
	}
}

func TestTimeFormat(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(2024, 12, 29, 15, 4, 5, 0, time.utc)
		local str = t:format("Mon Jan 2 15:04:05 MST 2006")
		if str ~= "Sun Dec 29 15:04:05 UTC 2024" then error("format mismatch: " .. str) end

		local rfc = t:format_rfc3339()
		if not rfc:find("2024") then error("format_rfc3339 mismatch") end
	`)
	if err != nil {
		t.Errorf("time format test failed: %v", err)
	}
}

func TestTimeUnixMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(1970, 1, 1, 0, 0, 1, 0, time.utc)
		if t:unix() ~= 1 then error("unix mismatch") end

		local t2 = time.date(1970, 1, 1, 0, 0, 0, 1, time.utc)
		if t2:unix_nano() ~= 1 then error("unix_nano mismatch") end
	`)
	if err != nil {
		t.Errorf("time unix methods test failed: %v", err)
	}
}

func TestTimeDateClock(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(2024, 12, 29, 15, 4, 5, 0, time.utc)

		local y, m, d = t:date()
		if y ~= 2024 then error("date year mismatch") end
		if m ~= 12 then error("date month mismatch") end
		if d ~= 29 then error("date day mismatch") end

		local h, mn, s = t:clock()
		if h ~= 15 then error("clock hour mismatch") end
		if mn ~= 4 then error("clock minute mismatch") end
		if s ~= 5 then error("clock second mismatch") end
	`)
	if err != nil {
		t.Errorf("time date/clock test failed: %v", err)
	}
}

func TestTimeAccessors(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(2024, 6, 15, 10, 30, 45, 123456789, time.utc)

		if t:year() ~= 2024 then error("year mismatch") end
		if t:month() ~= 6 then error("month mismatch") end
		if t:day() ~= 15 then error("day mismatch") end
		if t:hour() ~= 10 then error("hour mismatch") end
		if t:minute() ~= 30 then error("minute mismatch") end
		if t:second() ~= 45 then error("second mismatch") end
		if t:nanosecond() ~= 123456789 then error("nanosecond mismatch") end
		if t:weekday() ~= 6 then error("weekday mismatch: " .. t:weekday()) end
		if t:year_day() ~= 167 then error("year_day mismatch: " .. t:year_day()) end
	`)
	if err != nil {
		t.Errorf("time accessors test failed: %v", err)
	}
}

func TestTimeIsZero(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		if t:is_zero() then error("should not be zero") end
	`)
	if err != nil {
		t.Errorf("time is_zero test failed: %v", err)
	}
}

func TestTimeInLocation(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)
		local loc = time.load_location("America/New_York")
		local new_t = t:in_location(loc)
		if new_t:format("2006-01-02 15:04:05 MST") ~= "2023-12-31 19:00:00 EST" then
			error("in_location mismatch: " .. new_t:format("2006-01-02 15:04:05 MST"))
		end
	`)
	if err != nil {
		t.Errorf("time in_location test failed: %v", err)
	}
}

func TestTimeLocation(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)
		local loc = t:location()
		if tostring(loc) ~= "UTC" then error("location mismatch") end
	`)
	if err != nil {
		t.Errorf("time location test failed: %v", err)
	}
}

func TestTimeUTC(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local loc = time.load_location("America/New_York")
		local t = time.date(2024, 1, 1, 0, 0, 0, 0, loc)
		local utc = t:utc()
		if utc:format("2006-01-02 15:04:05 MST") ~= "2024-01-01 05:00:00 UTC" then
			error("utc mismatch: " .. utc:format("2006-01-02 15:04:05 MST"))
		end
	`)
	if err != nil {
		t.Errorf("time utc test failed: %v", err)
	}
}

func TestTimeLocal(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.date(2024, 1, 1, 0, 0, 0, 0, time.utc)
		local loc = t:in_local()
		if loc:year() ~= 2024 and loc:year() ~= 2023 then error("in_local year mismatch") end
	`)
	if err != nil {
		t.Errorf("time in_local test failed: %v", err)
	}
}

func TestTimeRound(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.parse("2006-01-02T15:04:05.999999999Z", "2024-01-01T12:34:56.789Z")
		local d = time.parse_duration("1s")
		local new_t = t:round(d)
		if new_t:format("2006-01-02T15:04:05Z") ~= "2024-01-01T12:34:57Z" then
			error("round mismatch: " .. new_t:format("2006-01-02T15:04:05Z"))
		end
	`)
	if err != nil {
		t.Errorf("time round test failed: %v", err)
	}
}

func TestTimeTruncate(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t = time.parse("2006-01-02T15:04:05.999999999Z", "2024-01-01T12:34:56.789Z")
		local d = time.parse_duration("1m")
		local new_t = t:truncate(d)
		if new_t:format("2006-01-02T15:04:05Z") ~= "2024-01-01T12:34:00Z" then
			error("truncate mismatch: " .. new_t:format("2006-01-02T15:04:05Z"))
		end
	`)
	if err != nil {
		t.Errorf("time truncate test failed: %v", err)
	}
}

func TestDurationMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local d = time.parse_duration("1h30m45s")

		if d:nanoseconds() <= 0 then error("nanoseconds should be > 0") end
		if d:microseconds() <= 0 then error("microseconds should be > 0") end
		if d:milliseconds() <= 0 then error("milliseconds should be > 0") end
		if d:seconds() < 5445 then error("seconds mismatch") end
		if d:minutes() < 90 then error("minutes mismatch") end
		if d:hours() < 1.5 then error("hours mismatch") end
	`)
	if err != nil {
		t.Errorf("duration methods test failed: %v", err)
	}
}

func TestLocationMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		if time.utc:string() ~= "UTC" then error("UTC string mismatch") end
		if time.localtz:string() == "" then error("local timezone string empty") end
	`)
	if err != nil {
		t.Errorf("location methods test failed: %v", err)
	}
}

func TestDurationConstants(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		if time.NANOSECOND ~= 1 then error("NANOSECOND mismatch") end
		if time.MICROSECOND ~= 1000 then error("MICROSECOND mismatch") end
		if time.MILLISECOND ~= 1000000 then error("MILLISECOND mismatch") end
		if time.SECOND ~= 1000000000 then error("SECOND mismatch") end
		if time.MINUTE ~= 60000000000 then error("MINUTE mismatch") end
		if time.HOUR ~= 3600000000000 then error("HOUR mismatch") end
	`)
	if err != nil {
		t.Errorf("duration constants test failed: %v", err)
	}
}

func TestFormatConstants(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		if time.RFC3339 ~= "2006-01-02T15:04:05Z07:00" then error("RFC3339 mismatch") end
		if time.DATE_TIME ~= "2006-01-02 15:04:05" then error("DATE_TIME mismatch") end
		if time.DATE_ONLY ~= "2006-01-02" then error("DATE_ONLY mismatch") end
		if time.TIME_ONLY ~= "15:04:05" then error("TIME_ONLY mismatch") end
	`)
	if err != nil {
		t.Errorf("format constants test failed: %v", err)
	}
}

func TestMonthConstants(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		if time.JANUARY ~= 1 then error("JANUARY mismatch") end
		if time.DECEMBER ~= 12 then error("DECEMBER mismatch") end
	`)
	if err != nil {
		t.Errorf("month constants test failed: %v", err)
	}
}

func TestWeekdayConstants(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		if time.SUNDAY ~= 0 then error("SUNDAY mismatch") end
		if time.SATURDAY ~= 6 then error("SATURDAY mismatch") end
	`)
	if err != nil {
		t.Errorf("weekday constants test failed: %v", err)
	}
}

func TestErrorHandling(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{
			name: "add invalid duration",
			script: `
				local t = time.now()
				local ok, err = pcall(function() return t:add({}) end)
				if ok then error("expected error") end
			`,
		},
		{
			name: "sub invalid time",
			script: `
				local t = time.now()
				local ok, err = pcall(function() return t:sub(123) end)
				if ok then error("expected error") end
			`,
		},
		{
			name: "after invalid time",
			script: `
				local t = time.now()
				local ok, err = pcall(function() return t:after(123) end)
				if ok then error("expected error") end
			`,
		},
		{
			name: "before invalid time",
			script: `
				local t = time.now()
				local ok, err = pcall(function() return t:before(123) end)
				if ok then error("expected error") end
			`,
		},
		{
			name: "equal invalid time",
			script: `
				local t = time.now()
				local ok, err = pcall(function() return t:equal(123) end)
				if ok then error("expected error") end
			`,
		},
		{
			name: "in_location invalid",
			script: `
				local t = time.now()
				local ok, err = pcall(function() return t:in_location(123) end)
				if ok then error("expected error") end
			`,
		},
		{
			name: "round invalid duration",
			script: `
				local t = time.now()
				local ok, err = pcall(function() return t:round(123) end)
				if ok then error("expected error") end
			`,
		},
		{
			name: "truncate invalid duration",
			script: `
				local t = time.now()
				local ok, err = pcall(function() return t:truncate(123) end)
				if ok then error("expected error") end
			`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			tbl, _ := Module.Build()
			l.SetGlobal(Module.Name, tbl)

			err := l.DoString(tc.script)
			if err != nil {
				t.Errorf("test failed: %v", err)
			}
		})
	}
}
