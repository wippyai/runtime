// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"testing"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/internal/telemetrytest"
)

func newTestTelemetry(t *testing.T) (*telemetry, *telemetrytest.Recorder) {
	t.Helper()
	rec := telemetrytest.NewRecorder()
	tt := newTelemetry(rec, nil, nil)
	if tt == nil {
		t.Fatalf("newTelemetry returned nil")
	}
	return tt, rec
}

func TestTelemetry_RecordJoin_OK(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordJoin("g1", nil, 12*time.Millisecond)

	if v := rec.CounterValue("pg_join_total", metrics.Labels{"pg": "g1", "result": "ok"}); v != 1 {
		t.Fatalf("pg_join_total{pg=g1,result=ok}: want 1, got %v", v)
	}
	if c := rec.HistogramCount("pg_op_duration_seconds", metrics.Labels{"pg": "g1", "op": "join"}); c != 1 {
		t.Fatalf("pg_op_duration_seconds count: want 1, got %v", c)
	}
}

func TestTelemetry_RecordJoin_Error(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordJoin("g1", errSomething, 0)
	if v := rec.CounterValue("pg_join_total", metrics.Labels{"pg": "g1", "result": "err"}); v != 1 {
		t.Fatalf("pg_join_total{result=err}: want 1, got %v", v)
	}
}

func TestTelemetry_RecordLeave(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordLeave("g1", nil, 5*time.Millisecond)
	if v := rec.CounterValue("pg_leave_total", metrics.Labels{"pg": "g1", "result": "ok"}); v != 1 {
		t.Fatalf("pg_leave_total: want 1, got %v", v)
	}
}

func TestTelemetry_RecordBroadcast(t *testing.T) {
	tt, rec := newTestTelemetry(t)
	tt.recordBroadcast("g1", 7, nil, 20*time.Millisecond)
	if v := rec.CounterValue("pg_broadcast_total", metrics.Labels{"pg": "g1", "result": "ok"}); v != 1 {
		t.Fatalf("pg_broadcast_total: want 1, got %v", v)
	}
	if c := rec.HistogramCount("pg_broadcast_recipients", metrics.Labels{"pg": "g1"}); c != 1 {
		t.Fatalf("pg_broadcast_recipients count: want 1, got %v", c)
	}
}

func TestTelemetry_NilCollector_NoPanic(t *testing.T) {
	tt := newTelemetry(nil, nil, nil)
	tt.recordJoin("g1", nil, time.Millisecond)
	tt.recordLeave("g1", nil, time.Millisecond)
	tt.recordBroadcast("g1", 1, nil, time.Millisecond)
}

var errSomething = errStub("boom")

type errStub string

func (e errStub) Error() string { return string(e) }
