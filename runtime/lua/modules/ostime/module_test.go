package ostime

import (
	"fmt"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Module.Load(l)

	mod := l.GetGlobal("os")
	if mod.Type() != lua.LTTable {
		t.Fatal("os module not registered")
	}

	tbl := mod.(*lua.LTable)
	funcs := []string{"time", "date", "clock", "difftime"}
	for _, fn := range funcs {
		if tbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}

	platform := tbl.RawGetString("platform")
	if platform.Type() != lua.LTString || platform.String() != "wippy" {
		t.Errorf("os.platform = %v, expected 'wippy'", platform)
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	Module.Load(l1)
	Module.Load(l2)

	mod1 := l1.GetGlobal("os").(*lua.LTable)
	mod2 := l2.GetGlobal("os").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestOsTimeNoArgs(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	before := time.Now().Unix()

	err := l.DoString(`result = os.time()`)
	if err != nil {
		t.Fatalf("os.time() failed: %v", err)
	}

	after := time.Now().Unix()

	result := int64(l.GetGlobal("result").(lua.LNumber))
	if result < before || result > after {
		t.Errorf("os.time() returned %d, expected between %d and %d", result, before, after)
	}
}

func TestOsTimeWithTable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		result = os.time({year=2024, month=6, day=15, hour=12, min=30, sec=45})
	`)
	if err != nil {
		t.Fatalf("os.time(table) failed: %v", err)
	}

	result := int64(l.GetGlobal("result").(lua.LNumber))
	expected := time.Date(2024, 6, 15, 12, 30, 45, 0, time.Local).Unix()
	if result != expected {
		t.Errorf("os.time(table) returned %d, expected %d", result, expected)
	}
}

func TestOsTimePartialTable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		result = os.time({year=2024, month=1, day=1})
	`)
	if err != nil {
		t.Fatalf("os.time(partial table) failed: %v", err)
	}

	result := int64(l.GetGlobal("result").(lua.LNumber))
	expected := time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local).Unix()
	if result != expected {
		t.Errorf("os.time(partial table) returned %d, expected %d", result, expected)
	}
}

