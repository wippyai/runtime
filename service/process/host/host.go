package host

import (
	"context"
	pubsubImpl "github.com/ponyruntime/pony/system/pubsub"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

type command interface{}

type startCommand struct {
	launch *process.LaunchProcess
}

type stopCommand struct {
	pid pubsub.PID
}

type stepWork struct {
	pid pubsub.PID
}

type message struct {
	pid   pubsub.PID
	batch *pubsub.Batch
}

type processInstance struct {
	pid     pubsub.PID
	process process.Process
	msgCh   chan *pubsub.Batch
}

type Host struct {
	id     registry.ID
	config process.HostConfig
	log    *zap.Logger

	processes sync.Map // map[pubsub.PID]*processInstance
	msgs      pubsub.Host

	commandCh chan command
	stepCh    chan stepWork
	messageCh chan message

	//workers *workerPool

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func NewHost(id registry.ID, config process.HostConfig, log *zap.Logger) *Host {
	ctx, cancel := context.WithCancel(context.Background())

	h := &Host{
		id:        id,
		config:    config,
		log:       log,
		commandCh: make(chan command, config.ProcessQueueSize),
		stepCh:    make(chan stepWork, config.StepQueueSize),
		messageCh: make(chan message, config.ProcessQueueSize),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
	}

	//h.workers = newWorkerPool(config.Workers, h.stepCh, log)

	return h
}

func (h *Host) Start(ctx context.Context) (<-chan any, error) {
	status := make(chan any, 1)
	h.msgs = pubsubImpl.NewHost(ctx, pubsubImpl.HostConfig{
		BufferSize:      0,
		WorkerCount:     0,
		Logger:          nil,
		RetryTimeout:    0,
		DeliveryTimeout: 0,
	})

	go h.run(status)
	return status, nil
}

func (h *Host) Stop(ctx context.Context) error {
	h.cancel()
	select {
	case <-h.done:
		return nil
	case <-time.After(h.config.ShutdownTimeout):
		return context.DeadlineExceeded
	}
}

func (h *Host) UpdateConfig(ctx context.Context, cfg process.HostConfig) error {
	// Only update non-critical configuration
	h.config = cfg
	return nil
}

func (h *Host) run(status chan<- any) {
	defer close(h.done)
	defer close(status)
	defer h.cleanup()

	// Signal we're ready
	status <- "started"

	// Start worker pool
	//	h.workers.start(h.ctx)

	for {
		select {
		case <-h.ctx.Done():
			return

		case cmd := <-h.commandCh:
			switch c := cmd.(type) {
			case startCommand:
				h.handleStart(c)
			case stopCommand:
				h.handleStop(c)
			}

		case msg := <-h.messageCh:
			h.handleMessage(msg)
		}
	}
}

func (h *Host) cleanup() {
	// Stop worker pool
	//	h.workers.stop()

	// Stop all processes
	h.processes.Range(func(key, value interface{}) bool {
		proc := value.(*processInstance)
		close(proc.msgCh)
		h.processes.Delete(key)
		return true
	})
}

func (h *Host) handleStart(cmd startCommand) {
	launch := cmd.launch
	proc := launch.Process

	instance := &processInstance{
		pid:     launch.PID,
		process: proc,
		msgCh:   make(chan *pubsub.Batch, h.config.MessageBuffer),
	}

	h.processes.Store(launch.PID, instance)

	// Start message handler for this process
	go h.handleProcessMessages(instance)

	// Schedule first step
	h.stepCh <- stepWork{pid: launch.PID}
}

func (h *Host) handleStop(cmd stopCommand) {
	if instance, ok := h.processes.LoadAndDelete(cmd.pid); ok {
		proc := instance.(*processInstance)
		close(proc.msgCh)
	}
}

func (h *Host) handleMessage(msg message) {
	if instance, ok := h.processes.Load(msg.pid); ok {
		proc := instance.(*processInstance)
		select {
		case proc.msgCh <- msg.batch:
		default:
			h.log.Warn("message buffer full, dropping message",
				zap.String("pid", msg.pid.String()))
		}
	}
}

func (h *Host) handleProcessMessages(proc *processInstance) {
	for msg := range proc.msgCh {
		if err := proc.process.Send(msg); err != nil {
			h.log.Error("failed to send message to process",
				zap.String("pid", proc.pid.String()),
				zap.Error(err))
		}

		// Schedule next step
		h.stepCh <- stepWork{pid: proc.pid}
	}
}

// Implements process.Managed interface
func (h *Host) Launch(ctx context.Context, launch *process.LaunchProcess) (pubsub.PID, error) {
	// Start process
	if err := launch.Process.Start(ctx, launch.PID, launch.Input); err != nil {
		return pubsub.PID{}, err
	}

	select {
	case h.commandCh <- startCommand{launch: launch}:
		return launch.PID, nil
	case <-ctx.Done():
		return pubsub.PID{}, ctx.Err()
	}
}

func (h *Host) Terminate(ctx context.Context, pid pubsub.PID) error {
	select {
	case h.commandCh <- stopCommand{pid: pid}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return process.ErrHostBusy
	}
}

func (h *Host) Send(ctx context.Context, pid pubsub.PID, batch *pubsub.Batch) error {
	return h.msgs.Send(ctx, pid, batch)
}

func (h *Host) Attach(pid pubsub.PID, ch chan *pubsub.Batch) (error, context.CancelFunc) {
	return h.msgs.Attach(pid, ch)
}
