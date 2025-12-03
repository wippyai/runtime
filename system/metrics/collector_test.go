package metrics

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	api "github.com/wippyai/runtime/api/metrics"
)

type mockExporter struct {
	mu      sync.Mutex
	records []recordEvent
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
	c := NewCollector(api.Config{Buffer: struct {
		Size int
	}{Size: 1000}})
	defer c.Close()

	mock := &mockExporter{}
	c.RegisterExporter(mock)

	c.CounterInc("test.counter", api.Labels{"a": "b"})
	c.CounterAdd("test.counter", 5, api.Labels{"a": "b"})

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, 2, mock.count())
}

func TestCollector_Gauge(t *testing.T) {
	c := NewCollector(api.Config{Buffer: struct {
		Size int
	}{Size: 1000}})
	defer c.Close()

	mock := &mockExporter{}
	c.RegisterExporter(mock)

	c.GaugeSet("test.gauge", 42, nil)
	c.GaugeInc("test.gauge", nil)
	c.GaugeDec("test.gauge", nil)

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, 3, mock.count())
}

func TestCollector_Histogram(t *testing.T) {
	c := NewCollector(api.Config{Buffer: struct {
		Size int
	}{Size: 1000}})
	defer c.Close()

	mock := &mockExporter{}
	c.RegisterExporter(mock)

	c.HistogramObserve("test.histogram", 0.125, api.Labels{"x": "y"})

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, 1, mock.count())
}

func TestCollector_ExporterFanout(t *testing.T) {
	c := NewCollector(api.Config{Buffer: struct {
		Size int
	}{Size: 1000}})
	defer c.Close()

	mock1, mock2 := &mockExporter{}, &mockExporter{}
	c.RegisterExporter(mock1)
	c.RegisterExporter(mock2)

	c.CounterInc("test", nil)

	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, 1, mock1.count())
	assert.Equal(t, 1, mock2.count())
}

func TestCollector_GracefulShutdown(t *testing.T) {
	c := NewCollector(api.Config{Buffer: struct {
		Size int
	}{Size: 1000}})
	mock := &mockExporter{}
	c.RegisterExporter(mock)

	for i := 0; i < 100; i++ {
		c.CounterInc("test", nil)
	}

	c.Close()
	assert.Equal(t, 100, mock.count())
}

func TestCollector_DefaultBufferSize(t *testing.T) {
	c := NewCollector(api.Config{})
	defer c.Close()

	mock := &mockExporter{}
	c.RegisterExporter(mock)

	c.CounterInc("test", nil)
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, 1, mock.count())
}

func TestCollector_BatchFlush(t *testing.T) {
	c := NewCollector(api.Config{Buffer: struct {
		Size int
	}{Size: 10000}})
	mock := &mockExporter{}
	c.RegisterExporter(mock)

	for i := 0; i < 150; i++ {
		c.CounterInc("test", nil)
	}

	time.Sleep(50 * time.Millisecond)
	assert.GreaterOrEqual(t, mock.count(), 100)

	c.Close()
	assert.Equal(t, 150, mock.count())
}
