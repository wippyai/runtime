package host

import (
	"context"
	"errors"
	contextApi "github.com/ponyruntime/pony/api/context"
	"sync"
	"sync/atomic"

	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"go.uber.org/zap"
)

// Host composes an internal pubsub.Host to manage process launching,
// routing, and graceful shutdown. It uses an external status channel for notifications.
type Host struct {
	id          registry.ID
	config      process.HostConfig
	log         *zap.Logger
	makeMsgHost func(ctx context.Context) pubsub.BatchHost
	msgHost     pubsub.BatchHost
	msgCh       chan *pubsub.PIDBatch // Single channel for all message routing
	pool        *ProcessPool
	ctx         context.Context
	done        chan struct{}
	msgWG       sync.WaitGroup
	shutdown    atomic.Bool // shutdown flag: true if shutdown in progress.
	statusChat  chan string // Optional external status notification channel.
}

// NewProcessHost creates a new Host instance.
func NewProcessHost(
	id registry.ID,
	config process.HostConfig,
	log *zap.Logger,
	msgHost func(context.Context) pubsub.BatchHost,
) *Host {
	return &Host{
		id:          id,
		config:      config,
		log:         log,
		makeMsgHost: msgHost,
		msgCh:       make(chan *pubsub.PIDBatch, config.BufferSize),
		done:        make(chan struct{}),
	}
}

// sendStatus sends a status message to the external status channel if available.
func (mph *Host) sendStatus(message string) {
	if mph.statusChat != nil {
		select {
		case mph.statusChat <- message:
		default:
			// Drop message if the channel is full.
		}
	}
}

// Start initializes the Host, starts the worker pool and routing workers,
// and sends an external notification that the host is active.
func (mph *Host) Start(ctx context.Context) (<-chan any, error) {
	status := make(chan any, 1)

	// Wrap the incoming context with an on-complete callback.
	mph.ctx = process.WithAddedOnComplete(ctx, mph.finalizeProcess)

	mph.msgHost = mph.makeMsgHost(ctx)

	mph.pool = NewProcessPool(ctx, mph.config.Workers, mph.config.StepQueueSize, mph.log)
	mph.pool.Start()

	mph.startMessageWorkers()
	mph.sendStatus("Host started and accepting processes")

	status <- "started"
	return status, nil
}

func (mph *Host) finalizeProcess(pid pubsub.PID, result *runtime.Result) {
	if result.Error != nil {
		mph.log.Error("process execution failed",
			zap.String("pid", pid.String()),
			zap.Error(result.Error))
	} else {
		mph.log.Info("process execution completed",
			zap.String("pid", pid.String()),
			zap.Any("result", result.Payload))
	}
	mph.msgHost.Detach(pid)
}

// startMessageWorkers spawns worker goroutines to process routing messages.
func (mph *Host) startMessageWorkers() {
	for i := 0; i < mph.config.MessageWorkerCount; i++ {
		mph.msgWG.Add(1)

		go func() {
			defer mph.msgWG.Done()
			for {
				select {
				case m, ok := <-mph.msgCh:
					if !ok {
						return
					}

					if !mph.pool.HasProcess(m.PID) {
						mph.log.Warn("routing worker received message for unknown process",
							zap.String("pid", m.PID.String()))
						continue
					}

					entryVal, _ := mph.pool.processes.Load(m.PID)
					entry := entryVal.(*processEntry)
					if err := entry.process.Send(m.Batch); err != nil {
						mph.log.Error("failed to send message to process",
							zap.String("pid", m.PID.String()),
							zap.Error(err))
					}
				case <-mph.ctx.Done():
					return
				}
			}
		}()
	}
}

// Launch starts a new process and sets up its routing. It rejects new launches if shutdown is in progress.
func (mph *Host) Launch(ctx context.Context, launch *process.LaunchProcess) (pubsub.PID, error) {
	if mph.shutdown.Load() {
		return pubsub.PID{}, errors.New("host is shutting down, cannot launch new process")
	}

	if mph.pool.HasProcess(launch.PID) {
		return pubsub.PID{}, process.ErrHostBusy
	}

	if mph.ctx == nil {
		return pubsub.PID{}, process.ErrHostDead
	}

	if err := launch.Process.Start(mph.prepareContext(ctx, launch.PID), launch.PID, launch.Input); err != nil {
		return pubsub.PID{}, err
	}

	// Attach to message routing with shared channel
	_, err := mph.msgHost.AttachWithPID(launch.PID, mph.msgCh)
	if err != nil {
		return pubsub.PID{}, err
	}

	if err := mph.pool.AddProcess(launch.PID, launch.Process); err != nil {
		mph.msgHost.Detach(launch.PID)
		return pubsub.PID{}, err
	}

	mph.log.Info("process launched", zap.String("pid", launch.PID.String()))
	return launch.PID, nil
}

func (mph *Host) prepareContext(ctx context.Context, pid pubsub.PID) context.Context {
	pCtx := process.MergeContext(contextApi.MergeContext(mph.ctx, ctx), ctx)
	pCtx = context.WithValue(pCtx, contextApi.IDCtx, pid.ID)
	pCtx = context.WithValue(pCtx, contextApi.WakeUpKey, func() {
		err := mph.pool.Schedule(pid)
		if err != nil {
			mph.log.Error("failed to wake up process",
				zap.String("pid", pid.String()),
				zap.Error(err))
		}
	})

	return pCtx
}

// Terminate stops a running process and detaches its routing.
func (mph *Host) Terminate(ctx context.Context, pid pubsub.PID) error {
	if !mph.pool.HasProcess(pid) {
		return process.ErrNoProcess
	}

	if err := mph.pool.CancelProcess(pid); err != nil {
		mph.log.Error("failed to cancel process",
			zap.String("pid", pid.String()),
			zap.Error(err))
		return err
	}

	mph.log.Info("process terminate requested", zap.String("pid", pid.String()))
	return nil
}

// Send forwards a message via the underlying msgHost, rejecting if shutdown is in progress.
func (mph *Host) Send(ctx context.Context, pid pubsub.PID, batch *pubsub.Batch) error {
	if mph.shutdown.Load() {
		return errors.New("host is shutting down, rejecting send")
	}
	return mph.msgHost.Send(ctx, pid, batch)
}

// Attach registers a receiver channel with the underlying msgHost, rejecting if shutdown is in progress.
func (mph *Host) Attach(pid pubsub.PID, ch chan *pubsub.Batch) (context.CancelFunc, error) {
	if mph.shutdown.Load() {
		return nil, errors.New("host is shutting down, rejecting attach")
	}
	return mph.msgHost.Attach(pid, ch)
}

// Stop gracefully shuts down the host by rejecting new operations and waiting for processes to complete.
func (mph *Host) Stop(ctx context.Context) error {
	mph.shutdown.Store(true)

	mph.sendStatus("host shutting down")
	if err := mph.pool.CancelAll(ctx); err != nil {
		mph.log.Error("error waiting for processes to stop", zap.Error(err))
		return err
	}

	mph.msgWG.Wait()
	mph.pool.Close()
	close(mph.done)

	mph.sendStatus("Host shutdown complete")
	return nil
}
