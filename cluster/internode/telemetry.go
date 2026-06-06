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
	for _, c := range []Class{ClassRaftControl, ClassGossip, ClassPGBroadcast, ClassRaftRPC} {
		coll.CounterAdd("internode_dropped_total", 0, metrics.Labels{
			"class": c.String(), "reason": "queue_full",
		})
		coll.CounterAdd("internode_dropped_total", 0, metrics.Labels{
			"class": c.String(), "reason": "conn_queue_full",
		})
		coll.GaugeSet("internode_queue_depth", 0, metrics.Labels{
			"class": c.String(), "peer": "_init",
		})
	}
	// Drops that don't have a class context (delivery RX failures, target not
	// managed at all). Bootstrapped so dashboards see the series at zero.
	coll.CounterAdd("internode_dropped_total", 0, metrics.Labels{
		"class": "unknown", "reason": "node_not_managed",
	})
	coll.CounterAdd("internode_dropped_total", 0, metrics.Labels{
		"class": "unknown", "reason": "delivery_failed",
	})
	coll.CounterAdd("internode_state_evicted_total", 0, metrics.Labels{
		"reason": "orphan",
	})
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

// recordDropReason increments the drop counter without a class context.
// Used when the caller doesn't know the message class (e.g. RX delivery
// failure, target not registered).
func (t *telemetry) recordDropReason(reason string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("internode_dropped_total", metrics.Labels{
		"class": "unknown", "reason": reason,
	})
}

// recordEviction is incremented by the orphan-sweep path when a node is
// dropped from the manager because the membership view no longer
// includes it. Should normally stay at zero — non-zero indicates that
// `cluster.NodeLeft` was missed and the defensive sweep is doing real
// work.
func (t *telemetry) recordEviction(reason string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("internode_state_evicted_total", metrics.Labels{
		"reason": reason,
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
