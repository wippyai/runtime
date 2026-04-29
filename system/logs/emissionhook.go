// SPDX-License-Identifier: MPL-2.0

package logs

import (
	"strings"
	"sync/atomic"

	"github.com/wippyai/runtime/api/metrics"
	"go.uber.org/zap/zapcore"
)

// emissionCollector holds the metrics collector that the per-emission
// log hook reports to. The logger is built before the metrics subsystem
// is wired (cmd/internal/app/app.go runs Init before any boot
// component), so the hook reads this atomic on every emission. Nil
// means the metrics subsystem has not booted yet — the hook is a no-op
// until SetEmissionCollector is called.
var emissionCollector atomic.Pointer[metrics.Collector]

// SetEmissionCollector binds the metrics collector that the
// per-emission zap hook reports to. Call once after the collector is
// constructed. Safe to call concurrently with log emissions; the hook
// reads the pointer atomically on each call. Pass nil to detach (e.g.
// during shutdown).
func SetEmissionCollector(c metrics.Collector) {
	if c == nil {
		emissionCollector.Store(nil)
		return
	}
	emissionCollector.Store(&c)
}

// EmissionMetricName is the counter incremented on every log emission.
// Labels: level (debug/info/warn/error/dpanic/panic/fatal) and
// component (the top-level logger name — see topLevelComponent).
const EmissionMetricName = "runtime_log_emissions_total"

// topLevelComponent returns the first dotted segment of a zap logger
// name. The counter is labeled with this rather than the full name so
// cardinality stays bounded by the count of top-level subsystems
// (~20: raft, pg, cluster, core, lua, …) instead of the full
// named-chain (which can include node IDs, host IDs, etc. — unbounded
// under chaos churn).
func topLevelComponent(name string) string {
	if name == "" {
		return "unnamed"
	}
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return name[:i]
	}
	return name
}

// EmissionHook is the zap.Hook fired on every emission. Reads the
// collector atomically; no-op when the metrics subsystem hasn't booted
// yet (early startup, tests).
func EmissionHook(e zapcore.Entry) error {
	cp := emissionCollector.Load()
	if cp == nil {
		return nil
	}
	(*cp).CounterInc(EmissionMetricName, metrics.Labels{
		"level":     e.Level.String(),
		"component": topLevelComponent(e.LoggerName),
	})
	return nil
}
