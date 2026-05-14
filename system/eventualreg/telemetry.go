// SPDX-License-Identifier: MPL-2.0

package eventualreg

import (
	"github.com/wippyai/runtime/api/metrics"
)

// telemetry owns metric emission for the eventualreg subsystem.
// All recorders are nil-safe.
type telemetry struct {
	coll metrics.Collector
	node string
}

func newTelemetry(coll metrics.Collector, node string) *telemetry {
	t := &telemetry{coll: coll, node: node}
	if coll != nil {
		// Bootstrap rare event series so dashboards have visible counters
		// before chaos surfaces a real event.
		base := metrics.Labels{"node": node}
		coll.CounterAdd("eventualreg_register_total", 0, copyAdd(base, "result", "ok"))
		coll.CounterAdd("eventualreg_unregister_total", 0, copyAdd(base, "result", "ok"))
		coll.CounterAdd("eventualreg_merge_conflicts_total", 0, copyAdd(base, "resolution", "wall_clock"))
		coll.CounterAdd("eventualreg_tombstones_gc_total", 0, copyAdd(base, "reason", "safe_counter"))
		coll.CounterAdd("eventualreg_tombstones_late_arrival_total", 0, base)
		coll.CounterAdd("eventualreg_antientropy_round_total", 0, copyAdd(base, "result", "ok"))
		coll.GaugeSet("eventualreg_entries", 0, copyAdd(base, "state", "live"))
		coll.GaugeSet("eventualreg_entries", 0, copyAdd(base, "state", "tombstone"))
		coll.GaugeSet("eventualreg_broadcast_queue_depth", 0, base)
		coll.GaugeSet("eventualreg_queue_dropped_total", 0, base)
		// Bootstrap the cross-subsystem reregistration counter so the
		// validate-chaos-impact.sh hard gate sees the series at zero
		// instead of "MISSING (no series — feature not deployed yet)".
		// Without this, the gate is silently dormant whenever no
		// reregistration has fired yet, defeating the safety net.
		coll.CounterAdd("runtime_name_reregistrations_total", 0,
			metrics.Labels{"node": node, "scope": "eventual"})
	}
	return t
}

func (t *telemetry) recordRegister(result string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("eventualreg_register_total", metrics.Labels{"node": t.node, "result": result})
}

func (t *telemetry) recordUnregister(result string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("eventualreg_unregister_total", metrics.Labels{"node": t.node, "result": result})
}

func (t *telemetry) recordMergeConflict(resolution string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("eventualreg_merge_conflicts_total", metrics.Labels{"node": t.node, "resolution": resolution})
}

func (t *telemetry) recordTombstoneGC(reason string, n int) {
	if t == nil || t.coll == nil || n == 0 {
		return
	}
	t.coll.CounterAdd("eventualreg_tombstones_gc_total", float64(n), metrics.Labels{"node": t.node, "reason": reason})
}

func (t *telemetry) recordTombstoneLate() {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("eventualreg_tombstones_late_arrival_total", metrics.Labels{"node": t.node})
}

func (t *telemetry) recordAntiEntropy(result string, durationMs float64, shardsSynced int) {
	if t == nil || t.coll == nil {
		return
	}
	labels := metrics.Labels{"node": t.node, "result": result}
	t.coll.CounterInc("eventualreg_antientropy_round_total", labels)
	// Prometheus convention is _seconds; observing ms into the default
	// _seconds buckets (0.005..10) puts every real observation (~10-200ms)
	// in +Inf, making the histogram useless. Convert here and rename so
	// the bucket boundaries are commensurate with the observed values.
	t.coll.HistogramObserve("eventualreg_antientropy_duration_seconds", durationMs/1000.0,
		metrics.Labels{"node": t.node, "result": result})
	if shardsSynced > 0 {
		t.coll.HistogramObserve("eventualreg_antientropy_shards_synced", float64(shardsSynced), metrics.Labels{"node": t.node})
	}
}

func (t *telemetry) recordDeltaBytes(direction, kind string, n int) {
	if t == nil || t.coll == nil || n == 0 {
		return
	}
	t.coll.CounterAdd("eventualreg_delta_bytes_total", float64(n),
		metrics.Labels{"node": t.node, "dir": direction, "kind": kind})
}

func (t *telemetry) setEntries(live, tombstone int64) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("eventualreg_entries", float64(live), metrics.Labels{"node": t.node, "state": "live"})
	t.coll.GaugeSet("eventualreg_entries", float64(tombstone), metrics.Labels{"node": t.node, "state": "tombstone"})
}

func (t *telemetry) setQueueDepth(d int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("eventualreg_broadcast_queue_depth", float64(d), metrics.Labels{"node": t.node})
}

// setQueueDropped publishes the cumulative count of entries Push() rejected
// because the bounded queue was full. Without this, queue saturation is
// silent — operator sees depth=cap but doesn't know if data is being lost.
// The gauge holds the cumulative count; rate() turns it into a drop rate.
func (t *telemetry) setQueueDropped(d uint64) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("eventualreg_queue_dropped_total", float64(d), metrics.Labels{"node": t.node})
}

func (t *telemetry) recordReregistration() {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("runtime_name_reregistrations_total", metrics.Labels{"node": t.node, "scope": "eventual"})
}

// recordShardRequest counts targeted shard-pull requests by outcome.
// "sent" — request emitted toward a peer; "suppressed" — request elided
// by the per-peer cooldown; "send_error" — MessageSender returned an
// error. Backs the empirical answer to "did the shard-pull path
// recover dropped deltas during chaos?"
func (t *telemetry) recordShardRequest(outcome string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("eventualreg_shard_request_total",
		metrics.Labels{"node": t.node, "outcome": outcome})
}

// recordShardResponse counts shard-response frames by direction.
// "tx" — emitted in response to an incoming request; "rx" — received
// and merged into local state.
func (t *telemetry) recordShardResponse(direction string, shards int) {
	if t == nil || t.coll == nil || shards == 0 {
		return
	}
	t.coll.CounterAdd("eventualreg_shard_response_total", float64(shards),
		metrics.Labels{"node": t.node, "dir": direction})
}

// copyAdd returns a copy of base with key=val added.
func copyAdd(base metrics.Labels, key, val string) metrics.Labels {
	out := make(metrics.Labels, len(base)+1)
	for k, v := range base {
		out[k] = v
	}
	out[key] = val
	return out
}
