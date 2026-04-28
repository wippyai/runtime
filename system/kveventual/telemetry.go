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
		base := metrics.Labels{"node": node}
		coll.CounterAdd("kveventual_space_open_total", 0, base)
		coll.CounterAdd("kveventual_op_total", 0,
			metrics.Labels{"node": node, "space": "_init", "op": "put", "result": "ok"})
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
