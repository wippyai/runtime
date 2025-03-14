package host

import (
	"context"
	"errors"
	"fmt"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/service/host"
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
	cfg         *host.EntryConfig
	log         *zap.Logger
	msgHost     pubsub.Host
	msgQueues   []chan *pubsub.Package // Multiple queues for message routing, one per worker
	pool        ProcessPoolAPI
	ctx         context.Context
	done        chan struct{}
	msgWG       sync.WaitGroup
	running     atomic.Bool // true if host is running
	shutdown    atomic.Bool // shutdown flag: true if shutdown in progress.
	statusCh    chan any    // Optional external status notification channel.
	msgFactory  MessageHostFactory
	poolFactory ProcessPoolFactory
}

// NewMultiProcessHost creates a new Host instance with given factories.
func NewMultiProcessHost(
	id registry.ID,
	config *host.EntryConfig,
	log *zap.Logger,
	msgFactory MessageHostFactory,
	poolFactory ProcessPoolFactory,
) *Host {
	// Create one message queue per worker for balanced processing
	msgQueues := make([]chan *pubsub.Package, config.HostConfig.MessageWorkerCount)
	for i := 0; i < config.HostConfig.MessageWorkerCount; i++ {
		msgQueues[i] = make(chan *pubsub.Package, config.HostConfig.BufferSize)
	}

	return &Host{
		id:          id,
		cfg:         config,
		log:         log,
		msgQueues:   msgQueues,
		done:        make(chan struct{}),
		msgFactory:  msgFactory,
		poolFactory: poolFactory,
	}
}

