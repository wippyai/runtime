// SPDX-License-Identifier: MPL-2.0

package consumer

import (
	"time"

	"github.com/wippyai/runtime/api/metrics"
)

const (
	metricMessagesTotal   = "wippy_queue_messages_total"
	metricProcessDuration = "wippy_queue_process_duration_seconds"
	metricInFlight        = "wippy_queue_in_flight"
)

// telemetry owns metric emission for the queue consumer subsystem. It is
// nil-safe so callers can ignore the absence of a configured collector.
type telemetry struct {
	coll metrics.Collector
}

func newTelemetry(coll metrics.Collector) *telemetry {
	return &telemetry{coll: coll}
}

func (t *telemetry) recordProcessed(queue, result string, duration time.Duration) {
	if t == nil || t.coll == nil {
		return
	}
	labels := metrics.Labels{"queue": queue, "result": result}
	t.coll.CounterInc(metricMessagesTotal, labels)
	t.coll.HistogramObserve(metricProcessDuration, duration.Seconds(), labels)
}

func (t *telemetry) inFlightInc(queue string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeInc(metricInFlight, metrics.Labels{"queue": queue})
}

func (t *telemetry) inFlightDec(queue string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeDec(metricInFlight, metrics.Labels{"queue": queue})
}
