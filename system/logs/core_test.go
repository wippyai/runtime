// SPDX-License-Identifier: MPL-2.0

package logs

import (
	"context"
	"testing"

	api "github.com/wippyai/runtime/api/logs"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	"go.uber.org/zap/zapcore"
)

// testDownstreamCore implements zapcore.Core for testing
type testDownstreamCore struct {
	withResponse  zapcore.Core
	writeResponse error
	syncResponse  error
	enabledCalls  []zapcore.Level
	writeCalls    []struct {
		ent    zapcore.Entry
		fields []zapcore.Field
	}
	withCalls       [][]zapcore.Field
	syncCalled      bool
	enabledResponse bool
}

func (t *testDownstreamCore) Enabled(level zapcore.Level) bool {
	t.enabledCalls = append(t.enabledCalls, level)
	return t.enabledResponse
}

func (t *testDownstreamCore) With(fields []zapcore.Field) zapcore.Core {
	t.withCalls = append(t.withCalls, fields)
	return t.withResponse
}

func (t *testDownstreamCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if !t.Enabled(ent.Level) {
		return ce
	}
	return ce.AddCore(ent, t)
}

func (t *testDownstreamCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	t.writeCalls = append(t.writeCalls, struct {
		ent    zapcore.Entry
		fields []zapcore.Field
	}{ent, fields})
	return t.writeResponse
}

func (t *testDownstreamCore) Sync() error {
	t.syncCalled = true
	return t.syncResponse
}

// testEventBus implements event.Bus for testing
type testEventBus struct {
	sendCalls []event.Event
}

func (t *testEventBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}

func (t *testEventBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "test", nil
}

func (t *testEventBus) Unsubscribe(context.Context, event.SubscriberID) {}

func (t *testEventBus) Send(_ context.Context, event event.Event) {
	t.sendCalls = append(t.sendCalls, event)
}

type testMetricsCollector struct {
	counters map[string]float64
}

func (t *testMetricsCollector) CounterInc(name string, labels metrics.Labels) {
	t.CounterAdd(name, 1, labels)
}

func (t *testMetricsCollector) CounterAdd(name string, delta float64, labels metrics.Labels) {
	if t.counters == nil {
		t.counters = make(map[string]float64)
	}
	t.counters[name+"/"+labels["level"]] += delta
}

func (t *testMetricsCollector) GaugeSet(string, float64, metrics.Labels)         {}
func (t *testMetricsCollector) GaugeInc(string, metrics.Labels)                  {}
func (t *testMetricsCollector) GaugeDec(string, metrics.Labels)                  {}
func (t *testMetricsCollector) HistogramObserve(string, float64, metrics.Labels) {}
func (t *testMetricsCollector) RegisterExporter(metrics.Exporter) error          { return nil }
func (t *testMetricsCollector) Close() error                                     { return nil }

func TestNewCore(t *testing.T) {
	downstream := &testDownstreamCore{}
	bus := &testEventBus{}

	core := NewCore(downstream, bus)

	config := core.GetConfig()
	if !config.PropagateDownstream {
		t.Error("expected PropagateDownstream to be true")
	}
	if config.StreamToEvents {
		t.Error("expected StreamToEvents to be false")
	}
}

func TestCore_LogEmissionMetric(t *testing.T) {
	downstream := &testDownstreamCore{}
	bus := &testEventBus{}
	core := NewCore(downstream, bus)
	coll := &testMetricsCollector{}
	core.SetCollector(coll)
	core.Configure(api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.DebugLevel,
	})

	if _, ok := coll.counters["runtime_log_emissions_total/info"]; !ok {
		t.Fatal("expected info log emission counter to be bootstrapped")
	}
	if err := core.Write(zapcore.Entry{Level: zapcore.InfoLevel, Message: "hello"}, nil); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := coll.counters["runtime_log_emissions_total/info"]; got != 1 {
		t.Fatalf("runtime_log_emissions_total/info = %v, want 1", got)
	}
}

func TestCore_Configure(t *testing.T) {
	downstream := &testDownstreamCore{}
	bus := &testEventBus{}
	core := NewCore(downstream, bus)

	newConfig := api.Config{
		PropagateDownstream: false,
		StreamToEvents:      true,
		MinLevel:            zapcore.InfoLevel,
	}

	core.Configure(newConfig)

	config := core.GetConfig()
	if config != newConfig {
		t.Errorf("expected config %v, got %v", newConfig, config)
	}
}

