package metrics

import (
	"sort"
	"sync"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	api "github.com/wippyai/runtime/api/metrics"
	lua "github.com/yuin/gopher-lua"
)

// mockCollector implements api.Collector for testing
type mockCollector struct {
	mu        sync.Mutex
	counters  map[string]float64
	gauges    map[string]float64
	histogram []histogramEntry
}

type histogramEntry struct {
	name   string
	value  float64
	labels api.Labels
}

func newMockCollector() *mockCollector {
	return &mockCollector{
		counters:  make(map[string]float64),
		gauges:    make(map[string]float64),
		histogram: make([]histogramEntry, 0),
	}
}

func (m *mockCollector) CounterInc(name string, labels api.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := name + labelsKey(labels)
	m.counters[key]++
}

func (m *mockCollector) CounterAdd(name string, delta float64, labels api.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := name + labelsKey(labels)
	m.counters[key] += delta
}

func (m *mockCollector) GaugeSet(name string, value float64, labels api.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := name + labelsKey(labels)
	m.gauges[key] = value
}

func (m *mockCollector) GaugeInc(name string, labels api.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := name + labelsKey(labels)
	m.gauges[key]++
}

func (m *mockCollector) GaugeDec(name string, labels api.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := name + labelsKey(labels)
	m.gauges[key]--
}

func (m *mockCollector) HistogramObserve(name string, value float64, labels api.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.histogram = append(m.histogram, histogramEntry{name: name, value: value, labels: labels})
}

func (m *mockCollector) RegisterExporter(api.Exporter) error { return nil }
func (m *mockCollector) Close() error                        { return nil }

func labelsKey(labels api.Labels) string {
	if labels == nil {
		return ""
	}
	// Sort keys for consistent ordering
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	key := ""
	for _, k := range keys {
		key += ":" + k + "=" + labels[k]
	}
	return key
}

func (m *mockCollector) getCounter(name string, labels api.Labels) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[name+labelsKey(labels)]
}

func (m *mockCollector) getGauge(name string, labels api.Labels) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gauges[name+labelsKey(labels)]
}

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Module.Load(l)

	mod := l.GetGlobal("metrics")
	if mod.Type() != lua.LTTable {
		t.Fatal("module not registered")
	}

	tbl := mod.(*lua.LTable)

	funcs := []string{"counter_inc", "counter_add", "gauge_set", "gauge_inc", "gauge_dec", "histogram"}
	for _, fn := range funcs {
		if tbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("function %s not registered", fn)
		}
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	Module.Load(l1)
	Module.Load(l2)

	mod1 := l1.GetGlobal("metrics").(*lua.LTable)
	mod2 := l2.GetGlobal("metrics").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestCounterInc(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	collector := newMockCollector()
	ctx := ctxapi.NewRootContext()
	api.WithCollector(ctx, collector)
	l.SetContext(ctx)

	Module.Load(l)

	err := l.DoString(`
		local ok, err = metrics.counter_inc("test.counter")
		if not ok then error("counter_inc failed: " .. tostring(err)) end
	`)
	if err != nil {
		t.Fatal(err)
	}

	if collector.getCounter("test.counter", nil) != 1 {
		t.Error("counter not incremented")
	}
}

func TestCounterIncWithLabels(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	collector := newMockCollector()
	ctx := ctxapi.NewRootContext()
	api.WithCollector(ctx, collector)
	l.SetContext(ctx)

	Module.Load(l)

	err := l.DoString(`
		local ok, err = metrics.counter_inc("test.counter", {method = "GET", path = "/api"})
		if not ok then error("counter_inc failed: " .. tostring(err)) end
	`)
	if err != nil {
		t.Fatal(err)
	}

	labels := api.Labels{"method": "GET", "path": "/api"}
	if collector.getCounter("test.counter", labels) != 1 {
		t.Error("counter with labels not incremented")
	}
}

func TestCounterAdd(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	collector := newMockCollector()
	ctx := ctxapi.NewRootContext()
	api.WithCollector(ctx, collector)
	l.SetContext(ctx)

	Module.Load(l)

	err := l.DoString(`
		local ok, err = metrics.counter_add("test.counter", 5)
		if not ok then error("counter_add failed: " .. tostring(err)) end
	`)
	if err != nil {
		t.Fatal(err)
	}

	if collector.getCounter("test.counter", nil) != 5 {
		t.Error("counter not added correctly")
	}
}

func TestGaugeSet(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	collector := newMockCollector()
	ctx := ctxapi.NewRootContext()
	api.WithCollector(ctx, collector)
	l.SetContext(ctx)

	Module.Load(l)

	err := l.DoString(`
		local ok, err = metrics.gauge_set("test.gauge", 42)
		if not ok then error("gauge_set failed: " .. tostring(err)) end
	`)
	if err != nil {
		t.Fatal(err)
	}

	if collector.getGauge("test.gauge", nil) != 42 {
		t.Error("gauge not set correctly")
	}
}

func TestGaugeIncDec(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	collector := newMockCollector()
	ctx := ctxapi.NewRootContext()
	api.WithCollector(ctx, collector)
	l.SetContext(ctx)

	Module.Load(l)

	err := l.DoString(`
		metrics.gauge_inc("test.gauge")
		metrics.gauge_inc("test.gauge")
		metrics.gauge_dec("test.gauge")
	`)
	if err != nil {
		t.Fatal(err)
	}

	if collector.getGauge("test.gauge", nil) != 1 {
		t.Errorf("gauge value incorrect, got %f", collector.getGauge("test.gauge", nil))
	}
}

func TestHistogram(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	collector := newMockCollector()
	ctx := ctxapi.NewRootContext()
	api.WithCollector(ctx, collector)
	l.SetContext(ctx)

	Module.Load(l)

	err := l.DoString(`
		local ok, err = metrics.histogram("test.histogram", 0.123)
		if not ok then error("histogram failed: " .. tostring(err)) end
	`)
	if err != nil {
		t.Fatal(err)
	}

	if len(collector.histogram) != 1 {
		t.Fatal("histogram observation not recorded")
	}
	if collector.histogram[0].value != 0.123 {
		t.Error("histogram value incorrect")
	}
}

func TestNoCollectorError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)

	Module.Load(l)

	err := l.DoString(`
		local ok, err = metrics.counter_inc("test.counter")
		if ok ~= nil then error("expected nil result") end
		if err == nil then error("expected error") end
		if err:kind() ~= errors.INTERNAL then
			error("expected Internal kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}
