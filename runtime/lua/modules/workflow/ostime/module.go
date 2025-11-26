package ostime

import (
	"sync"
	"time"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/workflow"
	origostime "github.com/wippyai/runtime/runtime/lua/modules/ostime"
	lua "github.com/yuin/gopher-lua"
)

type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

func NewOSTimeModule() *Module {
	return &Module{}
}

// Info returns module metadata
func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "workflow.os",
		Description: "Workflow-safe OS time functions",
		Class:       []string{luaapi.ClassWorkflow, luaapi.ClassTime},
	}
}

func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

func (m *Module) initModuleTable(l *lua.LState) {
	t := l.CreateTable(0, 3)

	t.RawSetString("time", l.NewFunction(osTime))
	t.RawSetString("date", l.NewFunction(osDate))
	t.RawSetString("clock", l.NewFunction(osClock))

	t.Immutable = true

	m.moduleTable = t
}

func osTime(l *lua.LState) int {
	if l.GetTop() == 0 {
		ref := workflow.GetTimeReference(l.Context())
		if ref == nil {
			l.RaiseError("TimeReference not found in context")
			return 0
		}

		l.Push(lua.LNumber(ref.Now().Unix()))
		return 1
	}

	tbl := l.CheckTable(1)

	ref := workflow.GetTimeReference(l.Context())
	var now time.Time
	if ref != nil {
		now = ref.Now()
	} else {
		now = time.Now()
	}

	year := origostime.GetIntField(tbl, "year", now.Year())
	month := origostime.GetIntField(tbl, "month", int(now.Month()))
	day := origostime.GetIntField(tbl, "day", now.Day())
	hour := origostime.GetIntField(tbl, "hour", 0)
	mn := origostime.GetIntField(tbl, "min", 0)
	sec := origostime.GetIntField(tbl, "sec", 0)

	t := time.Date(year, time.Month(month), day, hour, mn, sec, 0, time.Local)
	l.Push(lua.LNumber(t.Unix()))
	return 1
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
		ref := workflow.GetTimeReference(l.Context())
		if ref == nil {
			l.RaiseError("TimeReference not found in context")
			return 0
		}
		t = ref.Now()
	}

	utc := false
	if len(format) > 0 && format[0] == '!' {
		utc = true
		format = format[1:]
	}

	if utc {
		t = t.UTC()
	}

	if format == "*t" {
		return origostime.OsDateTable(l, t)
	}

	result := origostime.FormatDate(format, t)
	l.Push(lua.LString(result))
	return 1
}

func osClock(l *lua.LState) int {
	ref := workflow.GetTimeReference(l.Context())
	if ref == nil {
		l.RaiseError("TimeReference not found in context")
		return 0
	}

	startTime := ref.StartTime()
	now := ref.Now()
	elapsed := now.Sub(startTime).Seconds()

	l.Push(lua.LNumber(elapsed))
	return 1
}
