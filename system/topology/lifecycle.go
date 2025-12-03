package topology

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// Lifecycle implements process.Lifecycle to handle topology
// registration, monitoring, and linking based on lifecycle options.
type Lifecycle struct {
	topo   topology.Topology
	pidReg topology.PIDRegistry
	logger *zap.Logger
}

// NewLifecycle creates a new topology lifecycle handler.
func NewLifecycle(topo topology.Topology, pidReg topology.PIDRegistry, logger *zap.Logger) *Lifecycle {
	return &Lifecycle{
		topo:   topo,
		pidReg: pidReg,
		logger: logger,
	}
}

// OnStart registers the process in topology and sets up monitoring/linking
// based on lifecycle options from frame context.
func (t *Lifecycle) OnStart(ctx context.Context, pid relay.PID, _ process.Process) {
	if err := t.topo.Register(pid); err != nil {
		t.logger.Warn("failed to register pid in topology",
			zap.String("pid", pid.String()),
			zap.Error(err))
		return
	}

	opts := runtime.GetFrameLifecycleOptions(ctx)
	if opts == nil {
		return
	}

	attributes, ok := opts.(attrs.Attributes)
	if !ok {
		return
	}

	var parentPID relay.PID
	if parent, ok := attributes.Get(process.LifecycleParentKey); ok {
		if p, ok := parent.(relay.PID); ok {
			parentPID = p
		}
	}

	if parentPID.UniqID == "" {
		return
	}

	monitor := attributes.GetBool(process.LifecycleMonitorKey, false)
	link := attributes.GetBool(process.LifecycleLinkKey, false)

	if monitor {
		if err := t.topo.Wait(parentPID, pid); err != nil {
			t.logger.Warn("failed to monitor process",
				zap.String("pid", pid.String()),
				zap.String("parent", parentPID.String()),
				zap.Error(err))
		}
	}

	if link {
		if err := t.topo.Link(parentPID, pid); err != nil {
			t.logger.Warn("failed to link process",
				zap.String("pid", pid.String()),
				zap.String("parent", parentPID.String()),
				zap.Error(err))
		}
	}
}

// OnComplete notifies watchers and linked processes, then cleans up topology.
func (t *Lifecycle) OnComplete(_ context.Context, pid relay.PID, result *runtime.Result) {
	if result.Error != nil {
		if errors.Is(result.Error, supervisor.ErrExit) {
			result.Error = nil
		}
	}

	t.topo.Notify(pid, result)

	if t.pidReg != nil {
		t.pidReg.Remove(pid)
	}

	t.topo.Remove(pid)
}

var _ process.Lifecycle = (*Lifecycle)(nil)
