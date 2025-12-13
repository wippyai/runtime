package topology

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
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
func (t *Lifecycle) OnStart(ctx context.Context, p pid.PID, _ process.Process) {
	if err := t.topo.Register(p); err != nil {
		t.logger.Warn("failed to register pid in topology",
			zap.String("pid", p.String()),
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

	var parentPID pid.PID
	if parent, ok := attributes.Get(process.LifecycleParentKey); ok {
		if pp, ok := parent.(pid.PID); ok {
			parentPID = pp
		}
	}

	if parentPID.UniqID == "" {
		return
	}

	monitor := attributes.GetBool(process.LifecycleMonitorKey, false)
	link := attributes.GetBool(process.LifecycleLinkKey, false)

	if monitor {
		if err := t.topo.Monitor(parentPID, p); err != nil {
			t.logger.Warn("failed to monitor process",
				zap.String("pid", p.String()),
				zap.String("parent", parentPID.String()),
				zap.Error(err))
		}
	}

	if link {
		if err := t.topo.Link(parentPID, p); err != nil {
			t.logger.Warn("failed to link process",
				zap.String("pid", p.String()),
				zap.String("parent", parentPID.String()),
				zap.Error(err))
		}
	}
}

// OnComplete notifies watchers and linked processes, then cleans up topology.
func (t *Lifecycle) OnComplete(_ context.Context, p pid.PID, result *runtime.Result) {
	notifyResult := result
	if result.Error != nil && errors.Is(result.Error, supervisor.ErrExit) {
		notifyResult = &runtime.Result{Value: result.Value}
	}

	t.topo.Notify(p, notifyResult)

	if t.pidReg != nil {
		t.pidReg.Remove(p)
	}

	t.topo.Remove(p)
}

var _ process.Lifecycle = (*Lifecycle)(nil)
