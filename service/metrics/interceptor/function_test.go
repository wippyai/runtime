package interceptor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
)

type mockCollector struct {
	records []mockRecord
	mu      sync.Mutex
}

type mockRecord struct {
	labels metrics.Labels
	method string
	name   string
	value  float64
}

func (m *mockCollector) CounterInc(name string, labels metrics.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, mockRecord{method: "CounterInc", name: name, labels: labels})
}

func (m *mockCollector) CounterAdd(name string, delta float64, labels metrics.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, mockRecord{method: "CounterAdd", name: name, value: delta, labels: labels})
}

func (m *mockCollector) GaugeSet(name string, value float64, labels metrics.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, mockRecord{method: "GaugeSet", name: name, value: value, labels: labels})
}

func (m *mockCollector) GaugeInc(name string, labels metrics.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, mockRecord{method: "GaugeInc", name: name, labels: labels})
}

func (m *mockCollector) GaugeDec(name string, labels metrics.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, mockRecord{method: "GaugeDec", name: name, labels: labels})
}

func (m *mockCollector) HistogramObserve(name string, value float64, labels metrics.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, mockRecord{method: "HistogramObserve", name: name, value: value, labels: labels})
}

func (m *mockCollector) RegisterExporter(metrics.Exporter) error { return nil }
func (m *mockCollector) Close() error                            { return nil }

func (m *mockCollector) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}

func (m *mockCollector) getRecords() []mockRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mockRecord{}, m.records...)
}

func TestFunctionInterceptor_Success(t *testing.T) {
	mock := &mockCollector{}
	interceptor := NewFunctionInterceptor(mock, true)

	task := runtime.Task{ID: registry.NewID("ns", "test_func")}
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		time.Sleep(10 * time.Millisecond)
		return &runtime.Result{}, nil
	}

	result, err := interceptor.Handle(context.Background(), task, next)

	assert.NoError(t, err)
	assert.NotNil(t, result)

	records := mock.getRecords()
	assert.Equal(t, 4, len(records))

	assert.Equal(t, "GaugeInc", records[0].method)
	assert.Equal(t, FunctionInFlight, records[0].name)

	assert.Equal(t, "GaugeDec", records[1].method)
	assert.Equal(t, FunctionInFlight, records[1].name)

	assert.Equal(t, "HistogramObserve", records[2].method)
	assert.Equal(t, FunctionDuration, records[2].name)
	assert.Greater(t, records[2].value, 0.0)

	assert.Equal(t, "CounterInc", records[3].method)
	assert.Equal(t, FunctionCalls, records[3].name)
	assert.Equal(t, "success", records[3].labels["status"])
}

func TestFunctionInterceptor_Error(t *testing.T) {
	mock := &mockCollector{}
	interceptor := NewFunctionInterceptor(mock, true)

	task := runtime.Task{ID: registry.NewID("ns", "test_func")}
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return nil, errors.New("test error")
	}

	result, err := interceptor.Handle(context.Background(), task, next)

	assert.Error(t, err)
	assert.Nil(t, result)

	records := mock.getRecords()
	assert.Equal(t, 4, len(records))
	assert.Equal(t, "error", records[3].labels["status"])
}

func TestFunctionInterceptor_Disabled(t *testing.T) {
	mock := &mockCollector{}
	interceptor := NewFunctionInterceptor(mock, false)

	task := runtime.Task{ID: registry.NewID("ns", "test_func")}
	called := false
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		called = true
		return &runtime.Result{}, nil
	}

	_, _ = interceptor.Handle(context.Background(), task, next)

	assert.True(t, called)
	assert.Equal(t, 0, mock.count())
}

func TestFunctionInterceptor_NilCollector(t *testing.T) {
	interceptor := NewFunctionInterceptor(nil, true)

	task := runtime.Task{ID: registry.NewID("ns", "test_func")}
	called := false
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		called = true
		return &runtime.Result{}, nil
	}

	_, _ = interceptor.Handle(context.Background(), task, next)
	assert.True(t, called)
}
