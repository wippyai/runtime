package host

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/event"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/service/temporal"
	"go.uber.org/zap"
)

type Manager struct {
	ctx          context.Context
	bus          event.Bus
	resources    resource.Registry
	logger       *zap.Logger
	hosts        map[relay.HostID]*TemporalHost
	workerToHost map[registry.ID]relay.HostID
}

func NewManager(ctx context.Context, bus event.Bus, resources resource.Registry, logger *zap.Logger) *Manager {
	return &Manager{
		ctx:          ctx,
		bus:          bus,
		resources:    resources,
		logger:       logger,
		hosts:        make(map[relay.HostID]*TemporalHost),
		workerToHost: make(map[registry.ID]relay.HostID),
	}
}

func (m *Manager) Start(ctx context.Context) error {
	ch := make(chan event.Event, 100)
	_, err := m.bus.Subscribe(m.ctx, temporal.SystemTemporalTaskQueue, ch)
	if err != nil {
		return fmt.Errorf("failed to subscribe to task queue events: %w", err)
	}

	go m.handleEvents(ch)
	return nil
}

func (m *Manager) handleEvents(ch chan event.Event) {
	for {
		select {
		case <-m.ctx.Done():
			return
		case evt := <-ch:
			switch evt.Kind {
			case temporal.TaskQueueRegister:
				reg := evt.Data.(*temporal.TaskQueueRegistration)
				if err := m.registerTaskQueue(reg); err != nil {
					m.logger.Error("failed to register task queue", zap.Error(err))
				}
			case temporal.WorkflowRegister:
				reg := evt.Data.(*temporal.WorkflowRegistration)
				if err := m.registerWorkflow(reg); err != nil {
					m.logger.Error("failed to register workflow", zap.Error(err))
				}
			}
		}
	}
}

func (m *Manager) registerTaskQueue(reg *temporal.TaskQueueRegistration) error {
	hostID := relay.HostID(fmt.Sprintf("temporal:%s", reg.TaskQueue))

	m.logger.Info("registering task queue",
		zap.String("worker_id", reg.ID.String()),
		zap.String("task_queue", reg.TaskQueue),
		zap.String("host_id", string(hostID)))

	res, err := m.resources.Acquire(m.ctx, reg.Client, resource.ModeNormal)
	if err != nil {
		return fmt.Errorf("failed to acquire client: %w", err)
	}

	clientRes, err := res.Get()
	if err != nil {
		res.Release()
		return fmt.Errorf("failed to get client: %w", err)
	}

	cr, ok := clientRes.(temporal.ClientResource)
	if !ok {
		res.Release()
		return fmt.Errorf("unexpected client type: %T", clientRes)
	}

	host := NewTemporalHost(hostID, cr.GetTaskQueueName(reg.TaskQueue), cr.Client)
	m.hosts[hostID] = host
	m.workerToHost[reg.ID] = hostID

	m.logger.Info("registering temporal host",
		zap.String("host_id", string(hostID)),
		zap.String("worker_id", reg.ID.String()))

	// Register with relay for message routing
	m.bus.Send(m.ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostRegister,
		Path:   string(hostID),
		Data:   relay.Host(host),
	})

	// Register as process host for spawning
	m.bus.Send(m.ctx, event.Event{
		System: processapi.HostSystem,
		Kind:   processapi.HostRegister,
		Path:   string(hostID),
		Data:   processapi.Managed(host),
	})

	m.logger.Info("temporal host registered",
		zap.String("host_id", string(hostID)),
		zap.String("worker_id", reg.ID.String()),
		zap.String("task_queue", reg.TaskQueue))
	return nil
}

func (m *Manager) registerWorkflow(reg *temporal.WorkflowRegistration) error {
	hostID, ok := m.workerToHost[reg.TaskQueue]
	if !ok {
		return fmt.Errorf("worker %s not found (task queue not registered)", reg.TaskQueue.String())
	}

	host, ok := m.hosts[hostID]
	if !ok {
		return fmt.Errorf("host %s not found", hostID)
	}

	host.RegisterWorkflow(reg)

	m.logger.Info("registered workflow with host",
		zap.String("workflow", reg.Name),
		zap.String("worker", reg.TaskQueue.String()),
		zap.String("host_id", string(hostID)))
	return nil
}
