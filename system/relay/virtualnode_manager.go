package relay

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/event"
	api "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// VirtualNodeManager manages virtual node registrations via events.
// Virtual nodes are non-physical nodes that can receive packages (e.g., Temporal clients).
// Registration is dynamic and driven by event bus notifications.
type VirtualNodeManager struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        event.Bus
	router     *Router
	subscriber *eventbus.Subscriber
}

// NewVirtualNodeManager creates a new VirtualNodeManager.
func NewVirtualNodeManager(router *Router, bus event.Bus, logger *zap.Logger) *VirtualNodeManager {
	return &VirtualNodeManager{
		router: router,
		bus:    bus,
		logger: logger,
	}
}

// Start begins listening for virtual node registration events.
func (m *VirtualNodeManager) Start(ctx context.Context) error {
	m.ctx = ctx

	sub, err := eventbus.NewSubscriber(
		ctx,
		m.bus,
		api.VirtualNodeSystem,
		"virtual_nodes.(register|delete)",
		m.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	m.subscriber = sub

	return nil
}

// Stop cleans up manager resources.
func (m *VirtualNodeManager) Stop() error {
	if m.subscriber != nil {
		m.subscriber.Close()
	}
	return nil
}

func (m *VirtualNodeManager) handleEvent(e event.Event) {
	switch e.Kind {
	case api.VirtualNodeRegister:
		m.handleRegister(e)
	case api.VirtualNodeDelete:
		m.handleDelete(e)
	default:
		m.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (m *VirtualNodeManager) handleRegister(e event.Event) {
	info, ok := e.Data.(api.VirtualNodeInfo)
	if !ok {
		m.logger.Error("invalid virtual node payload",
			zap.String("nodeID", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		m.sendReject(e.Path, "invalid payload")
		return
	}

	err := m.router.RegisterVirtualNode(info.NodeID, info.Receiver)
	if err != nil {
		m.logger.Error("failed to register virtual node",
			zap.String("nodeID", string(info.NodeID)),
			zap.Error(err))
		m.sendReject(e.Path, err.Error())
		return
	}

	m.logger.Info("virtual node registered",
		zap.String("nodeID", string(info.NodeID)))
	m.sendAccept(e.Path)
}

func (m *VirtualNodeManager) handleDelete(e event.Event) {
	nodeID := api.NodeID(e.Path)
	existed := m.router.UnregisterVirtualNode(nodeID)

	if !existed {
		m.logger.Warn("virtual node not found", zap.String("nodeID", e.Path))
	} else {
		m.logger.Info("virtual node unregistered", zap.String("nodeID", e.Path))
	}

	m.sendAccept(e.Path)
}

func (m *VirtualNodeManager) sendAccept(path event.Path) {
	m.bus.Send(m.ctx, event.Event{
		System: api.VirtualNodeSystem,
		Kind:   api.VirtualNodeAccept,
		Path:   path,
	})
}

func (m *VirtualNodeManager) sendReject(path event.Path, reason string) {
	m.bus.Send(m.ctx, event.Event{
		System: api.VirtualNodeSystem,
		Kind:   api.VirtualNodeReject,
		Path:   path,
		Data:   reason,
	})
}
