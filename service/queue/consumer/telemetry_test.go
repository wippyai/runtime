// SPDX-License-Identifier: MPL-2.0

package consumer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/internal/telemetrytest"
)

func TestTelemetry_RecordProcessed(t *testing.T) {
	rec := telemetrytest.NewRecorder()
	tel := newTelemetry(rec)

	tel.recordProcessed("ns:orders", "ack", 5*time.Millisecond)

	assert.Equal(t, 1.0, rec.CounterValue(metricMessagesTotal,
		metrics.Labels{"queue": "ns:orders", "result": "ack"}))
	assert.Equal(t, uint64(1), rec.HistogramCount(metricProcessDuration,
		metrics.Labels{"queue": "ns:orders", "result": "ack"}))
}

func TestTelemetry_NilCollector_NoPanic(t *testing.T) {
	tel := newTelemetry(nil)
	tel.recordProcessed("ns:orders", "nack", time.Millisecond)
	tel.inFlightInc("ns:orders")
	tel.inFlightDec("ns:orders")
}

func TestTelemetry_InFlight(t *testing.T) {
	rec := telemetrytest.NewRecorder()
	tel := newTelemetry(rec)

	tel.inFlightInc("ns:orders")
	tel.inFlightInc("ns:orders")
	tel.inFlightDec("ns:orders")

	assert.Equal(t, 1.0, rec.GaugeValue(metricInFlight,
		metrics.Labels{"queue": "ns:orders"}))
}
