package metrics

import (
	"sync"

	api "github.com/wippyai/runtime/api/metrics"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable  *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// Module is the singleton metrics module instance.
var Module = &metricsModule{}

type metricsModule struct{}

func (m *metricsModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "metrics",
		Description: "Counters, gauges, and histograms",
		Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	}
}

func (m *metricsModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()
		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})
	return registration
}

func (m *metricsModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 6)
	mod.RawSetString("counter_inc", lua.LGoFunc(counterInc))
	mod.RawSetString("counter_add", lua.LGoFunc(counterAdd))
	mod.RawSetString("gauge_set", lua.LGoFunc(gaugeSet))
	mod.RawSetString("gauge_inc", lua.LGoFunc(gaugeInc))
	mod.RawSetString("gauge_dec", lua.LGoFunc(gaugeDec))
	mod.RawSetString("histogram", lua.LGoFunc(histogram))
	mod.Immutable = true
	return mod
}

func counterInc(l *lua.LState) int {
	collector := api.GetCollector(l.Context())
	if collector == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("metrics collector not available"))
		return 2
	}

	name := l.CheckString(1)
	labels := parseLabels(l, 2)

	collector.CounterInc(name, labels)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func counterAdd(l *lua.LState) int {
	collector := api.GetCollector(l.Context())
	if collector == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("metrics collector not available"))
		return 2
	}

	name := l.CheckString(1)
	value := l.CheckNumber(2)
	labels := parseLabels(l, 3)

	collector.CounterAdd(name, float64(value), labels)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func gaugeSet(l *lua.LState) int {
	collector := api.GetCollector(l.Context())
	if collector == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("metrics collector not available"))
		return 2
	}

	name := l.CheckString(1)
	value := l.CheckNumber(2)
	labels := parseLabels(l, 3)

	collector.GaugeSet(name, float64(value), labels)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func gaugeInc(l *lua.LState) int {
	collector := api.GetCollector(l.Context())
	if collector == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("metrics collector not available"))
		return 2
	}

	name := l.CheckString(1)
	labels := parseLabels(l, 2)

	collector.GaugeInc(name, labels)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func gaugeDec(l *lua.LState) int {
	collector := api.GetCollector(l.Context())
	if collector == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("metrics collector not available"))
		return 2
	}

	name := l.CheckString(1)
	labels := parseLabels(l, 2)

	collector.GaugeDec(name, labels)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func histogram(l *lua.LState) int {
	collector := api.GetCollector(l.Context())
	if collector == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("metrics collector not available"))
		return 2
	}

	name := l.CheckString(1)
	value := l.CheckNumber(2)
	labels := parseLabels(l, 3)

	collector.HistogramObserve(name, float64(value), labels)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func parseLabels(l *lua.LState, argIndex int) api.Labels {
	if l.GetTop() < argIndex {
		return nil
	}

	lv := l.Get(argIndex)
	if lv.Type() != lua.LTTable {
		return nil
	}

	labels := make(api.Labels)
	l.ForEach(lv.(*lua.LTable), func(key, value lua.LValue) {
		if keyStr, ok := key.(lua.LString); ok {
			if valStr, ok := value.(lua.LString); ok {
				labels[string(keyStr)] = string(valStr)
			}
		}
	})

	return labels
}
