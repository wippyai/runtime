// SPDX-License-Identifier: MPL-2.0

package telemetrytest

import (
	"testing"

	"github.com/wippyai/runtime/api/metrics"
)

func TestRecorder_RecordsCountersAndLabels(t *testing.T) {
	r := NewRecorder()

	r.CounterAdd("pg_join_total", 1, metrics.Labels{"pg": "g1", "result": "ok"})
	r.CounterAdd("pg_join_total", 1, metrics.Labels{"pg": "g1", "result": "ok"})
	r.CounterAdd("pg_join_total", 1, metrics.Labels{"pg": "g2", "result": "err"})

	got := r.CounterValue("pg_join_total", metrics.Labels{"pg": "g1", "result": "ok"})
	if got != 2 {
		t.Fatalf("want 2, got %v", got)
	}
	got = r.CounterValue("pg_join_total", metrics.Labels{"pg": "g2", "result": "err"})
	if got != 1 {
		t.Fatalf("want 1, got %v", got)
	}
}

func TestRecorder_GaugeAndHistogram(t *testing.T) {
	r := NewRecorder()
	r.GaugeSet("pg_queue_depth", 7, metrics.Labels{"pg": "g1"})
	if v := r.GaugeValue("pg_queue_depth", metrics.Labels{"pg": "g1"}); v != 7 {
		t.Fatalf("want 7, got %v", v)
	}
	r.HistogramObserve("pg_op_duration_seconds", 0.5, metrics.Labels{"pg": "g1", "op": "join"})
	r.HistogramObserve("pg_op_duration_seconds", 0.7, metrics.Labels{"pg": "g1", "op": "join"})
	if c := r.HistogramCount("pg_op_duration_seconds", metrics.Labels{"pg": "g1", "op": "join"}); c != 2 {
		t.Fatalf("want 2 observations, got %v", c)
	}
}
