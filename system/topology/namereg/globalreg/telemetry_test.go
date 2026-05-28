// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"testing"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/internal/telemetrytest"
)

func newTestTelemetry(t *testing.T) (*telemetry, *telemetrytest.Recorder) {
	t.Helper()
	rec := telemetrytest.NewRecorder()
	tt := newTelemetry(rec, nil, nil, "_test")
	if tt == nil {
		t.Fatalf("newTelemetry returned nil")
	}

	return tt, rec
}

func TestTelemetry_NilCollector_NoPanic(t *testing.T) {
	tt := newTelemetry(nil, nil, nil, "_test")
	tt.recordFenceToken("g1", "node-a", 1)
	tt.recordGlobalregSize(0)
	tt.recordGlobalregDedupe()
}

func TestTelemetry_RecordFence(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordFenceToken("g1", "node-a", 7)
	if v := rec.GaugeValue("pg_fence_token", metrics.Labels{"pg": "g1", "node": "node-a"}); v != 7 {
		t.Fatalf("pg_fence_token: want 7, got %v", v)
	}
}

func TestTelemetry_RecordGlobalreg(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordGlobalregSize(42)
	if v := rec.GaugeValue("pg_globalreg_size", nil); v != 42 {
		t.Fatalf("pg_globalreg_size: want 42, got %v", v)
	}

	tt.recordGlobalregDedupe()
	if v := rec.CounterValue("pg_globalreg_dedupe_total", nil); v != 1 {
		t.Fatalf("pg_globalreg_dedupe_total: want 1, got %v", v)
	}
}
