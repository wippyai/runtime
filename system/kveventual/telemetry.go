// SPDX-License-Identifier: MPL-2.0

package kveventual

import "github.com/wippyai/runtime/api/metrics"

type telemetry struct {
	coll metrics.Collector
	node string
}

func newTelemetry(coll metrics.Collector, node string) *telemetry {
	t := &telemetry{coll: coll, node: node}
	if coll != nil {
		// Bootstrap with the FULL label set the recorder will later use,
		// so the prometheus exporter doesn't see a label-set mismatch
		// between the bootstrap series and the first real CounterInc/Add.
		coll.CounterAdd("kveventual_space_open_total", 0,
			metrics.Labels{"node": node, "space": "_init"})
		coll.CounterAdd("kveventual_op_total", 0,
			metrics.Labels{"node": node, "space": "_init", "op": "put", "result": "ok"})
		coll.CounterAdd("kveventual_bytes_total", 0,
			metrics.Labels{"node": node, "space": "_init", "dir": "tx", "kind": "delta"})
		coll.CounterAdd("kveventual_tombstones_gc_total", 0,
			metrics.Labels{"node": node, "space": "_init", "reason": "safe_counter"})
		coll.GaugeSet("kveventual_entries", 0,
			metrics.Labels{"node": node, "space": "_init", "state": "live"})
		coll.GaugeSet("kveventual_entries", 0,
			metrics.Labels{"node": node, "space": "_init", "state": "tombstone"})
		// Bootstrap the watch-overflow counter so the validate-chaos-impact.sh
		// hard gate sees the series at zero instead of "MISSING (kv subsystem
		// disabled)" — the gate is otherwise silently dormant whenever no
		// watcher has overflowed yet.
		coll.CounterAdd("kv_watch_dropped_total", 0,
			metrics.Labels{"space": "_init", "mode": "eventual"})
		// Bootstrap the broadcast-queue overflow counter with the full
		// label set per-space so saturation is visible at the producer
		// (not just inferable from receiver-side gaps).
		coll.GaugeSet("kveventual_queue_dropped_total", 0,
			metrics.Labels{"node": node, "space": "_init"})
	}
	return t
}

func (t *telemetry) recordSpaceOpen(name string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("kveventual_space_open_total", metrics.Labels{
		"node":  t.node,
		"space": name,
	})
}

func (t *telemetry) recordDigestMismatch(name string, n int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.HistogramObserve("kveventual_antientropy_mismatched_shards", float64(n),
		metrics.Labels{"node": t.node, "space": name})
}
