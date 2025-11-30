package time

import (
	"context"
	"testing"
	stdtime "time"

	"github.com/wippyai/runtime/api/workflow"
	lua "github.com/yuin/gopher-lua"
)

func TestBind(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Bind(l)

	mod := l.GetGlobal("time")
	if mod.Type() != lua.LTTable {
		t.Fatal("time module not registered")
	}

	tbl := mod.(*lua.LTable)

	// Check duration constants
	constants := []string{"NANOSECOND", "MICROSECOND", "MILLISECOND", "SECOND", "MINUTE", "HOUR"}
	for _, c := range constants {
		if tbl.RawGetString(c).Type() != lua.LTNumber {
			t.Errorf("%s constant not registered", c)
		}
	}

	// Check format constants
	formats := []string{"RFC3339", "RFC3339NANO", "RFC822", "RFC822Z", "RFC850", "RFC1123", "RFC1123Z",
		"KITCHEN", "STAMP", "STAMP_MILLI", "STAMP_MICRO", "STAMP_NANO", "DATE_TIME", "DATE_ONLY", "TIME_ONLY"}
	for _, f := range formats {
		if tbl.RawGetString(f).Type() != lua.LTString {
			t.Errorf("%s format constant not registered", f)
		}
	}

	// Check month constants
	months := []string{"JANUARY", "FEBRUARY", "MARCH", "APRIL", "MAY", "JUNE",
		"JULY", "AUGUST", "SEPTEMBER", "OCTOBER", "NOVEMBER", "DECEMBER"}
	for _, m := range months {
		if tbl.RawGetString(m).Type() != lua.LTNumber {
			t.Errorf("%s month constant not registered", m)
		}
	}

	// Check weekday constants
	weekdays := []string{"SUNDAY", "MONDAY", "TUESDAY", "WEDNESDAY", "THURSDAY", "FRIDAY", "SATURDAY"}
	for _, w := range weekdays {
		if tbl.RawGetString(w).Type() != lua.LTNumber {
			t.Errorf("%s weekday constant not registered", w)
		}
	}

	// Check functions
	funcs := []string{"now", "date", "unix", "parse", "parse_duration", "load_location", "fixed_zone"}
	for _, fn := range funcs {
		if tbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}

	// Check location userdata
	if tbl.RawGetString("utc").Type() != lua.LTUserData {
		t.Error("utc location not registered")
	}
	if tbl.RawGetString("localtz").Type() != lua.LTUserData {
		t.Error("localtz location not registered")
	}
}

func TestNow(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.now()
		if type(t) ~= "userdata" then
			error("now() should return userdata")
		end
	`)
	if err != nil {
		t.Errorf("now test failed: %v", err)
	}
}

type mockTimeReference struct {
	now       stdtime.Time
	startTime stdtime.Time
}

func (m *mockTimeReference) Now() stdtime.Time       { return m.now }
func (m *mockTimeReference) StartTime() stdtime.Time { return m.startTime }

func TestNowWithTimeReference(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	fixedTime := stdtime.Date(2020, 6, 15, 10, 30, 0, 0, stdtime.UTC)
	mockRef := &mockTimeReference{now: fixedTime}

	ctx := context.Background()
	workflow.WithTimeReference(ctx, mockRef)
	l.SetContext(ctx)

	err := l.DoString(`
		local t = time.now()
		local year = t:year()
		-- Just verify we get a valid Time object
		if type(t) ~= "userdata" then
			error("expected userdata")
		end
	`)
	if err != nil {
		t.Errorf("now with time reference test failed: %v", err)
	}
}

func TestDate(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 30, 45, 0)
		if t:year() ~= 2024 then error("year mismatch") end
		if t:month() ~= 1 then error("month mismatch") end
		if t:day() ~= 15 then error("day mismatch") end
		if t:hour() ~= 10 then error("hour mismatch") end
		if t:minute() ~= 30 then error("minute mismatch") end
		if t:second() ~= 45 then error("second mismatch") end
	`)
	if err != nil {
		t.Errorf("date test failed: %v", err)
	}
}

func TestDateWithLocation(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 30, 45, 0, time.utc)
		local loc = t:location()
		if loc:string() ~= "UTC" then error("location mismatch: " .. loc:string()) end
	`)
	if err != nil {
		t.Errorf("date with location test failed: %v", err)
	}
}

func TestDateInvalidLocation(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`time.date(2024, 1, 15, 10, 30, 45, 0, "not a location")`)
	if err == nil {
		t.Error("expected error for invalid location")
	}
}

func TestUnix(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.unix(1704067200, 0)
		if t:unix() ~= 1704067200 then error("unix mismatch") end
	`)
	if err != nil {
		t.Errorf("unix test failed: %v", err)
	}
}

