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
// If a name is specified in lifecycle options, attempts atomic name registration.
// Returns error if name registration fails (name already taken).
func (t *Lifecycle) OnStart(ctx context.Context, p pid.PID, _ process.Process) error {
	opts := runtime.GetFrameLifecycleOptions(ctx)
	var attributes attrs.Attributes
	if opts != nil {
		attributes, _ = opts.(attrs.Attributes)
	}

	// Handle name registration first (before topology registration)
	// This allows atomic spawn-or-signal: routing is ready, now claim the name
	if attributes != nil && t.pidReg != nil {
		if name := attributes.GetString(process.ProcessNameKey, ""); name != "" {
			existingPID, err := t.pidReg.Register(name, p)
			if err != nil {
				return topology.NameAlreadyRegisteredError(existingPID)
			}
		}
	}

	if err := t.topo.Register(p); err != nil {
		t.logger.Warn("failed to register pid in topology",
			zap.String("pid", p.String()),
			zap.Error(err))
		return nil // topology registration failure is not fatal
	}

	if attributes == nil {
		return nil
	}

	var parentPID pid.PID
	if parent, ok := attributes.Get(process.ProcessParentKey); ok {
		if pp, ok := parent.(pid.PID); ok {
			parentPID = pp
		}
	}

	if parentPID.UniqID == "" {
		return nil
	}

	monitor := attributes.GetBool(process.ProcessMonitorKey, false)
	link := attributes.GetBool(process.ProcessLinkKey, false)

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

	return nil
}

// OnComplete notifies watchers and linked processes, then cleans up topology.
func (t *Lifecycle) OnComplete(_ context.Context, p pid.PID, result *runtime.Result) {
	notifyResult := result
	if result.Error != nil && errors.Is(result.Error, supervisor.ErrExit) {
		notifyResult = &runtime.Result{Value: result.Value}
	}

	if t.pidReg != nil {
		t.pidReg.Remove(p)
	}

	t.topo.Complete(p, notifyResult)
}

var _ process.Lifecycle = (*Lifecycle)(nil)
