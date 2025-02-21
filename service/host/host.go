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
	"go.uber.org/zap"
)

// Host composes an internal pubsub.Host to manage process launching,
// routing, and graceful shutdown. It uses an external status channel for notifications.
type Host struct {
	id          registry.ID
	cfg         *process.EntryConfig
	log         *zap.Logger
	makeMsgHost func(ctx context.Context) pubsub.BatchHost
	msgHost     pubsub.BatchHost
	msgCh       chan *pubsub.PIDBatch // Single channel for all message routing
	pool        *ProcessPool
	ctx         context.Context
	done        chan struct{}
	msgWG       sync.WaitGroup
	shutdown    atomic.Bool // shutdown flag: true if shutdown in progress.
	statusCh    chan any    // Optional external status notification channel.
}

// NewProcessHost creates a new Host instance.
func NewProcessHost(
	id registry.ID,
	config *process.EntryConfig,
	log *zap.Logger,
	msgHost func(context.Context) pubsub.BatchHost,
) *Host {
	return &Host{
		id:          id,
		cfg:         config,
		log:         log,
		makeMsgHost: msgHost,
		msgCh:       make(chan *pubsub.PIDBatch, config.HostConfig.BufferSize),
		done:        make(chan struct{}),
	}
}

// sendStatus sends a status message to the external status channel if available.
func (mph *Host) sendStatus(message string) {
	select {
	case mph.statusCh <- message:
	default:
		// Drop message if the channel is full.
	}
}

// Start initializes the Host, starts the worker pool and routing workers,
// and sends an external notification that the host is active.
func (mph *Host) Start(ctx context.Context) (<-chan any, error) {
	mph.statusCh = make(chan any, 1)

	// Wrap the incoming context with an on-complete callback.
	mph.msgHost = mph.makeMsgHost(ctx)

	mph.ctx = context.WithValue(ctx, contextApi.HostCtx, mph)

	mph.pool = NewProcessPool(ctx, mph.cfg.HostConfig.Workers, mph.cfg.HostConfig.StepQueueSize, mph.log)
	mph.pool.Start()

	mph.startMessageWorkers()
	mph.sendStatus("host started and accepting processes")

	return mph.statusCh, nil
}

func (mph *Host) finalizeProcess(pid pubsub.PID, result *runtime.Result) {
	if result.Error != nil {
		mph.log.Error("process execution failed",
			zap.String("pid", pid.String()),
			zap.Error(result.Error))
	} else {
		mph.log.Debug("process execution completed",
			zap.String("pid", pid.String()))
	}

	mph.msgHost.Detach(pid)
	mph.pool.RemoveProcess(pid)
}

// startMessageWorkers spawns worker goroutines to process routing messages.
func (mph *Host) startMessageWorkers() {
	for i := 0; i < mph.cfg.HostConfig.MessageWorkerCount; i++ {
		mph.msgWG.Add(1)

		go func() {
			defer mph.msgWG.Done()
			for {
				select {
				case <-mph.done:
					return
				case m, ok := <-mph.msgCh:
					if !ok {
						return
					}

					entryVal, ok := mph.pool.processes.Load(m.PID)
					if !ok {
						mph.log.Warn("routing worker received message for unknown process",
							zap.String("pid", m.PID.String()))
						continue
					}

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

	mph.log.Debug("process launched", zap.String("pid", launch.PID.String()))
	return launch.PID, nil
}

func (mph *Host) prepareContext(ctx context.Context, pid pubsub.PID) context.Context {
	// security and other core keys
	pCtx := contextApi.MergeContext(mph.ctx, ctx)

	// lifecycle
	pCtx = process.GetTopology(pCtx).AttachToContext(pCtx)
	pCtx = process.WithAddedOnComplete(pCtx, mph.finalizeProcess)

	pCtx = context.WithValue(pCtx, contextApi.IDCtx, pid.ID)
	pCtx = context.WithValue(pCtx, contextApi.WakeUpKey, func() {
		_ = mph.pool.Schedule(pid) // it's ok since it means process no longer found, possible during termination
	})

	return pCtx
}

// Terminate stops a running process and detaches its routing.
func (mph *Host) Terminate(ctx context.Context, pid pubsub.PID) error {
	if !mph.pool.HasProcess(pid) {
		return process.ErrNoProcess
	}

	// terminate is aggressive, so we don't wait for the process to finish, use cancel signals instead
	mph.pool.RemoveProcess(pid)

	mph.log.Debug("process terminate requested", zap.String("pid", pid.String()))
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
	if err := mph.pool.CancelAll(ctx, time.Now().Add(mph.cfg.Lifecycle.StopTimeout)); err != nil {
		mph.log.Error("error waiting for processes to stop", zap.Error(err))
		return err
	}

	mph.pool.Close()
	close(mph.done)
	mph.msgWG.Wait()
	mph.sendStatus("host shutdown complete")
	close(mph.statusCh)

	return nil
}
