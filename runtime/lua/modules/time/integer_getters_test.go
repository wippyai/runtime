// SPDX-License-Identifier: MPL-2.0

package time

import (
	"testing"
	stdtime "time"

	lua "github.com/wippyai/go-lua"
)

func TestIntegerGettersReturnIntegers(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	cases := []struct {
		expr string
		want int64
	}{
		{`time.unix(1735484645, 250000000):unix_nano()`, 1735484645250000000},
		{`time.unix(1735484645, 250000000):unix()`, 1735484645},
		{`time.unix(1735484645, 250000000):unix_nano() // 1000000`, 1735484645250},
		{`time.unix(1735484645, 250000000):nanosecond()`, 250000000},
		{`time.unix(1735484645, 0):utc():year()`, 2024},
		{`time.unix(1735484645, 0):utc():month()`, 12},
		{`time.unix(1735484645, 0):utc():day()`, 29},
		{`time.unix(1735484645, 0):utc():hour()`, 15},
		{`time.unix(1735484645, 0):utc():minute()`, 4},
		{`time.unix(1735484645, 0):utc():second()`, 5},
		{`time.unix(1735484645, 0):utc():weekday()`, 0},
		{`time.unix(1735484645, 0):utc():year_day()`, 364},
		{`time.parse_duration("2562047h"):nanoseconds() + 1`, int64(2562047*stdtime.Hour) + 1},
		{`time.parse_duration("1500ms"):microseconds()`, 1500000},
		{`time.parse_duration("1500ms"):milliseconds()`, 1500},
	}
	for _, tc := range cases {
		if err := l.DoString("return " + tc.expr); err != nil {
			t.Fatalf("%s: %v", tc.expr, err)
		}
		got, ok := l.Get(-1).(lua.LInteger)
		l.Pop(1)
		if !ok {
			t.Errorf("%s: result is not an integer", tc.expr)
			continue
		}
		if int64(got) != tc.want {
			t.Errorf("%s = %d, want %d", tc.expr, got, tc.want)
		}
	}
}

func TestIntegerGettersMultipleReturns(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	if err := l.DoString(`return time.unix(1735484645, 0):utc():date()`); err != nil {
		t.Fatal(err)
	}
	for i := -3; i < 0; i++ {
		if _, ok := l.Get(i).(lua.LInteger); !ok {
			t.Errorf("date() result %d is not an integer", i+4)
		}
	}
	l.Pop(3)
	if err := l.DoString(`return time.unix(1735484645, 0):utc():clock()`); err != nil {
		t.Fatal(err)
	}
	for i := -3; i < 0; i++ {
		if _, ok := l.Get(i).(lua.LInteger); !ok {
			t.Errorf("clock() result %d is not an integer", i+4)
		}
	}
}

func TestParseDefaultsToUTC(t *testing.T) {
	saved := stdtime.Local
	stdtime.Local = stdtime.FixedZone("TEST", 5*3600+1800)
	defer func() { stdtime.Local = saved }()

	l := lua.NewState()
	defer l.Close()
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local t, err = time.parse("2006-01-02T15:04:05Z", "2024-12-29T15:04:05Z")
		if err then error(tostring(err)) end
		if t:unix() ~= 1735484645 then error("unix mismatch: " .. t:unix()) end
		local c, err = time.parse("2006-01-02 15:04:05", "2024-12-29 15:04:05")
		if err then error(tostring(err)) end
		if c:unix() ~= 1735484645 then error("layout without zone must parse as UTC: " .. c:unix()) end
		local l, err = time.parse("2006-01-02 15:04:05", "2024-12-29 15:04:05", time.fixed_zone("X", 5*3600+1800))
		if err then error(tostring(err)) end
		if l:unix() ~= 1735484645 - (5*3600+1800) then error("explicit location ignored: " .. l:unix()) end
	`)
	if err != nil {
		t.Fatal(err)
	}
}
