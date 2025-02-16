package pubsub

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	api "github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// Manager implements node management by composing over the Node type
type Manager struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        events.Bus
	node       *Node
	subscriber *eventbus.Subscriber
}

// NewNodeManager creates a new node manager instance that wraps a Node
func NewNodeManager(node *Node, bus events.Bus, logger *zap.Logger) *Manager {
	return &Manager{
		node:   node,
		bus:    bus,
		logger: logger,
	}
}

// Start initializes the manager and begins listening for host registration events
func (m *Manager) Start(ctx context.Context) error {
	m.ctx = ctx

	// Subscribe to host registration events
	sub, err := eventbus.NewSubscriber(
		ctx,
		m.bus,
		api.System,
		"node.(register_host|remove_host)",
		m.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	m.subscriber = sub

	return nil
}

// Stop gracefully shuts down the manager
func (m *Manager) Stop() error {
	if m.subscriber != nil {
		m.subscriber.Close()
	}
	return nil
}

func (m *Manager) handleEvent(e events.Event) {
	switch e.Kind {
	case api.RegisterHost:
		m.handleRegisterHost(e)
	case api.DeleteHost:
		m.handleDeleteHost(e)
	default:
		m.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path),
			zap.String("node_id", m.node.nodeID),
		)
	}
}

func (m *Manager) handleRegisterHost(e events.Event) {
	host, ok := e.Data.(api.Host)
	if !ok {
		m.logger.Error("invalid host payload",
			zap.String("host", e.Path),
			zap.String("node_id", m.node.nodeID),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		m.sendReject(e.Path, "invalid host payload")
		return
	}

	// Register with the underlying node
	err := m.node.RegisterHost(e.Path, host)
	if err != nil {
		m.logger.Error("failed to register host",
			zap.String("host", e.Path),
			zap.String("node_id", m.node.nodeID),
			zap.Error(err))

		m.sendReject(e.Path, err.Error())
		return
	}

	m.logger.Info("host registered successfully",
		zap.String("host", e.Path),
		zap.String("node_id", m.node.nodeID),
		zap.String("type", fmt.Sprintf("%T", host)),
	)
	m.sendAccept(e.Path)
}

func (m *Manager) handleDeleteHost(e events.Event) {
	// Unregister from the underlying node
	m.node.UnregisterHost(e.Path)

	m.logger.Info("host unregistered successfully",
		zap.String("host", e.Path),
		zap.String("node_id", m.node.nodeID),
	)
	m.sendAccept(e.Path)
}

func (m *Manager) sendAccept(path events.Path) {
	m.bus.Send(m.ctx, events.Event{
		System: api.System,
		Kind:   api.AcceptHost,
		Path:   path,
	})
}

func (m *Manager) sendReject(path events.Path, reason string) {
	m.bus.Send(m.ctx, events.Event{
		System: api.System,
		Kind:   api.RejectHost,
		Path:   path,
		Data:   reason,
	})
}

// Send delegates message sending to the underlying node
func (m *Manager) Send(ctx context.Context, pid api.PID, batch *api.Batch) error {
	return m.node.Send(ctx, pid, batch)
}

// Attach delegates attachment to the underlying node
func (m *Manager) Attach(pid api.PID, ch chan *api.Batch) (error, context.CancelFunc) {
	return m.node.Attach(pid, ch)
}

// Node returns the underlying Node instance
func (m *Manager) Node() *Node {
	return m.node
}
