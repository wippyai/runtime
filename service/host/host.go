package host

import (
	"context"
	"errors"
	contextApi "github.com/ponyruntime/pony/api/context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	msg "github.com/ponyruntime/pony/system/pubsub"
	"go.uber.org/zap"
)

// ProcessInfo holds a launched process and its PID.
type ProcessInfo struct {
	pid     pubsub.PID
	process process.Process
}

// ProcessHost composes an internal pubsub.Host to manage process launching,
// routing, and graceful shutdown. It uses an external status channel for notifications.
type ProcessHost struct {
	id           registry.ID
	config       process.HostConfig
	log          *zap.Logger
	msgHost      *msg.Host
	hostMessages chan *pubsub.PIDBatch
	processes    sync.Map // map[pubsub.PID]*ProcessInfo
	workers      *WorkerPool
	ctx          context.Context
	done         chan struct{}
	msgWG        sync.WaitGroup
	shutdown     atomic.Bool // shutdown flag: true if shutdown in progress.
	statusChat   chan string // Optional external status notification channel.
}

// NewProcessHost creates a new ProcessHost instance.
func NewProcessHost(id registry.ID, config process.HostConfig, log *zap.Logger, msgHost *msg.Host) *ProcessHost {
	return &ProcessHost{
		id:           id,
		config:       config,
		log:          log,
		msgHost:      msgHost,
		hostMessages: make(chan *pubsub.PIDBatch, config.BufferSize),
		done:         make(chan struct{}),
	}
}

// sendStatus sends a status message to the external status channel if available.
func (mph *ProcessHost) sendStatus(message string) {
	if mph.statusChat != nil {
		select {
		case mph.statusChat <- message:
		default:
			// Drop message if the channel is full.
		}
	}
}

// Start initializes the ProcessHost, starts the worker pool and routing workers,
// and sends an external notification that the host is active.
func (mph *ProcessHost) Start(ctx context.Context) (<-chan any, error) {
	status := make(chan any, 1)
	// Wrap the incoming context with an on-complete callback.
	mph.ctx = process.WithAddedOnComplete(ctx, mph.finalizeProcess)

	mph.workers = NewWorkerPool(ctx, mph.config.Workers, mph.config.StepQueueSize, mph.log)
	mph.workers.Start()

	mph.startMessageWorkers()
	mph.sendStatus("Host started and accepting processes")

	status <- "started"
	return status, nil
}

func (mph *ProcessHost) finalizeProcess(pid pubsub.PID, result *runtime.Result) {
	if result.Error != nil {
		mph.log.Error("process execution failed",
			zap.String("pid", pid.String()),
			zap.Error(result.Error))
	} else {
		mph.log.Info("process execution completed",
			zap.String("pid", pid.String()),
			zap.Any("result", result.Payload))
	}
	mph.processes.Delete(pid)
	mph.msgHost.Detach(pid)
}

// startMessageWorkers spawns worker goroutines to process fallback messages.
func (mph *ProcessHost) startMessageWorkers() {
	for i := 0; i < mph.config.MessageWorkerCount; i++ {
		mph.msgWG.Add(1)

		go func() {
			defer mph.msgWG.Done()
			for {
				select {
				case m, ok := <-mph.hostMessages:
					if !ok {
						return
					}

					if pi, exists := mph.processes.Load(m.PID); exists {
						if err := pi.(*ProcessInfo).process.Send(m.Batch); err != nil {
							mph.log.Error("failed to send message to worker",
								zap.String("pid", m.PID.String()),
								zap.Error(err))
						}
					} else {
						mph.log.Warn("routing worker received message for unknown process",
							zap.String("pid", m.PID.String()))
					}
				case <-mph.ctx.Done():
					return
				}
			}
		}()
	}
}

