package metrics

import (
	"sync"

	api "github.com/wippyai/runtime/api/metrics"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

type Module struct {
	once        sync.Once
	moduleTable *lua.LTable
}

func NewMetricsModule() *Module {
	return &Module{}
}

func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "metrics",
		Description: "Metrics collection and export",
		Class:       []string{luaapi.ClassNondeterministic},
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
	mod := l.CreateTable(0, 6)
	mod.RawSetString("counter_inc", l.NewFunction(m.counterInc))
	mod.RawSetString("counter_add", l.NewFunction(m.counterAdd))
	mod.RawSetString("gauge_set", l.NewFunction(m.gaugeSet))
	mod.RawSetString("gauge_inc", l.NewFunction(m.gaugeInc))
	mod.RawSetString("gauge_dec", l.NewFunction(m.gaugeDec))
	mod.RawSetString("histogram", l.NewFunction(m.histogram))
	mod.Immutable = true
	m.moduleTable = mod
}

func (m *Module) counterInc(l *lua.LState) int {
	collector := api.GetCollector(l.Context())
	if collector == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("metrics collector not available"))
		return 2
	}

	name := l.CheckString(1)
	labels := m.parseLabels(l, 2)

	collector.CounterInc(name, labels)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) counterAdd(l *lua.LState) int {
	collector := api.GetCollector(l.Context())
	if collector == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("metrics collector not available"))
		return 2
	}

	name := l.CheckString(1)
	value := l.CheckNumber(2)
	labels := m.parseLabels(l, 3)

	collector.CounterAdd(name, float64(value), labels)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) gaugeSet(l *lua.LState) int {
	collector := api.GetCollector(l.Context())
	if collector == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("metrics collector not available"))
		return 2
	}

	name := l.CheckString(1)
	value := l.CheckNumber(2)
	labels := m.parseLabels(l, 3)

	collector.GaugeSet(name, float64(value), labels)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) gaugeInc(l *lua.LState) int {
	collector := api.GetCollector(l.Context())
	if collector == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("metrics collector not available"))
		return 2
	}

	name := l.CheckString(1)
	labels := m.parseLabels(l, 2)

	collector.GaugeInc(name, labels)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) gaugeDec(l *lua.LState) int {
	collector := api.GetCollector(l.Context())
	if collector == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("metrics collector not available"))
		return 2
	}

	name := l.CheckString(1)
	labels := m.parseLabels(l, 2)

	collector.GaugeDec(name, labels)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) histogram(l *lua.LState) int {
	collector := api.GetCollector(l.Context())
	if collector == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("metrics collector not available"))
		return 2
	}

	name := l.CheckString(1)
	value := l.CheckNumber(2)
	labels := m.parseLabels(l, 3)

	collector.HistogramObserve(name, float64(value), labels)

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) parseLabels(l *lua.LState, argIndex int) api.Labels {
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
