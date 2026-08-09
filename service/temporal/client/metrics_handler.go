// SPDX-License-Identifier: MPL-2.0

package client

import (
	"time"

	"github.com/wippyai/runtime/api/metrics"
	"go.temporal.io/sdk/client"
)

// metricsHandler bridges the Temporal SDK metrics.Handler interface onto the
// wippy metrics collector, so the SDK's own workflow/worker/poller metrics
// flow to both the Prometheus and OTel sinks. Tags become metric labels; a nil
// collector makes every method a no-op.
type metricsHandler struct {
	coll metrics.Collector
	tags metrics.Labels
}

// NewMetricsHandler returns a Temporal metrics.Handler backed by coll.
func NewMetricsHandler(coll metrics.Collector) client.MetricsHandler {
	return &metricsHandler{coll: coll}
}

func (h *metricsHandler) WithTags(tags map[string]string) client.MetricsHandler {
	merged := make(metrics.Labels, len(h.tags)+len(tags))
	for k, v := range h.tags {
		merged[k] = v
	}
	for k, v := range tags {
		merged[k] = v
	}
	return &metricsHandler{coll: h.coll, tags: merged}
}

type metricsCounterFunc func(int64)

func (f metricsCounterFunc) Inc(d int64) { f(d) }

type metricsGaugeFunc func(float64)

func (f metricsGaugeFunc) Update(d float64) { f(d) }

type metricsTimerFunc func(time.Duration)

func (f metricsTimerFunc) Record(d time.Duration) { f(d) }

func (h *metricsHandler) Counter(name string) client.MetricsCounter {
	return metricsCounterFunc(func(d int64) {
		if h.coll != nil {
			h.coll.CounterAdd(name, float64(d), h.tags)
		}
	})
}

func (h *metricsHandler) Gauge(name string) client.MetricsGauge {
	return metricsGaugeFunc(func(d float64) {
		if h.coll != nil {
			h.coll.GaugeSet(name, d, h.tags)
		}
	})
}

func (h *metricsHandler) Timer(name string) client.MetricsTimer {
	return metricsTimerFunc(func(d time.Duration) {
		if h.coll != nil {
			h.coll.HistogramObserve(name, d.Seconds(), h.tags)
		}
	})
}