func TestParse(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t, err = time.parse(time.RFC3339, "2024-01-15T10:30:00Z")
		if not t then error(err) end
		if t:year() ~= 2024 then error("year mismatch") end
	`)
	if err != nil {
		t.Errorf("parse test failed: %v", err)
	}
}

func TestParseWithLocation(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t, err = time.parse(time.DATE_TIME, "2024-01-15 10:30:00", time.utc)
		if not t then error(err) end
		if t:location():string() ~= "UTC" then error("location mismatch") end
	`)
	if err != nil {
		t.Errorf("parse with location test failed: %v", err)
	}
}

func TestParseInvalidLocation(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`time.parse(time.RFC3339, "2024-01-15T10:30:00Z", "invalid")`)
	if err == nil {
		t.Error("expected error for invalid location")
	}
}

func TestParseInvalidFormat(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t, err = time.parse(time.RFC3339, "invalid")
		if t then error("expected nil for invalid format") end
		if not err then error("expected error") end
	`)
	if err != nil {
		t.Errorf("parse invalid format test failed: %v", err)
	}
}

func TestParseDuration(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, err = time.parse_duration("1h30m")
		if not d then error(err) end
		if d:hours() < 1.4 or d:hours() > 1.6 then error("hours mismatch") end
	`)
	if err != nil {
		t.Errorf("parse_duration test failed: %v", err)
	}
}

func TestParseDurationFromNumber(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, err = time.parse_duration(1000000000)
		if not d then error(err) end
		if d:seconds() ~= 1 then error("seconds mismatch: " .. d:seconds()) end
	`)
	if err != nil {
		t.Errorf("parse_duration from number test failed: %v", err)
	}
}

func TestParseDurationInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, err = time.parse_duration("invalid")
		if d then error("expected nil for invalid duration") end
		if not err then error("expected error") end
	`)
	if err != nil {
		t.Errorf("parse_duration invalid test failed: %v", err)
	}
}

func TestLoadLocation(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local loc, err = time.load_location("America/New_York")
		if not loc then error(err) end
		if loc:string() ~= "America/New_York" then error("location mismatch") end
	`)
	if err != nil {
		t.Errorf("load_location test failed: %v", err)
	}
}

func TestLoadLocationEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local loc, err = time.load_location("")
		if loc then error("expected nil for empty location") end
		if not err then error("expected error") end
	`)
	if err != nil {
		t.Errorf("load_location empty test failed: %v", err)
	}
}

func TestLoadLocationInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local loc, err = time.load_location("Invalid/Location")
		if loc then error("expected nil for invalid location") end
		if not err then error("expected error") end
	`)
	if err != nil {
		t.Errorf("load_location invalid test failed: %v", err)
	}
}

func TestFixedZone(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local loc = time.fixed_zone("EST", -5*3600)
		if loc:string() ~= "EST" then error("zone name mismatch") end
	`)
	if err != nil {
		t.Errorf("fixed_zone test failed: %v", err)
	}
}

func TestDurationMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h30m45s")

		-- Test all methods
		local ns = d:nanoseconds()
		local us = d:microseconds()
		local ms = d:milliseconds()
		local s = d:seconds()
		local m = d:minutes()
		local h = d:hours()

		if ns <= 0 then error("nanoseconds should be > 0") end
		if us <= 0 then error("microseconds should be > 0") end
		if ms <= 0 then error("milliseconds should be > 0") end
		if s < 5445 then error("seconds mismatch") end
		if m < 90 then error("minutes mismatch") end
		if h < 1.5 then error("hours mismatch") end
	`)
	if err != nil {
		t.Errorf("duration methods test failed: %v", err)
	}
}

func TestLocationMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local utcStr = time.utc:string()
		if utcStr ~= "UTC" then error("UTC string mismatch: " .. utcStr) end

		local localStr = time.localtz:string()
		if localStr == "" then error("local timezone string empty") end
	`)
	if err != nil {
		t.Errorf("location methods test failed: %v", err)
	}
}

