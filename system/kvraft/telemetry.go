// SPDX-License-Identifier: MPL-2.0

package kvraft

import "github.com/wippyai/runtime/api/metrics"

type telemetry struct {
	coll metrics.Collector
}

func newTelemetry(coll metrics.Collector) *telemetry {
	t := &telemetry{coll: coll}
	if coll != nil {
		coll.CounterAdd("kvraft_reap_total", 0, nil)
	}
	return t
}

func (t *telemetry) recordReap(removed int) {
	if t == nil || t.coll == nil || removed <= 0 {
		return
	}
	t.coll.CounterAdd("kvraft_reap_total", float64(removed), nil)
}
