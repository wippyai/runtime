package process

import (
	"context"
	"errors"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
	sharedTopology "github.com/ponyruntime/pony/system/topology"
	"go.uber.org/zap"
)

// Topology manages all topology-related aspects of process topology
type Topology struct {
	logger   *zap.Logger
	monitor  topology.Monitor
	nodeID   pubsub.NodeID
	upstream pubsub.Upstream
}

// NewTopology creates a new topology manager with the given node's upstream
func NewTopology(
	ctx context.Context,
	log *zap.Logger,
	upstream pubsub.Upstream,
) *Topology {
	return &Topology{
		logger:   log,
		monitor:  sharedTopology.NewMonitor(ctx, upstream),
		upstream: upstream,
	}
}

// AttachToContext returns a context with all topology callbacks attached
func (l *Topology) AttachToContext(ctx context.Context) context.Context {
	ctx = process.WithAddedOnStart(ctx, func(pid pubsub.PID, proc process.Process) {
		l.logger.Debug("process started", zap.String("pid", pid.String()))
	})

	ctx = process.WithAddedOnComplete(ctx, func(pid pubsub.PID, result *runtime.Result) {
		if result.Error != nil {
			if errors.Is(result.Error, supervisor.ErrExit) {
				l.logger.Debug("process exited", zap.String("pid", pid.String()))
			} else {
				l.logger.Warn("process failed", zap.String("pid", pid.String()), zap.Error(result.Error))
			}
		} else {
			l.logger.Debug("process completed", zap.String("pid", pid.String()), zap.Any("result", result.Payload))
		}

		// Handle completion/failure notification
		l.monitor.Notify(pid, result)
		l.monitor.Remove(pid)
	})

	return ctx
}