func TestOsClock(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		clock1 = os.clock()
	`)
	if err != nil {
		t.Fatalf("os.clock() failed: %v", err)
	}

	clock1 := float64(l.GetGlobal("clock1").(lua.LNumber))
	if clock1 < 0 {
		t.Errorf("os.clock() returned negative: %f", clock1)
	}

	time.Sleep(10 * time.Millisecond)

	err = l.DoString(`clock2 = os.clock()`)
	if err != nil {
		t.Fatalf("second os.clock() failed: %v", err)
	}

	clock2 := float64(l.GetGlobal("clock2").(lua.LNumber))
	if clock2 <= clock1 {
		t.Errorf("os.clock() should increase over time: %f <= %f", clock2, clock1)
	}
}

func TestOsDateDefault(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`result = os.date()`)
	if err != nil {
		t.Fatalf("os.date() failed: %v", err)
	}

	result := l.GetGlobal("result").String()
	if len(result) == 0 {
		t.Error("os.date() returned empty string")
	}
}

func TestOsDateFormats(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	timestamp := time.Date(2024, 6, 15, 14, 30, 45, 0, time.Local).Unix()

	tests := []struct {
		format   string
		expected string
	}{
		{"%Y", "2024"},
		{"%m", "06"},
		{"%d", "15"},
		{"%H", "14"},
		{"%M", "30"},
		{"%S", "45"},
		{"%y", "24"},
		{"%a", "Sat"},
		{"%A", "Saturday"},
		{"%b", "Jun"},
		{"%B", "June"},
		{"%%", "%"},
	}

	for _, tc := range tests {
		l2 := lua.NewState()
		Module.Load(l2)

		err := l2.DoString(`result = os.date("` + tc.format + `", ` + itoa(timestamp) + `)`)
		if err != nil {
			t.Errorf("os.date(%q) failed: %v", tc.format, err)
			l2.Close()
			continue
		}

		result := l2.GetGlobal("result").String()
		if result != tc.expected {
			t.Errorf("os.date(%q) = %q, expected %q", tc.format, result, tc.expected)
		}
		l2.Close()
	}
}

func TestOsDateTable(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	timestamp := time.Date(2024, 6, 15, 14, 30, 45, 0, time.Local).Unix()

	err := l.DoString(`result = os.date("*t", ` + itoa(timestamp) + `)`)
	if err != nil {
		t.Fatalf("os.date('*t') failed: %v", err)
	}

	tbl := l.GetGlobal("result").(*lua.LTable)

	tests := []struct {
		key      string
		expected int
	}{
		{"year", 2024},
		{"month", 6},
		{"day", 15},
		{"hour", 14},
		{"min", 30},
		{"sec", 45},
		{"wday", 7}, // Saturday = 7 in Lua (1-indexed)
	}

	for _, tc := range tests {
		val := tbl.RawGetString(tc.key)
		if val.Type() != lua.LTNumber {
			t.Errorf("os.date('*t').%s not a number", tc.key)
			continue
		}
		if int(val.(lua.LNumber)) != tc.expected {
			t.Errorf("os.date('*t').%s = %v, expected %d", tc.key, val, tc.expected)
		}
	}
}

func TestOsDateUTC(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	timestamp := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC).Unix()

	err := l.DoString(`result = os.date("!*t", ` + itoa(timestamp) + `)`)
	if err != nil {
		t.Fatalf("os.date('!*t') failed: %v", err)
	}

	tbl := l.GetGlobal("result").(*lua.LTable)

	hour := tbl.RawGetString("hour")
	if int(hour.(lua.LNumber)) != 0 {
		t.Errorf("UTC date hour = %v, expected 0", hour)
	}
}

func TestOsDateYearDay(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	timestamp := time.Date(2024, 1, 15, 0, 0, 0, 0, time.Local).Unix()

	err := l.DoString(`result = os.date("%j", ` + itoa(timestamp) + `)`)
	if err != nil {
		t.Fatalf("os.date('%%j') failed: %v", err)
	}

	result := l.GetGlobal("result").String()
	if result != "015" {
		t.Errorf("os.date('%%j') = %q, expected '015'", result)
	}
}

func TestOsDateWeekday(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	timestamp := time.Date(2024, 6, 15, 0, 0, 0, 0, time.Local).Unix()

	err := l.DoString(`result = os.date("%w", ` + itoa(timestamp) + `)`)
	if err != nil {
		t.Fatalf("os.date('%%w') failed: %v", err)
	}

	result := l.GetGlobal("result").String()
	if result != "6" {
		t.Errorf("os.date('%%w') = %q, expected '6' (Saturday)", result)
	}
}

func TestOsDateAMPM(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	timestamp := time.Date(2024, 6, 15, 14, 0, 0, 0, time.Local).Unix()

	err := l.DoString(`result = os.date("%p", ` + itoa(timestamp) + `)`)
	if err != nil {
		t.Fatalf("os.date('%%p') failed: %v", err)
	}

	result := l.GetGlobal("result").String()
	if result != "PM" {
		t.Errorf("os.date('%%p') = %q, expected 'PM'", result)
	}
}

func TestOsDate12Hour(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	timestamp := time.Date(2024, 6, 15, 14, 0, 0, 0, time.Local).Unix()

	err := l.DoString(`result = os.date("%I", ` + itoa(timestamp) + `)`)
	if err != nil {
		t.Fatalf("os.date('%%I') failed: %v", err)
	}

	result := l.GetGlobal("result").String()
	if result != "02" {
		t.Errorf("os.date('%%I') = %q, expected '02'", result)
	}
}

func TestOsDateTimezone(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`result = os.date("%Z")`)
	if err != nil {
		t.Fatalf("os.date('%%Z') failed: %v", err)
	}

	result := l.GetGlobal("result").String()
	if len(result) == 0 {
		t.Error("os.date('%Z') returned empty string")
	}
}

func TestOsDateUnknownSpecifier(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`result = os.date("%Q")`)
	if err != nil {
		t.Fatalf("os.date('%%Q') failed: %v", err)
	}

	result := l.GetGlobal("result").String()
	if result != "%Q" {
		t.Errorf("os.date('%%Q') = %q, expected '%%Q'", result)
	}
}

func TestOsDateMixedFormat(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	timestamp := time.Date(2024, 6, 15, 14, 30, 45, 0, time.Local).Unix()

	err := l.DoString(`result = os.date("%Y-%m-%d %H:%M:%S", ` + itoa(timestamp) + `)`)
	if err != nil {
		t.Fatalf("mixed format failed: %v", err)
	}

	result := l.GetGlobal("result").String()
	expected := "2024-06-15 14:30:45"
	if result != expected {
		t.Errorf("mixed format = %q, expected %q", result, expected)
	}
}

func TestOsDifftime(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		t1 = os.time({year=2024, month=1, day=1, hour=0, min=0, sec=0})
		t2 = os.time({year=2024, month=1, day=1, hour=0, min=0, sec=30})
		result = os.difftime(t2, t1)
	`)
	if err != nil {
		t.Fatalf("os.difftime failed: %v", err)
	}

	result := float64(l.GetGlobal("result").(lua.LNumber))
	if result != 30 {
		t.Errorf("os.difftime = %v, expected 30", result)
	}
}

func TestOsDifftimeNegative(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		t1 = os.time({year=2024, month=1, day=1, hour=0, min=0, sec=30})
		t2 = os.time({year=2024, month=1, day=1, hour=0, min=0, sec=0})
		result = os.difftime(t2, t1)
	`)
	if err != nil {
		t.Fatalf("os.difftime negative failed: %v", err)
	}

	result := float64(l.GetGlobal("result").(lua.LNumber))
	if result != -30 {
		t.Errorf("os.difftime negative = %v, expected -30", result)
	}
}

func TestOsPlatform(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`result = os.platform`)
	if err != nil {
		t.Fatalf("os.platform failed: %v", err)
	}

	result := l.GetGlobal("result").String()
	if result != "wippy" {
		t.Errorf("os.platform = %q, expected 'wippy'", result)
	}
}

func TestImmutability(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Module.Load(l)

	err := l.DoString(`
		local success = pcall(function()
			os.foo = "bar"
		end)
	`)
	if err != nil {
		t.Errorf("immutability test failed: %v", err)
	}
}

func itoa(n int64) string {
	return fmt.Sprintf("%d", n)
}
