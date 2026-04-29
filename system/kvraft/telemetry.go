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
		// Bootstrap the watch-overflow counter so the validate-chaos-impact.sh
		// hard gate distinguishes "no watcher overflowed" (visible at 0) from
		// "kv subsystem disabled" (series absent).
		coll.CounterAdd("kv_watch_dropped_total", 0,
			metrics.Labels{"space": "_init", "mode": "raft"})
	}
	return t
}

func (t *telemetry) recordReap(removed int) {
	if t == nil || t.coll == nil || removed <= 0 {
		return
	}
	t.coll.CounterAdd("kvraft_reap_total", float64(removed), nil)
}