// fnv1a32 is a very fast hash function for string inputs
// It's simple and provides good distribution
func fnv1a32(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// Attach registers a receiver channel with the underlying msgHost, rejecting if shutdown is in progress.
func (h *Host) Attach(pid pubsub.PID, ch chan *pubsub.Package) (context.CancelFunc, error) {
	if h.running.Load() == false {
		return nil, errors.New("host is not running, cannot launch new process")
	}

	if h.shutdown.Load() {
		return nil, errors.New("host is shutting down, rejecting attach")
	}

	return h.msgHost.Attach(pid, ch)
}

// Detach unregisters a receiver channel from the underlying msgHost, rejecting if shutdown is in progress.
func (h *Host) Detach(pid pubsub.PID) {
	if h.running.Load() == false {
		return
	}

	if h.shutdown.Load() {
		return
	}

	h.msgHost.Detach(pid)
}

// finalizeProcess handles cleanup when a process completes execution
func (h *Host) finalizeProcess(pid pubsub.PID, result *runtime.Result) {
	if result.Error != nil {
		h.log.Error("process execution failed",
			zap.String("pid", pid.String()),
			zap.Error(result.Error))
	} else {
		h.log.Debug("process execution completed",
			zap.String("pid", pid.String()))
	}

	h.msgHost.Detach(pid)
	h.pool.Remove(pid)
}

// Launch starts a new process and sets up its routing. It rejects new launches if shutdown is in progress.
func (h *Host) Launch(ctx context.Context, launch *process.Launch) (pubsub.PID, error) {
	if !h.running.Load() {
		return pubsub.PID{}, errors.New("host is not running, cannot launch new process")
	}

	if h.shutdown.Load() {
		return pubsub.PID{}, errors.New("host is shutting down, cannot launch new process")
	}

	if h.pool.Has(launch.PID) {
		return pubsub.PID{}, process.ErrHostBusy
	}

	if h.ctx == nil {
		return pubsub.PID{}, process.ErrHostDead
	}

	ctx = h.prepareContext(ctx, launch.PID, launch.Lifecycle)

	if err := launch.Process.Start(ctx, launch.PID, launch.Input); err != nil {
		return pubsub.PID{}, err
	}

	// Attach to message routing with shared channel
	_, err := h.msgHost.Attach(launch.PID, h.getQueueForPID(launch.PID))
	if err != nil {
		return pubsub.PID{}, err
	}

	if err := h.pool.Add(launch.PID, launch.Process); err != nil {
		h.msgHost.Detach(launch.PID)
		return pubsub.PID{}, err
	}

	h.log.Debug("process launched", zap.String("pid", launch.PID.String()))
	return launch.PID, nil
}

// getQueueForPID determines which message queue to use for a given PID
// This ensures messages from the same source are processed in order
func (h *Host) getQueueForPID(pid pubsub.PID) chan *pubsub.Package {
	hash := fnv1a32(pid.UniqID)
	index := int(hash % uint32(len(h.msgQueues)))
	return h.msgQueues[index]
}

// prepareContext sets up the context for a process
func (h *Host) prepareContext(ctx context.Context, pid pubsub.PID, lifecycle process.Lifecycle) context.Context {
	// security and other core keys
	pCtx := ctxapi.MergeContext(h.ctx, ctx)

	// global lifecycle
	pCtx = process.GetProcesses(ctx).AttachLifecycle(pCtx, lifecycle)

	// local lifecycle
	pCtx = process.WithAddedOnComplete(pCtx, h.finalizeProcess)
	pCtx = context.WithValue(pCtx, ctxapi.WakeUpKey, func() {
		_ = h.pool.Schedule(pid)
	})

	pCtx = logs.WithLogger(pCtx, h.log.With(zap.String("pid", pid.String())))

	return pCtx
}

// Send forwards a message via the underlying msgHost, rejecting if shutdown is in progress.
func (h *Host) Send(pkg *pubsub.Package) error {
	if h.running.Load() == false {
		return errors.New("host is not running, cannot launch new process")
	}

	if h.shutdown.Load() {
		return errors.New("host is shutting down, rejecting send")
	}

	return h.msgHost.Send(pkg)
}

// sendStatus sends a status message to the external status channel if available.
func (h *Host) sendStatus(message string) {
	select {
	case h.statusCh <- message:
	default:
		// Drop message if the channel is full.
	}
}

// Start initializes the Host, starts the worker pool and routing workers,
// and sends an external notification that the host is active.
func (h *Host) Start(ctx context.Context) (<-chan any, error) {
	h.statusCh = make(chan any, 1)

	// Set up the context
	h.ctx = pubsub.WithHost(ctx, h)

	// Create message host using the factory
	var err error
	h.msgHost, err = h.msgFactory.CreateMessageHost(h.ctx, h.cfg, h.log)
	if err != nil {
		return nil, fmt.Errorf("failed to create message host: %w", err)
	}

	// Create process pool using the factory
	h.pool, err = h.poolFactory.CreateProcessPool(
		h.ctx,
		h.cfg.HostConfig.Workers,
		h.cfg.HostConfig.MaxProcesses,
		h.log,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create process pool: %w", err)
	}

	h.pool.Start()
	h.startMessageWorkers()
	h.sendStatus("host started and accepting processes")

	h.running.Store(true)

	return h.statusCh, nil
}

// startMessageWorkers spawns worker goroutines to process routing messages.
func (h *Host) startMessageWorkers() {
	// Start one worker per message queue for load balancing
	for i := 0; i < len(h.msgQueues); i++ {
		h.msgWG.Add(1)
		queue := h.msgQueues[i]

		go func(workerID int, q chan *pubsub.Package) {
			defer h.msgWG.Done()

			h.log.Debug("starting message worker", zap.Int("workerID", workerID))

			for {
				select {
				case <-h.done:
					return
				case m, ok := <-q:
					if !ok {
						return
					}

					err := h.pool.Send(m.Target, m)
					if err != nil {
						h.log.Warn("failed to send message to process",
							zap.String("pid", m.Target.String()),
							zap.Error(err))
						continue
					}
				case <-h.ctx.Done():
					return
				}
			}
		}(i, queue)
	}
}

// Stop gracefully shuts down the host by rejecting new operations and waiting for processes to complete.
func (h *Host) Stop(ctx context.Context) error {
	if h.running.Load() == false {
		return errors.New("host is not running, cannot stop")
	}

	h.shutdown.Store(true)

	h.sendStatus("host shutting down")
	if err := h.pool.CancelAll(ctx, time.Now().Add(h.cfg.Lifecycle.StopTimeout)); err != nil {
		h.log.Error("error waiting for processes to stop", zap.Error(err))
		return err
	}

	h.pool.Close()
	close(h.done)

	// Close all message queues
	for _, q := range h.msgQueues {
		close(q)
	}

	h.msgWG.Wait()
	h.sendStatus("host shutdown complete")
	close(h.statusCh)
	h.running.Store(false)

	return nil
}

// Terminate stops a running process and detaches its routing.
func (h *Host) Terminate(ctx context.Context, pid pubsub.PID) error {
	if h.running.Load() == false {
		return errors.New("host is not running, cannot launch new process")
	}

	if !h.pool.Has(pid) {
		return process.ErrNoProcess
	}

	// terminate is aggressive, so we don't wait for the process to finish, use cancel signals instead
	h.pool.Terminate(pid)
	h.pool.Remove(pid)

	h.log.Debug("process terminate requested", zap.String("pid", pid.String()))
	return nil
}