func TestTimeAdd(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)

		-- Add with duration userdata
		local d, _ = time.parse_duration("1h")
		local t2 = t:add(d)
		if t2:hour() ~= 11 then error("add duration mismatch") end

		-- Add with string
		local t3 = t:add("30m")
		if t3:minute() ~= 30 then error("add string mismatch") end

		-- Add with number (nanoseconds)
		local t4 = t:add(time.HOUR)
		if t4:hour() ~= 11 then error("add number mismatch") end
	`)
	if err != nil {
		t.Errorf("time add test failed: %v", err)
	}
}

func TestTimeAddInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		t:add({})
	`)
	if err == nil {
		t.Error("expected error for invalid add argument")
	}
}

func TestTimeSub(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t1 = time.date(2024, 1, 15, 12, 0, 0, 0, time.utc)
		local t2 = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		local d = t1:sub(t2)
		if d:hours() ~= 2 then error("sub mismatch: " .. d:hours()) end
	`)
	if err != nil {
		t.Errorf("time sub test failed: %v", err)
	}
}

func TestTimeSubInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		t:sub("not a time")
	`)
	if err == nil {
		t.Error("expected error for invalid sub argument")
	}
}

func TestTimeAddDate(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		local t2 = t:add_date(1, 2, 3)
		if t2:year() ~= 2025 then error("year mismatch") end
		if t2:month() ~= 3 then error("month mismatch") end
		if t2:day() ~= 18 then error("day mismatch") end
	`)
	if err != nil {
		t.Errorf("time add_date test failed: %v", err)
	}
}

func TestTimeComparisons(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t1 = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		local t2 = time.date(2024, 1, 15, 12, 0, 0, 0, time.utc)
		local t3 = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)

		if not t2:after(t1) then error("after mismatch") end
		if not t1:before(t2) then error("before mismatch") end
		if not t1:equal(t3) then error("equal mismatch") end
		if t1:equal(t2) then error("should not be equal") end
	`)
	if err != nil {
		t.Errorf("time comparisons test failed: %v", err)
	}
}

func TestTimeComparisonInvalidAfter(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		t:after("not a time")
	`)
	if err == nil {
		t.Error("expected error for invalid after argument")
	}
}

func TestTimeComparisonInvalidBefore(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		t:before("not a time")
	`)
	if err == nil {
		t.Error("expected error for invalid before argument")
	}
}

func TestTimeComparisonInvalidEqual(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		t:equal("not a time")
	`)
	if err == nil {
		t.Error("expected error for invalid equal argument")
	}
}

func TestTimeFormat(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 30, 45, 0, time.utc)

		local str = t:format(time.RFC3339)
		if not str:find("2024") then error("format mismatch") end

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
	Bind(l)

	err := l.DoString(`
		local t = time.unix(1704067200, 123456789)

		if t:unix() ~= 1704067200 then error("unix mismatch") end

		local ns = t:unix_nano()
		if ns <= 0 then error("unix_nano should be > 0") end
	`)
	if err != nil {
		t.Errorf("time unix methods test failed: %v", err)
	}
}

func TestTimeDateClock(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 3, 25, 14, 30, 45, 0, time.utc)

		local y, m, d = t:date()
		if y ~= 2024 then error("date year mismatch") end
		if m ~= 3 then error("date month mismatch") end
		if d ~= 25 then error("date day mismatch") end

		local h, mn, s = t:clock()
		if h ~= 14 then error("clock hour mismatch") end
		if mn ~= 30 then error("clock minute mismatch") end
		if s ~= 45 then error("clock second mismatch") end
	`)
	if err != nil {
		t.Errorf("time date/clock test failed: %v", err)
	}
}

func TestTimeAccessors(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 6, 15, 10, 30, 45, 123456789, time.utc)

		if t:year() ~= 2024 then error("year mismatch") end
		if t:month() ~= 6 then error("month mismatch") end
		if t:day() ~= 15 then error("day mismatch") end
		if t:hour() ~= 10 then error("hour mismatch") end
		if t:minute() ~= 30 then error("minute mismatch") end
		if t:second() ~= 45 then error("second mismatch") end
		if t:nanosecond() ~= 123456789 then error("nanosecond mismatch") end

		-- Saturday = 6
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
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		if t:is_zero() then error("should not be zero") end

		local zero = time.unix(0, 0)
		-- Note: Unix epoch is not Go's zero time, so this is false
		-- Go's zero time is January 1, year 1, 00:00:00 UTC
	`)
	if err != nil {
		t.Errorf("time is_zero test failed: %v", err)
	}
}

func TestTimeInLocation(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)

		local loc, _ = time.load_location("America/New_York")
		local t2 = t:in_location(loc)

		-- Should be 5 hours behind
		if t2:hour() ~= 5 then error("in_location hour mismatch: " .. t2:hour()) end

		local loc2 = t2:location()
		if loc2:string() ~= "America/New_York" then error("location mismatch") end
	`)
	if err != nil {
		t.Errorf("time in_location test failed: %v", err)
	}
}

func TestTimeInLocationInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		t:in_location("not a location")
	`)
	if err == nil {
		t.Error("expected error for invalid in_location argument")
	}
}

func TestTimeUTCLocal(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.localtz)

		local utc = t:utc()
		if utc:location():string() ~= "UTC" then error("utc location mismatch") end

		local loc = t:in_local()
		-- Just verify it returns a valid time
		if loc:year() ~= 2024 then error("in_local year mismatch") end
	`)
	if err != nil {
		t.Errorf("time utc/local test failed: %v", err)
	}
}

func TestTimeRoundTruncate(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 33, 45, 0, time.utc)

		local d, _ = time.parse_duration("1h")

		local rounded = t:round(d)
		if rounded:hour() ~= 11 then error("round mismatch: " .. rounded:hour()) end

		local truncated = t:truncate(d)
		if truncated:hour() ~= 10 then error("truncate mismatch: " .. truncated:hour()) end
	`)
	if err != nil {
		t.Errorf("time round/truncate test failed: %v", err)
	}
}

func TestTimeRoundInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		t:round("not a duration")
	`)
	if err == nil {
		t.Error("expected error for invalid round argument")
	}
}

func TestTimeTruncateInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 0, 0, 0, time.utc)
		t:truncate("not a duration")
	`)
	if err == nil {
		t.Error("expected error for invalid truncate argument")
	}
}

func TestTimeFormat2(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.date(2024, 1, 15, 10, 30, 0, 0, time.utc)
		local str = t:format(time.RFC3339)
		if not str:find("2024") then error("format should contain year") end
	`)
	if err != nil {
		t.Errorf("time format test failed: %v", err)
	}
}

func TestDurationConstants(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

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
	Bind(l)

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
	Bind(l)

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
	Bind(l)

	err := l.DoString(`
		if time.SUNDAY ~= 0 then error("SUNDAY mismatch") end
		if time.SATURDAY ~= 6 then error("SATURDAY mismatch") end
	`)
	if err != nil {
		t.Errorf("weekday constants test failed: %v", err)
	}
}

func TestInvalidTimeArg(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	tests := []string{
		`time.date(2024, 1, 15, 10, 0, 0, 0, time.utc):add({})`,
		`time.date(2024, 1, 15, 10, 0, 0, 0, time.utc):sub({})`,
	}

	for _, test := range tests {
		err := l.DoString(test)
		if err == nil {
			t.Errorf("expected error for: %s", test)
		}
	}
}

func TestDurationInvalidArg(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, err = time.parse_duration({})
		if d then error("expected nil") end
		if not err then error("expected error") end
	`)
	if err != nil {
		t.Errorf("duration invalid arg test failed: %v", err)
	}
}

func TestRebind(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Bind(l)
	Bind(l)

	mod := l.GetGlobal("time")
	if mod.Type() != lua.LTTable {
		t.Fatal("time module not registered after rebind")
	}
}

func TestTimeAddInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.add(nil, "1h")
	`)
	if err == nil {
		t.Error("expected error when calling add on non-time")
	}
}

func TestTimeSubInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.sub(nil, time.now())
	`)
	if err == nil {
		t.Error("expected error when calling sub on non-time")
	}
}

func TestTimeAddDateInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.add_date(nil, 1, 0, 0)
	`)
	if err == nil {
		t.Error("expected error when calling add_date on non-time")
	}
}

func TestTimeAfterInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.after(nil, time.now())
	`)
	if err == nil {
		t.Error("expected error when calling after on non-time")
	}
}

func TestTimeBeforeInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.before(nil, time.now())
	`)
	if err == nil {
		t.Error("expected error when calling before on non-time")
	}
}

func TestTimeEqualInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.equal(nil, time.now())
	`)
	if err == nil {
		t.Error("expected error when calling equal on non-time")
	}
}

func TestTimeFormatInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.format(nil, time.RFC3339)
	`)
	if err == nil {
		t.Error("expected error when calling format on non-time")
	}
}

func TestTimeFormatRFC3339InvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.format_rfc3339(nil)
	`)
	if err == nil {
		t.Error("expected error when calling format_rfc3339 on non-time")
	}
}

func TestTimeUnixInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.unix(nil)
	`)
	if err == nil {
		t.Error("expected error when calling unix on non-time")
	}
}

func TestTimeUnixNanoInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.unix_nano(nil)
	`)
	if err == nil {
		t.Error("expected error when calling unix_nano on non-time")
	}
}

func TestTimeDateInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.date(nil)
	`)
	if err == nil {
		t.Error("expected error when calling date on non-time")
	}
}

func TestTimeClockInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.clock(nil)
	`)
	if err == nil {
		t.Error("expected error when calling clock on non-time")
	}
}

func TestTimeYearInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.year(nil)
	`)
	if err == nil {
		t.Error("expected error when calling year on non-time")
	}
}

func TestTimeMonthInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.month(nil)
	`)
	if err == nil {
		t.Error("expected error when calling month on non-time")
	}
}

func TestTimeDayInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.day(nil)
	`)
	if err == nil {
		t.Error("expected error when calling day on non-time")
	}
}

func TestTimeHourInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.hour(nil)
	`)
	if err == nil {
		t.Error("expected error when calling hour on non-time")
	}
}

func TestTimeMinuteInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.minute(nil)
	`)
	if err == nil {
		t.Error("expected error when calling minute on non-time")
	}
}

func TestTimeSecondInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.second(nil)
	`)
	if err == nil {
		t.Error("expected error when calling second on non-time")
	}
}

func TestTimeNanosecondInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.nanosecond(nil)
	`)
	if err == nil {
		t.Error("expected error when calling nanosecond on non-time")
	}
}

func TestTimeWeekdayInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.weekday(nil)
	`)
	if err == nil {
		t.Error("expected error when calling weekday on non-time")
	}
}

func TestTimeYearDayInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.year_day(nil)
	`)
	if err == nil {
		t.Error("expected error when calling year_day on non-time")
	}
}

func TestTimeIsZeroInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.is_zero(nil)
	`)
	if err == nil {
		t.Error("expected error when calling is_zero on non-time")
	}
}

func TestTimeInLocationInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.in_location(nil, time.utc)
	`)
	if err == nil {
		t.Error("expected error when calling in_location on non-time")
	}
}

func TestTimeLocationInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.location(nil)
	`)
	if err == nil {
		t.Error("expected error when calling location on non-time")
	}
}

func TestTimeUTCInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.utc(nil)
	`)
	if err == nil {
		t.Error("expected error when calling utc on non-time")
	}
}

func TestTimeLocalInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.in_local(nil)
	`)
	if err == nil {
		t.Error("expected error when calling in_local on non-time")
	}
}

func TestTimeRoundInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.round(nil, d)
	`)
	if err == nil {
		t.Error("expected error when calling round on non-time")
	}
}

func TestTimeTruncateInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d, _ = time.parse_duration("1h")
		d.truncate(nil, d)
	`)
	if err == nil {
		t.Error("expected error when calling truncate on non-time")
	}
}

func TestDurationNanosecondsInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.now()
		t.nanoseconds(nil)
	`)
	if err == nil {
		t.Error("expected error when calling nanoseconds on non-duration")
	}
}

func TestDurationMicrosecondsInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.now()
		t.microseconds(nil)
	`)
	if err == nil {
		t.Error("expected error when calling microseconds on non-duration")
	}
}

func TestDurationMillisecondsInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.now()
		t.milliseconds(nil)
	`)
	if err == nil {
		t.Error("expected error when calling milliseconds on non-duration")
	}
}

func TestDurationSecondsInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.now()
		t.seconds(nil)
	`)
	if err == nil {
		t.Error("expected error when calling seconds on non-duration")
	}
}

func TestDurationMinutesInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.now()
		t.minutes(nil)
	`)
	if err == nil {
		t.Error("expected error when calling minutes on non-duration")
	}
}

func TestDurationHoursInvalidSelf(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.now()
		t.hours(nil)
	`)
	if err == nil {
		t.Error("expected error when calling hours on non-duration")
	}
}

func TestLocationStringInvalidArg(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local t = time.now()
		t.string(nil)
	`)
	if err == nil {
		t.Error("expected error when calling string on non-location")
	}
}

func TestParseDurationWithDuration(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local d1, _ = time.parse_duration("1h")
		local d2, _ = time.parse_duration(d1)
		if d2:hours() ~= 1 then error("should be 1 hour") end
	`)
	if err != nil {
		t.Errorf("parse_duration with duration test failed: %v", err)
	}
}