// Launch starts a new process and sets up its fallback routing. It rejects new launches if shutdown is in progress.
func (mph *ProcessHost) Launch(ctx context.Context, launch *process.LaunchProcess) (pubsub.PID, error) {
	if mph.shutdown.Load() {
		return pubsub.PID{}, errors.New("host is shutting down, cannot launch new process")
	}
	if _, exists := mph.processes.Load(launch.PID); exists {
		return pubsub.PID{}, process.ErrHostBusy
	}
	if mph.ctx == nil {
		return pubsub.PID{}, process.ErrHostDead
	}

	if err := launch.Process.Start(mph.prepareContext(ctx, launch.PID), launch.PID, launch.Input); err != nil {
		return pubsub.PID{}, err
	}
	pi := &ProcessInfo{
		pid:     launch.PID,
		process: launch.Process,
	}
	mph.processes.Store(launch.PID, pi)

	fallbackCh := make(chan *pubsub.PIDBatch, mph.config.BufferSize)
	_, err := mph.msgHost.AttachWithPID(launch.PID, fallbackCh)
	if err != nil {
		mph.processes.Delete(launch.PID)
		return pubsub.PID{}, err
	}

	// Spawn a goroutine to forward fallback messages into the central routing channel.
	go func(pid pubsub.PID, ch chan *pubsub.PIDBatch) {
		for {
			select {
			case m, ok := <-ch:
				if !ok {
					mph.log.Debug("fallback channel closed", zap.String("pid", pid.String()))
					return
				}
				select {
				case mph.hostMessages <- m:
				case <-time.After(mph.config.RetryTimeout):
					mph.log.Warn("fallback routing queue is full", zap.String("pid", pid.String()))
				}
			}
		}
	}(launch.PID, fallbackCh)

	if err := mph.workers.Schedule(&WorkRequest{
		PID:    launch.PID,
		Runner: launch.Process,
	}); err != nil {
		mph.processes.Delete(launch.PID)
		return pubsub.PID{}, err
	}
	mph.log.Info("process launched", zap.String("pid", launch.PID.String()))
	return launch.PID, nil
}

func (mph *ProcessHost) prepareContext(ctx context.Context, pid pubsub.PID) context.Context {
	pCtx := process.MergeContext(contextApi.MergeContext(mph.ctx, ctx), ctx)
	pCtx = context.WithValue(pCtx, contextApi.IDCtx, pid.ID)

	pCtx = process.WithAddedOnComplete(pCtx, func(pid pubsub.PID, result *runtime.Result) {
		if result.Error != nil {
			mph.log.Error("terminal process execution failed",
				zap.String("pid", pid.String()),
				zap.Error(result.Error))
		} else {
			mph.log.Info("terminal process execution completed",
				zap.String("pid", pid.String()),
				zap.Any("result", result.Payload))
		}
		mph.processes.Delete(pid)
		mph.log.Debug("process evicted", zap.String("pid", pid.String()))
	})

	return pCtx
}

// Terminate stops a running process and detaches its fallback receiver.
func (mph *ProcessHost) Terminate(_ context.Context, pid pubsub.PID) error {
	mph.workers.Terminate(pid)
	mph.log.Info("process terminate requested", zap.String("pid", pid.String()))
	return nil
}

// Send forwards a message via the underlying msgHost, rejecting the call if shutdown is in progress.
func (mph *ProcessHost) Send(ctx context.Context, pid pubsub.PID, batch *pubsub.Batch) error {
	if mph.shutdown.Load() {
		return errors.New("host is shutting down, rejecting send")
	}
	return mph.msgHost.Send(ctx, pid, batch)
}

// Attach registers a receiver channel with the underlying msgHost, rejecting if shutdown is in progress.
func (mph *ProcessHost) Attach(pid pubsub.PID, ch chan *pubsub.Batch) (context.CancelFunc, error) {
	if mph.shutdown.Load() {
		return nil, errors.New("host is shutting down, rejecting attach")
	}
	return mph.msgHost.Attach(pid, ch)
}

// Stop gracefully shuts down the host by rejecting new operations, sending cancel notifications,
// and waiting until all running processes have exited.
func (mph *ProcessHost) Stop(ctx context.Context) error {
	mph.shutdown.Store(true)
	mph.workers.Stop()

	mph.sendStatus("Host shutting down: sending cancel notifications to running processes")
	cancelBatch := pubsub.NewBatch("cancel")
	mph.processes.Range(func(key, _ interface{}) bool {
		pid := key.(pubsub.PID)
		if err := mph.msgHost.Send(ctx, pid, cancelBatch); err != nil {
			mph.log.Warn("failed to send cancel", zap.String("pid", pid.String()), zap.Error(err))
		}
		return true
	})

	mph.msgWG.Wait()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		processCount := 0
		mph.processes.Range(func(_, _ interface{}) bool {
			processCount++
			return true
		})
		if processCount == 0 {
			close(mph.done)
			mph.sendStatus("Host shutdown complete")
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Continue waiting.
		}
	}
}
