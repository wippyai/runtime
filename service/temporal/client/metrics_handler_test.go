// SPDX-License-Identifier: MPL-2.0

package client

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/internal/telemetrytest"
)

func TestMetricsHandler_CounterGaugeTimer(t *testing.T) {
	rec := telemetrytest.NewRecorder()
	h := NewMetricsHandler(rec)

	h.Counter("temporal_workflow_completed").Inc(1)
	h.Counter("temporal_workflow_completed").Inc(2)
	h.Gauge("temporal_worker_task_queue_active").Update(5)
	h.Timer("temporal_workflow_task_latency").Record(100 * time.Millisecond)

	assert.Equal(t, 3.0, rec.CounterValue("temporal_workflow_completed", metrics.Labels{}))
	assert.Equal(t, 5.0, rec.GaugeValue("temporal_worker_task_queue_active", metrics.Labels{}))
	assert.Equal(t, uint64(1), rec.HistogramCount("temporal_workflow_task_latency", metrics.Labels{}))
}

func TestMetricsHandler_WithTagsMergesLabels(t *testing.T) {
	rec := telemetrytest.NewRecorder()
	h := NewMetricsHandler(rec).WithTags(map[string]string{"namespace": "default", "task_queue": "orders"})

	h.Counter("temporal_workflow_completed").Inc(1)

	assert.Equal(t, 1.0, rec.CounterValue("temporal_workflow_completed",
		metrics.Labels{"namespace": "default", "task_queue": "orders"}))
}

func TestMetricsHandler_NilCollector(t *testing.T) {
	h := NewMetricsHandler(nil)
	h.Counter("x").Inc(1)
	h.Gauge("y").Update(1)
	h.Timer("z").Record(time.Millisecond)
}

func TestMetricsHandler_WithTagsChains(t *testing.T) {
	rec := telemetrytest.NewRecorder()
	h := NewMetricsHandler(rec).WithTags(map[string]string{"a": "1"}).WithTags(map[string]string{"b": "2"})

	h.Counter("c").Inc(1)

	assert.Equal(t, 1.0, rec.CounterValue("c", metrics.Labels{"a": "1", "b": "2"}))
}
