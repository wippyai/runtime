// SPDX-License-Identifier: MPL-2.0

package core

import (
	"context"
	goruntime "runtime"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/system/scheduler/affinity"
	"go.uber.org/zap"
)

// WASMIsolation computes the actor/WASM CPU partition from
// `scheduler.wasm_isolation` and publishes it on the context for the host
// manager (actor workers) and the WASM function manager (pinned pool) to consume.
//
// Disabled by default. When enabled it only reserves cores; it does not change
// GOMAXPROCS — the runtime keeps one P per core so the actor and WASM thread
// pools, each sized to its core set, run in parallel on their reserved cores.
// CPU pinning itself is Linux-only; on other platforms the partition is computed
// and pools are still sized, but threads are not bound to cores.
func WASMIsolation() boot.Component {
	return boot.New(boot.P{
		Name: WASMIsolationName,
		Load: func(ctx context.Context) (context.Context, error) {
			cfg := boot.GetConfig(ctx)
			if cfg == nil {
				return ctx, nil
			}

			sub := cfg.Sub(SchedulerName).Sub(WASMIsolationName)
			if !sub.GetBool(WASMIsolationEnabled, false) {
				return ctx, nil
			}

			numCPU := goruntime.NumCPU()
			reserved := sub.GetInt(WASMIsolationReserved, 1)
			part := affinity.Compute(numCPU, reserved, nil, nil)

			logger := logapi.GetLogger(ctx).Named("scheduler.affinity")
			if !part.Enabled {
				logger.Warn("wasm isolation enabled but core split is invalid; ignoring",
					zap.Int("num_cpu", numCPU),
					zap.Int("reserved_cores", reserved))
				return ctx, nil
			}

			logger.Info("wasm isolation enabled: cores partitioned",
				zap.Int("num_cpu", numCPU),
				zap.Ints("actor_cpus", part.ActorCPUs),
				zap.Ints("wasm_cpus", part.WASMCPUs))

			return affinity.WithPartition(ctx, part), nil
		},
	})
}
