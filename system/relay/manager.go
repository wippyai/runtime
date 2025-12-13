package relay

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	api "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// NodeManager manages state of current node based on events received from the bus.
type NodeManager struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        event.Bus
	node       *Node
	subscriber *eventbus.Subscriber
	stopOnce   sync.Once
}

// NewNodeManager creates a new node manager instance that wraps a Node
func NewNodeManager(node *Node, bus event.Bus, logger *zap.Logger) *NodeManager {
	return &NodeManager{
		node:   node,
		bus:    bus,
		logger: logger,
	}
}

// Start initializes the manager and begins listening for host registration events
func (m *NodeManager) Start(ctx context.Context) error {
	m.ctx = ctx

	// Subscribe to host registration events
	sub, err := eventbus.NewSubscriber(
		ctx,
		m.bus,
		api.System,
		"host.(register|delete)",
		m.handleEvent,
	)
	if err != nil {
		return api.NewSubscriberError(err)
	}
	m.subscriber = sub

	return nil
}

// Stop gracefully shuts down the manager
func (m *NodeManager) Stop() error {
	m.stopOnce.Do(func() {
		if m.subscriber != nil {
			m.subscriber.Close()
		}
	})
	return nil
}

func (m *NodeManager) handleEvent(e event.Event) {
	switch e.Kind {
	case api.HostRegister:
		m.handleRegisterHost(e)
	case api.HostDelete:
		m.handleDeleteHost(e)
	default:
		m.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path),
			zap.String("node_id", m.node.ID()),
		)
	}
}

func (m *NodeManager) handleRegisterHost(e event.Event) {
	host, ok := e.Data.(api.Receiver)
	if !ok {
		m.logger.Error("invalid host payload",
			zap.String("host", e.Path),
			zap.String("node_id", m.node.ID()),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		m.sendReject(e.Path, "invalid host payload")
		return
	}

	// Register with the underlying node
	err := m.node.RegisterHost(e.Path, host)
	if err != nil {
		m.logger.Error("failed to register host",
			zap.String("host", e.Path),
			zap.String("node_id", m.node.ID()),
			zap.Error(err))

		m.sendReject(e.Path, err.Error())
		return
	}

	m.logger.Info("host registered successfully",
		zap.String("host", e.Path),
		zap.String("node_id", m.node.ID()),
		zap.String("type", fmt.Sprintf("%T", host)),
	)
	m.sendAccept(e.Path)
}

func (m *NodeManager) handleDeleteHost(e event.Event) {
	// Unregister from the underlying node
	m.node.UnregisterHost(e.Path)

	m.logger.Info("host unregistered successfully",
		zap.String("host", e.Path),
		zap.String("node_id", m.node.ID()),
	)
	m.sendAccept(e.Path)
}

func (m *NodeManager) sendAccept(path event.Path) {
	m.bus.Send(m.ctx, event.Event{
		System: api.System,
		Kind:   api.HostAccept,
		Path:   path,
	})
}

func (m *NodeManager) sendReject(path event.Path, reason string) {
	m.bus.Send(m.ctx, event.Event{
		System: api.System,
		Kind:   api.HostReject,
		Path:   path,
		Data:   reason,
	})
}

// Node returns the underlying Node instance
func (m *NodeManager) Node() api.Node {
	return m.node
}
