package host

import (
	"context"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"go.uber.org/zap"
)

type processInfo struct {
	pid     pubsub.PID
	process process.Process
}

type Host struct {
	id     registry.ID
	config process.HostConfig
	log    *zap.Logger

	processes  sync.Map // map[pubsub.PID]*processInfo
	pidBatchCh chan *pubsub.PIDBatch

	ctx  context.Context
	done chan struct{}
}

func NewHost(id registry.ID, config process.HostConfig, log *zap.Logger) *Host {
	h := &Host{
		id:         id,
		config:     config,
		log:        log,
		pidBatchCh: make(chan *pubsub.PIDBatch, config.BufferSize),
		done:       make(chan struct{}),
	}

	return h
}

func (h *Host) Start(ctx context.Context) (<-chan any, error) {
	status := make(chan any, 1)

	h.ctx = process.WithAddedOnComplete(ctx, func(pid pubsub.PID, result *runtime.Result) {
		if result.Error != nil {
			h.log.Error("process execution failed",
				zap.String("pid", pid.String()),
				zap.Error(result.Error))
		} else {
			h.log.Info("process execution completed",
				zap.String("pid", pid.String()),
				zap.Any("result", result.Payload))
		}
		h.processes.Delete(pid)
	})

	go h.run(status)
	return status, nil
}

func (h *Host) Stop(ctx context.Context) error {
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
		//case <-time.After(h.config.ShutdownTimeout):
		//	return context.DeadlineExceeded
	}
}

func (h *Host) run(status chan<- any) {
	defer close(h.done)
	defer close(status)
	defer h.cleanup()

	status <- "started"

	for {
		select {
		case <-h.ctx.Done():
			return
		case pidBatch := <-h.pidBatchCh:
			if info, ok := h.processes.Load(pidBatch.PID); ok {
				procInfo := info.(*processInfo)

				// todo: we expect that process will wake up automatically!
				if err := procInfo.process.Send(pidBatch.Batch); err != nil {
					h.log.Error("failed to send message to process",
						zap.String("pid", procInfo.pid.String()),
						zap.Error(err))
				}
			}
		}
	}
}

func (h *Host) cleanup() {
	// todo: stop workers!
	h.processes.Range(func(pid, _ interface{}) bool {
		h.processes.Delete(pid)
		return true
	})

	for len(h.pidBatchCh) > 0 {
		<-h.pidBatchCh
	}

	h.log.Info("host cleanup completed")
}

func (h *Host) Launch(ctx context.Context, launch *process.LaunchProcess) (pubsub.PID, error) {
	// check if pid is busy
	if _, loaded := h.processes.Load(launch.PID); loaded {
		return pubsub.PID{}, process.ErrHostBusy
	}

	if err := launch.Process.Start(h.ctx, launch.PID, launch.Input); err != nil {
		return pubsub.PID{}, err
	}

	info := &processInfo{
		pid:     launch.PID,
		process: launch.Process,
	}
	h.processes.Store(launch.PID, info)

	go func() {
		timer := time.NewTicker(time.Millisecond * 100)
		defer timer.Stop()

		for {
			select {
			case <-h.ctx.Done():
				return
			case <-timer.C:
				if err := info.process.Step(); err != nil {
					h.log.Error("initial process step failed",
						zap.String("pid", launch.PID.String()),
						zap.Error(err))
				}
			}
		}
	}()

	h.log.Info("process launched", zap.String("pid", launch.PID.String()))
	return launch.PID, nil
}

func (h *Host) Terminate(ctx context.Context, pid pubsub.PID) error {
	h.processes.Delete(pid)
	h.log.Info("process terminated", zap.String("pid", pid.String()))
	return nil
}

func (h *Host) Send(ctx context.Context, pid pubsub.PID, batch *pubsub.Batch) error {
	pidBatch := &pubsub.PIDBatch{
		PID:   pid,
		Batch: batch,
	}

	select {
	case h.pidBatchCh <- pidBatch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return process.ErrHostBusy
	}
}

func (h *Host) Attach(pid pubsub.PID, ch chan *pubsub.Batch) (error, context.CancelFunc) {
	_, loaded := h.processes.LoadOrStore(pid, ch)
	if loaded {
		return pubsub.ErrAlreadyAttached, nil
	}

	cancel := func() {
		h.processes.Delete(pid)
		h.log.Debug("receiver detached", zap.String("pid", pid.String()))
	}

	h.log.Debug("receiver attached", zap.String("pid", pid.String()))
	return nil, cancel
}