func TestCore_Enabled(t *testing.T) {
	downstream := &testDownstreamCore{}
	bus := &testEventBus{}
	core := NewCore(downstream, bus)

	tests := []struct {
		name     string
		config   api.Config
		level    zapcore.Level
		expected bool
	}{
		{
			name: "below min level",
			config: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      false,
				MinLevel:            zapcore.InfoLevel,
			},
			level:    zapcore.DebugLevel,
			expected: false,
		},
		{
			name: "above min level with propagation",
			config: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      false,
				MinLevel:            zapcore.InfoLevel,
			},
			level:    zapcore.WarnLevel,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core.Configure(tt.config)
			if got := core.Enabled(tt.level); got != tt.expected {
				t.Errorf("Enabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCore_Write(t *testing.T) {
	tests := []struct {
		name             string
		config           api.Config
		expectDownstream bool
		expectEvent      bool
	}{
		{
			name: "propagate downstream only",
			config: api.Config{
				PropagateDownstream: true,
				StreamToEvents:      false,
				MinLevel:            zapcore.InfoLevel,
			},
			expectDownstream: true,
			expectEvent:      false,
		},
		{
			name: "stream to events only",
			config: api.Config{
				PropagateDownstream: false,
				StreamToEvents:      true,
				MinLevel:            zapcore.InfoLevel,
			},
			expectDownstream: false,
			expectEvent:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downstream := &testDownstreamCore{}
			bus := &testEventBus{}
			core := NewCore(downstream, bus)
			core.Configure(tt.config)

			entry := zapcore.Entry{
				Level:      zapcore.InfoLevel,
				Message:    "test message",
				LoggerName: "test.logger",
			}
			fields := []zapcore.Field{
				{
					Key:    "test",
					Type:   zapcore.StringType,
					String: "value",
				},
			}

			err := core.Write(entry, fields)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if tt.expectDownstream {
				if len(downstream.writeCalls) != 1 {
					t.Error("expected one downstream write call")
				}
				if downstream.writeCalls[0].ent != entry {
					t.Error("unexpected entry in downstream write")
				}
			} else if len(downstream.writeCalls) > 0 {
				t.Error("unexpected downstream write call")
			}

			if tt.expectEvent {
				if len(bus.sendCalls) != 1 {
					t.Error("expected one event send call")
				}
				ev := bus.sendCalls[0]
				if ev.System != api.System {
					t.Error("unexpected system in event")
				}
				if ev.Kind != api.Entry {
					t.Error("unexpected kind in event")
				}
				if ev.Path != entry.LoggerName {
					t.Error("unexpected path in event")
				}
			} else if len(bus.sendCalls) > 0 {
				t.Error("unexpected event send call")
			}
		})
	}
}

func TestCore_With(t *testing.T) {
	downstream := &testDownstreamCore{}
	bus := &testEventBus{}
	core := NewCore(downstream, bus)

	fields := []zapcore.Field{
		{
			Key:    "test",
			Type:   zapcore.StringType,
			String: "value",
		},
	}

	newDownstream := &testDownstreamCore{}
	downstream.withResponse = newDownstream

	newCore := core.With(fields)
	if len(downstream.withCalls) != 1 {
		t.Error("expected one With call")
	}
	if downstream.withCalls[0][0] != fields[0] {
		t.Error("unexpected fields in With call")
	}

	coreImpl := newCore.(*Core)
	if coreImpl.downstream != newDownstream {
		t.Error("unexpected downstream core")
	}
	if coreImpl.bus != bus {
		t.Error("unexpected event bus")
	}
}

func TestCore_Sync(t *testing.T) {
	downstream := &testDownstreamCore{}
	bus := &testEventBus{}
	core := NewCore(downstream, bus)

	err := core.Sync()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if !downstream.syncCalled {
		t.Error("expected Sync to be called")
	}
}

func TestCore_Check(t *testing.T) {
	downstream := &testDownstreamCore{}
	bus := &testEventBus{}
	core := NewCore(downstream, bus)

	entry := zapcore.Entry{
		Level:      zapcore.InfoLevel,
		Message:    "test message",
		LoggerName: "test.logger",
	}
	ce := &zapcore.CheckedEntry{}

	core.Configure(api.Config{
		PropagateDownstream: true,
		StreamToEvents:      true,
		MinLevel:            zapcore.InfoLevel,
	})

	result := core.Check(entry, ce)
	if result == nil {
		t.Error("expected non-nil CheckedEntry")
	}

	// Test with disabled level
	core.Configure(api.Config{
		PropagateDownstream: true,
		StreamToEvents:      true,
		MinLevel:            zapcore.WarnLevel,
	})

	result = core.Check(entry, ce)
	if result != ce {
		t.Error("expected original CheckedEntry when disabled")
	}
}
