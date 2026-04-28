// SPDX-License-Identifier: MPL-2.0

package internode

import "github.com/wippyai/runtime/api/metrics"

// telemetry owns metric emission for the internode subsystem. nil-safe so
// unit tests without a Collector wired still work.
type telemetry struct {
	coll metrics.Collector
}

func newTelemetry(coll metrics.Collector) *telemetry {
	t := &telemetry{coll: coll}
	if coll == nil {
		return t
	}
	// Bootstrap counters so dashboards have visible series before any drop.
	for _, c := range []Class{ClassRaftControl, ClassGossip, ClassPGBroadcast} {
		coll.CounterAdd("internode_dropped_total", 0, metrics.Labels{
			"class": c.String(), "reason": "queue_full",
		})
		coll.GaugeSet("internode_queue_depth", 0, metrics.Labels{
			"class": c.String(), "peer": "_init",
		})
	}
	return t
}

func (t *telemetry) recordDrop(class Class, reason string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("internode_dropped_total", metrics.Labels{
		"class": class.String(), "reason": reason,
	})
}

func (t *telemetry) recordQueueDepth(class Class, peer string, depth int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("internode_queue_depth", float64(depth), metrics.Labels{
		"class": class.String(), "peer": peer,
	})
}
