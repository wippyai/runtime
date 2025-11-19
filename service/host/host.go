package host

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/process/stats"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/service/host"
	"go.uber.org/zap"
)

// Host composes an internal relay.Host to manage process launching,
// routing, and graceful shutdown. It uses an external status channel for notifications.
type Host struct {
	id          registry.ID
	cfg         *host.EntryConfig
	log         *zap.Logger
	msgHost     relay.Host
	msgQueues   []chan *relay.Package // Multiple queues for message routing, one per worker
	systemQueue chan *relay.Package   // Dedicated queue for @system control messages
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
	msgQueues := make([]chan *relay.Package, config.HostConfig.MessageWorkerCount)
	for i := 0; i < config.HostConfig.MessageWorkerCount; i++ {
		msgQueues[i] = make(chan *relay.Package, config.HostConfig.BufferSize)
	}

	return &Host{
		id:          id,
		cfg:         config,
		log:         log,
		msgQueues:   msgQueues,
		systemQueue: make(chan *relay.Package, 16),
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
func (h *Host) Attach(pid relay.PID, ch chan *relay.Package) (context.CancelFunc, error) {
	if !h.running.Load() {
		return nil, errors.New("host is not running, cannot launch new process")
	}

	if h.shutdown.Load() {
		return nil, errors.New("host is shutting down, rejecting attach")
	}

	return h.msgHost.Attach(pid, ch)
}

// Detach unregisters a receiver channel from the underlying msgHost, rejecting if shutdown is in progress.
func (h *Host) Detach(pid relay.PID) {
	if !h.running.Load() {
		return
	}

	if h.shutdown.Load() {
		return
	}

	h.msgHost.Detach(pid)
}

// finalizeProcess handles cleanup when a process completes execution
func (h *Host) finalizeProcess(_ context.Context, pid relay.PID, result *runtime.Result) {
	if result.Error != nil {
		h.log.Error("process execution failed",
			zap.String("pid", pid.String()),
			zap.String("error", result.Error.Error()))
	} else {
		h.log.Debug("process execution completed",
			zap.String("pid", pid.String()))
	}

	h.msgHost.Detach(pid)
	h.pool.Remove(pid)
}

// Launch starts a new process and sets up its routing. It rejects new launches if shutdown is in progress.
func (h *Host) Launch(ctx context.Context, launch *process.Launch) (relay.PID, error) {
	if !h.running.Load() {
		return relay.PID{}, errors.New("host is not running, cannot launch new process")
	}

	if h.shutdown.Load() {
		return relay.PID{}, errors.New("host is shutting down, cannot launch new process")
	}

	if h.pool.Has(launch.PID) {
		return relay.PID{}, process.ErrHostBusy
	}

	if h.ctx == nil {
		return relay.PID{}, process.ErrHostDead
	}

	// Use Host's context, not the calling context - processes live with the Host, not the caller
	ctx = h.prepareContext(ctx, launch)

	if err := launch.Process.Start(ctx, launch.PID, launch.Input); err != nil {
		return relay.PID{}, err
	}

	// Attach to message routing with shared channel
	_, err := h.msgHost.Attach(launch.PID, h.getQueueForPID(launch.PID))
	if err != nil {
		launch.Process.Terminate()
		return relay.PID{}, err
	}

	if err := h.pool.Add(ctx, launch); err != nil {
		launch.Process.Terminate()
		h.msgHost.Detach(launch.PID)
		return relay.PID{}, err
	}

	h.log.Debug("process launched",
		zap.String("pid", launch.PID.String()),
		zap.String("id", launch.Source.String()))
	return launch.PID, nil
}

// getQueueForPID determines which message queue to use for a given PID
// This ensures messages from the same source are processed in order
func (h *Host) getQueueForPID(pid relay.PID) chan *relay.Package {
	hash := fnv1a32(pid.UniqID)
	index := int(hash) % len(h.msgQueues)
	return h.msgQueues[index]
}

// prepareContext sets up the context for a process
func (h *Host) prepareContext(callCtx context.Context, launch *process.Launch) context.Context {
	// Create FrameContext on Host's context, inheriting actor/scope from calling context
	pCtx, fc := ctxapi.OpenFrameContextOn(h.ctx, callCtx)

	// Store frame metadata and apply launch context overrides
	pairsLen := 3 + len(launch.Context)
	pairs := make([]ctxapi.Pair, pairsLen)
	pairs[0] = ctxapi.Pair{Key: runtime.FrameIDKey, Value: launch.Source}
	pairs[1] = ctxapi.Pair{Key: runtime.FramePIDKey, Value: launch.PID}
	pairs[2] = ctxapi.Pair{Key: runtime.FrameHostKey, Value: h.id}

	// Add launch context overrides (actor, scope, custom values, etc.)
	if len(launch.Context) > 0 {
		copy(pairs[3:], launch.Context)
	}

	if err := fc.SetMultiple(pairs...); err != nil {
		h.log.Error("failed to set frame context", zap.Error(err))
	}

	// Store lifecycle hook arrays from Launch
	if len(launch.OnStart) > 0 {
		if err := process.SetOnStartHooks(pCtx, launch.OnStart); err != nil {
			h.log.Error("failed to set onStart hooks", zap.Error(err))
		}
	}

	// Add host's finalizeProcess to OnComplete hooks
	onCompleteHooks := launch.OnComplete
	onCompleteHooks = append(onCompleteHooks, h.finalizeProcess)
	if err := process.SetOnCompleteHooks(pCtx, onCompleteHooks); err != nil {
		h.log.Error("failed to set onComplete hooks", zap.Error(err))
	}

	if err := ctxapi.SetWakeUp(pCtx, func() {
		_ = h.pool.Schedule(launch.PID)
	}); err != nil {
		h.log.Error("failed to set wakeup callback", zap.Error(err))
	}

	return pCtx
}

// Send forwards a message via the underlying msgHost, rejecting if shutdown is in progress.
func (h *Host) Send(pkg *relay.Package) error {
	if !h.running.Load() {
		return errors.New("host is not running, cannot send message")
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

	// Set up the Host's context - processes fork from this, not the calling context
	h.ctx = relay.WithHost(ctx, h)

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

	// Register @system endpoint for host control messages
	systemPID := relay.PID{UniqID: stats.SystemEndpoint}
	if _, err := h.msgHost.Attach(systemPID, h.systemQueue); err != nil {
		return nil, fmt.Errorf("failed to attach @system endpoint: %w", err)
	}

	h.pool.Start()
	h.startMessageWorkers(ctx)
	h.startSystemWorker(ctx)
	h.sendStatus("host started and accepting processes")

	h.running.Store(true)

	return h.statusCh, nil
}

// startMessageWorkers spawns worker goroutines to process routing messages.
func (h *Host) startMessageWorkers(ctx context.Context) {
	// Serve one worker per message queue for load balancing
	for i := 0; i < len(h.msgQueues); i++ {
		h.msgWG.Add(1)
		queue := h.msgQueues[i]

		go func(_ int, q chan *relay.Package) {
			defer h.msgWG.Done()

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
				case <-ctx.Done():
					return
				}
			}
		}(i, queue)
	}
}

// startSystemWorker spawns a dedicated worker to handle @system control messages.
func (h *Host) startSystemWorker(ctx context.Context) {
	h.msgWG.Add(1)

	go func() {
		defer h.msgWG.Done()

		for {
			select {
			case <-h.done:
				return
			case <-ctx.Done():
				return
			case pkg, ok := <-h.systemQueue:
				if !ok {
					return
				}

				h.handleSystemMessage(pkg)
			}
		}
	}()
}

// Collect implements stats.Provider interface.
func (h *Host) Collect(ctx context.Context) (*stats.Snapshot, error) {
	enabled, sampleRate, entries, err := h.pool.Collect(ctx)
	if err != nil {
		return nil, err
	}

	snapshot := &stats.Snapshot{
		HostID:     h.id.String(),
		Timestamp:  time.Now(),
		Enabled:    enabled,
		SampleRate: sampleRate,
		Processes:  entries,
	}

	return snapshot, nil
}

// handleSystemMessage processes control messages sent to the @system endpoint.
func (h *Host) handleSystemMessage(pkg *relay.Package) {
	for _, msg := range pkg.Messages {
		switch msg.Topic {
		case stats.TopicStatsEnable:
			var sampleRate int64 = 100
			if len(msg.Payloads) > 0 {
				if rate, ok := msg.Payloads[0].Data().(int64); ok && rate > 0 {
					sampleRate = rate
				}
			}
			h.pool.EnableStats(sampleRate)
			h.log.Debug("stats collection enabled", zap.Int64("sample_rate", sampleRate))

		case stats.TopicStatsDisable:
			h.pool.DisableStats()
			h.log.Debug("stats collection disabled")

		case stats.TopicStatsCollect:
			snapshot, err := h.Collect(h.ctx)
			if err != nil {
				h.log.Error("failed to collect stats", zap.Error(err))
				continue
			}

			// Send snapshot back to caller
			if err := h.msgHost.Send(relay.NewPackage(
				pkg.Target,
				pkg.Source,
				msg.Topic,
				payload.New(snapshot),
			)); err != nil {
				h.log.Error("failed to send stats snapshot",
					zap.String("to", pkg.Source.String()),
					zap.Error(err))
			}

		default:
			h.log.Warn("unknown system topic", zap.String("topic", msg.Topic))
		}
	}
}

// Stop gracefully shuts down the host by rejecting new operations and waiting for processes to complete.
func (h *Host) Stop(ctx context.Context) error {
	if !h.running.Load() {
		return errors.New("host is not running, cannot stop")
	}

	h.shutdown.Store(true)

	h.sendStatus("host shutting down")
	if err := h.pool.CancelAll(ctx, time.Now().Add(h.cfg.Lifecycle.StopTimeout)); err != nil {
		h.log.Error("error waiting for processes to stop", zap.Error(err))
		return err
	}

	h.pool.Close()

	// Detach @system endpoint
	systemPID := relay.PID{UniqID: stats.SystemEndpoint}
	h.msgHost.Detach(systemPID)

	close(h.done)

	// Close all message queues
	for _, q := range h.msgQueues {
		close(q)
	}
	close(h.systemQueue)

	h.msgWG.Wait()
	h.sendStatus("host shutdown complete")
	close(h.statusCh)
	h.running.Store(false)

	return nil
}

// Terminate stops a running process and detaches its routing.
func (h *Host) Terminate(_ context.Context, pid relay.PID) error {
	if !h.running.Load() {
		return errors.New("host is not running, cannot terminate process")
	}

	if !h.pool.Has(pid) {
		return process.ErrNoProcess
	}

	// terminate is aggressive, so we don't wait for the process to finish, use cancel signals instead
	h.pool.Terminate(pid)
	h.msgHost.Detach(pid)
	h.pool.Remove(pid)

	h.log.Debug("process terminate requested", zap.String("pid", pid.String()))
	return nil
}
