// SPDX-License-Identifier: MPL-2.0

package global

import (
	"time"

	"github.com/wippyai/runtime/api/metrics"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	forwardResultOK          = "ok"
	forwardResultError       = "error"
	forwardResultDecodeError = "decode_error"
	forwardResultTimeout     = "timeout"
	forwardResultNoLeader    = "no_leader"
	forwardResultSendFailed  = "send_failed"
)

// forwardResultLabels enumerates every value that may be passed to
// recordForwardedApply's `result` argument. The list is used to seed all
// label combinations at zero so dashboards never see "MISSING" before the
// first real event.
var forwardResultLabels = []string{
	forwardResultOK,
	forwardResultError,
	forwardResultDecodeError,
	forwardResultTimeout,
	forwardResultNoLeader,
	forwardResultSendFailed,
}

// forwardCommandLabels enumerates every command label that may be passed to
// recordForwardedApply. Same bootstrap rationale as forwardResultLabels.
var forwardCommandLabels = []string{
	"register",
	"unregister",
	"remove_pid",
	"remove_node",
}

// telemetry owns metric emission for the globalreg subsystem. It emits with
// the same pg_* prefix used elsewhere in the runtime — those metric names
// are part of the Grafana dashboard contract and are independent of the
// owning Go package.
//
// All recorders are nil-safe so callers can ignore the absence of a
// configured collector (e.g., in unit tests that don't wire OTel).
type telemetry struct {
	coll metrics.Collector
}

// newTelemetry constructs a telemetry recorder. The tracer/meter providers
// are accepted to match the per-package constructor pattern but are not
// currently used — the globalreg subsystem only emits metrics.
//
// `localNode` is the node label used to bootstrap the cross-subsystem
// reregistration counter so the validate-chaos-impact.sh hard gate sees
// the series at zero instead of "MISSING".
func newTelemetry(coll metrics.Collector, _ otelmetric.MeterProvider, _ trace.TracerProvider, localNode string) *telemetry {
	t := &telemetry{coll: coll}
	if coll != nil {
		// Bootstrap rare event-driven series so dashboards have visible
		// gauges/counters even before an actual fence token has been
		// committed or a registration deduped.
		coll.GaugeSet("pg_fence_token", 0, metrics.Labels{"pg": "_init", "node": "_init"})
		coll.CounterAdd("pg_globalreg_dedupe_total", 0, nil)
		coll.CounterAdd("runtime_name_reregistrations_total", 0,
			metrics.Labels{"node": localNode, "scope": "global"})
		// Bootstrap forwarded-apply counter labels so dashboards do not see
		// MISSING before the first follower forwards a write. The companion
		// histogram is intentionally not bootstrapped: HistogramObserve adds a
		// real observation which would break unit tests that count samples.
		for _, cmd := range forwardCommandLabels {
			for _, res := range forwardResultLabels {
				coll.CounterAdd("globalreg_forwarded_apply_total", 0,
					metrics.Labels{"cmd": cmd, "result": res})
			}
		}
	}
	return t
}

func (t *telemetry) recordForwardedApply(cmd CommandType, result string, dur time.Duration) {
	if t == nil || t.coll == nil {
		return
	}

	labels := metrics.Labels{"cmd": commandLabel(cmd), "result": result}
	t.coll.CounterInc("globalreg_forwarded_apply_total", labels)
	t.coll.HistogramObserve("globalreg_forwarded_apply_latency_seconds",
		dur.Seconds(), metrics.Labels{"cmd": commandLabel(cmd)})
}

func (t *telemetry) recordFenceToken(pg, node string, token uint64) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("pg_fence_token", float64(token), metrics.Labels{"pg": pg, "node": node})
}

func (t *telemetry) recordGlobalregSize(n int) {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.GaugeSet("pg_globalreg_size", float64(n), nil)
}

func (t *telemetry) recordGlobalregDedupe() {
	if t == nil || t.coll == nil {
		return
	}

	t.coll.CounterInc("pg_globalreg_dedupe_total", nil)
}

// recordReregistration is emitted when a global registration replaces a
// prior owner of the same name. Sustained rate post-partition-heal is a
// flood signal — the soak gate fails the run if the rate stays high.
// The {node, scope} label set must match what eventualreg's telemetry
// emits for the same metric name; otherwise prometheus client_golang
// rejects the second registration with a label-set-mismatch panic.
func (t *telemetry) recordReregistration(node, scope string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("runtime_name_reregistrations_total",
		metrics.Labels{"node": node, "scope": scope})
}

// recordRemoveNodeChunk emits per-chunk progress so dashboards can confirm
// the chunking actually fires under chaos and surface stuck cleanups.
func (t *telemetry) recordRemoveNodeChunk(node string, count int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("globalreg_remove_node_chunks_total", metrics.Labels{"node": node})
	if count > 0 {
		t.coll.CounterAdd("globalreg_remove_node_names_total", float64(count), metrics.Labels{"node": node})
	}
}

// The Strong-scope counters retain "root" in their emitted metric names as
// the stable wire identifier consumed by existing dashboards; the Go method
// names use Strong to match the scope.
func (t *telemetry) recordStrongPending(result string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("globalreg_root_pending_total", metrics.Labels{"result": result})
}

func (t *telemetry) recordStrongActive(bucket string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("globalreg_root_active_total", metrics.Labels{"ack_count_bucket": bucket})
}

func (t *telemetry) recordStrongExpired(reason string) {
	if t == nil || t.coll == nil {
		return
	}
	if reason == "" {
		reason = "unspecified"
	}
	t.coll.CounterInc("globalreg_root_expired_total", metrics.Labels{"reason": reason})
}

func (t *telemetry) recordStrongAck(kind string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("globalreg_root_ack_total", metrics.Labels{"kind": kind})
}

func (t *telemetry) recordStrongRelease(reason string) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("globalreg_root_release_total", metrics.Labels{"reason": reason})
}

func (t *telemetry) setStrongPendingInFlight(n int) {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.GaugeSet("globalreg_root_pending_in_flight", float64(n), nil)
}

// recordStrongDropRequired counts departed-node prunes of in-flight pending
// RequiredNodes sets (issued by the leader on NodeLeft).
func (t *telemetry) recordStrongDropRequired() {
	if t == nil || t.coll == nil {
		return
	}
	t.coll.CounterInc("globalreg_root_drop_required_total", nil)
}
