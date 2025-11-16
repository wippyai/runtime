package process

import (
	"context"
	"errors"

	api "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// TopologyLifecycleMutator creates a StartMutator that adds topology lifecycle hooks
func TopologyLifecycleMutator(logger *zap.Logger) api.StartMutator {
	return func(ctx context.Context, start *api.Start) (context.Context, error) {
		// Capture topology and pid registry from Manager's context
		topo := topology.GetTopology(ctx)
		if topo == nil {
			return ctx, errors.New("topology not found in context")
		}

		pidReg := topology.GetRegistry(ctx)
		if pidReg == nil {
			return ctx, errors.New("pid registry not found in context")
		}

		// Read lifecycle parameters from Options
		var parentPID relay.PID
		if parent, ok := start.Options.Get(api.LifecycleParentKey); ok {
			if pid, ok := parent.(relay.PID); ok {
				parentPID = pid
			}
		}
		monitor := start.Options.GetBool(api.LifecycleMonitorKey, false)
		link := start.Options.GetBool(api.LifecycleLinkKey, false)

		// Build OnStart hook for topology registration
		topologyOnStart := func(ctx context.Context, pid relay.PID, _ api.Process) {
			// Register the PID with topology
			err := topo.Register(pid)
			if err != nil {
				logger.Warn("failed to register pid for monitoring",
					zap.String("pid", pid.String()),
					zap.Error(err))
				return
			}

			// Set up monitoring if requested and Parent PID is provided
			if monitor && parentPID.String() != "" {
				if err = topo.Wait(parentPID, pid); err != nil {
					logger.Warn("failed to monitor process",
						zap.String("pid", pid.String()),
						zap.String("caller", parentPID.String()),
						zap.Error(err))
				}
			}

			// Set up linking if requested and Parent PID is provided
			if link && parentPID.String() != "" {
				if err = topo.Link(parentPID, pid); err != nil {
					logger.Warn("failed to link process",
						zap.String("pid", pid.String()),
						zap.String("caller", parentPID.String()),
						zap.Error(err))
				}
			}
		}

		// Build OnComplete hook for topology cleanup
		topologyOnComplete := func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			if result.Error != nil {
				if errors.Is(result.Error, supervisor.ErrExit) {
					result.Error = nil // normal exit
				}
			}

			topo.Notify(pid, result)
			pidReg.Remove(pid)
			topo.Remove(pid)
		}

		// Append hooks to start request
		start.OnStart = append(start.OnStart, topologyOnStart)
		start.OnComplete = append(start.OnComplete, topologyOnComplete)

		return ctx, nil
	}
}
