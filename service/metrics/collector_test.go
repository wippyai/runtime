package metrics

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	api "github.com/wippyai/runtime/api/metrics"
	apicfg "github.com/wippyai/runtime/api/service/metrics"
)

type mockExporter struct {
	records []recordEvent
	mu      sync.Mutex
}

func (m *mockExporter) Name() string { return "mock" }

func (m *mockExporter) Record(name string, typ api.MetricType, value float64, labels api.Labels) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, recordEvent{name: name, typ: typ, value: value, labels: labels})
	return nil
}

func (m *mockExporter) Close() error { return nil }

func (m *mockExporter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}

func TestCollector_Counter(t *testing.T) {
	c := NewCollector(apicfg.Config{Buffer: struct {
		Size int `json:"size"`
	}{Size: 1000}})
	defer func() { _ = c.Close() }()

	mock := &mockExporter{}
	_ = c.RegisterExporter(mock)

	c.CounterInc("test.counter", api.Labels{"a": "b"})
	c.CounterAdd("test.counter", 5, api.Labels{"a": "b"})

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, 2, mock.count())
}

func TestCollector_Gauge(t *testing.T) {
	c := NewCollector(apicfg.Config{Buffer: struct {
		Size int `json:"size"`
	}{Size: 1000}})
	defer func() { _ = c.Close() }()

	mock := &mockExporter{}
	_ = c.RegisterExporter(mock)

	c.GaugeSet("test.gauge", 42, nil)
	c.GaugeInc("test.gauge", nil)
	c.GaugeDec("test.gauge", nil)

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, 3, mock.count())
}

func TestCollector_Histogram(t *testing.T) {
	c := NewCollector(apicfg.Config{Buffer: struct {
		Size int `json:"size"`
	}{Size: 1000}})
	defer func() { _ = c.Close() }()

	mock := &mockExporter{}
	_ = c.RegisterExporter(mock)

	c.HistogramObserve("test.histogram", 0.125, api.Labels{"x": "y"})

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, 1, mock.count())
}

func TestCollector_ExporterFanout(t *testing.T) {
	c := NewCollector(apicfg.Config{Buffer: struct {
		Size int `json:"size"`
	}{Size: 1000}})
	defer func() { _ = c.Close() }()

	mock1, mock2 := &mockExporter{}, &mockExporter{}
	_ = c.RegisterExporter(mock1)
	_ = c.RegisterExporter(mock2)

	c.CounterInc("test", nil)

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, 1, mock1.count())
	assert.Equal(t, 1, mock2.count())
}

func TestCollector_GracefulShutdown(t *testing.T) {
	c := NewCollector(apicfg.Config{Buffer: struct {
		Size int `json:"size"`
	}{Size: 1000}})
	mock := &mockExporter{}
	_ = c.RegisterExporter(mock)

	for i := 0; i < 100; i++ {
		c.CounterInc("test", nil)
	}

	_ = c.Close()
	assert.Equal(t, 100, mock.count())
}

func TestCollector_DefaultBufferSize(t *testing.T) {
	c := NewCollector(apicfg.Config{})
	defer func() { _ = c.Close() }()

	mock := &mockExporter{}
	_ = c.RegisterExporter(mock)

	c.CounterInc("test", nil)
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, 1, mock.count())
}

func TestCollector_BatchFlush(t *testing.T) {
	c := NewCollector(apicfg.Config{Buffer: struct {
		Size int `json:"size"`
	}{Size: 10000}})
	mock := &mockExporter{}
	_ = c.RegisterExporter(mock)

	for i := 0; i < 150; i++ {
		c.CounterInc("test", nil)
	}

	time.Sleep(50 * time.Millisecond)
	assert.GreaterOrEqual(t, mock.count(), 100)

	_ = c.Close()
	assert.Equal(t, 150, mock.count())
}

type nopExporter struct{}

func (n *nopExporter) Name() string                                             { return "nop" }
func (n *nopExporter) Record(string, api.MetricType, float64, api.Labels) error { return nil }
func (n *nopExporter) Close() error                                             { return nil }

func BenchmarkCollector_CounterInc(b *testing.B) {
	c := NewCollector(apicfg.Config{Buffer: struct {
		Size int `json:"size"`
	}{Size: 100000}})
	_ = c.RegisterExporter(&nopExporter{})
	defer func() { _ = c.Close() }()

	labels := api.Labels{"method": "GET", "status": "200"}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c.CounterInc("http_requests_total", labels)
	}
}

func BenchmarkCollector_GaugeSet(b *testing.B) {
	c := NewCollector(apicfg.Config{Buffer: struct {
		Size int `json:"size"`
	}{Size: 100000}})
	_ = c.RegisterExporter(&nopExporter{})
	defer func() { _ = c.Close() }()

	labels := api.Labels{"pool": "workers"}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c.GaugeSet("active_connections", float64(i%100), labels)
	}
}

func BenchmarkCollector_HistogramObserve(b *testing.B) {
	c := NewCollector(apicfg.Config{Buffer: struct {
		Size int `json:"size"`
	}{Size: 100000}})
	_ = c.RegisterExporter(&nopExporter{})
	defer func() { _ = c.Close() }()

	labels := api.Labels{"endpoint": "/api/v1/users"}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c.HistogramObserve("request_duration", 0.125, labels)
	}
}

func BenchmarkCollector_Parallel(b *testing.B) {
	c := NewCollector(apicfg.Config{Buffer: struct {
		Size int `json:"size"`
	}{Size: 100000}})
	_ = c.RegisterExporter(&nopExporter{})
	defer func() { _ = c.Close() }()

	labels := api.Labels{"method": "GET"}
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.CounterInc("parallel_counter", labels)
		}
	})
}

func BenchmarkCollector_NoLabels(b *testing.B) {
	c := NewCollector(apicfg.Config{Buffer: struct {
		Size int `json:"size"`
	}{Size: 100000}})
	_ = c.RegisterExporter(&nopExporter{})
	defer func() { _ = c.Close() }()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c.CounterInc("simple_counter", nil)
	}
}
