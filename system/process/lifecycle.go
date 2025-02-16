package process

import (
	"context"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/topology"
	sharedTopology "github.com/ponyruntime/pony/system/topology"
)

// Lifecycle manages all topology-related aspects of process topology
type Lifecycle struct {
	monitor  topology.Monitor
	nodeID   pubsub.NodeID
	upstream pubsub.Upstream
}

// NewTopologyLifecycle creates a new topology manager with the given node's upstream
func NewTopologyLifecycle(
	ctx context.Context,
	upstream pubsub.Upstream,
) *Lifecycle {
	return &Lifecycle{
		monitor:  sharedTopology.NewMonitor(ctx, upstream),
		upstream: upstream,
	}
}

// AttachToContext returns a context with all topology callbacks attached
func (l *Lifecycle) AttachToContext(ctx context.Context) context.Context {
	// Monitor handling
	ctx = process.WithAddedOnStart(ctx, func(startPid pubsub.PID, proc process.Process) {
	})

	ctx = process.WithAddedOnComplete(ctx, func(completePid pubsub.PID, result *runtime.Result) {
		// Handle completion/failure notification
		l.monitor.Notify(completePid, result)
		l.monitor.Remove(completePid)
	})

	return ctx
}

// Future methods for Link and Group access
// func (l *Lifecycle) Link() Link { ... }
// func (l *Lifecycle) Group() Group { ... }

//m.registerLayer(
//func (pid pubsub.PID, proc api.Process) {
//	logger.Info("process started", zap.String("pid", pid.String()))
//},
//func (pid pubsub.PID, result *runtime.Result) {
//	if result.Error != nil {
//		if errors.Is(result.Error, supervisor.ErrExit) {
//			logger.Info("process exited", zap.String("pid", pid.String()))
//		} else {
//			logger.Error("process failed", zap.String("pid", pid.String()), zap.Error(result.Error))
//		}
//	} else {
//		logger.Info("process completed", zap.String("pid", pid.String()), zap.Any("result", result.Payload))
//	}
//},
//)
