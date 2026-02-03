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

// PeerManager manages peer node registrations via events.
// Peer nodes are external receivers (e.g., Temporal) that can receive packages.
// Registration is dynamic and driven by event bus notifications.
type PeerManager struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        event.Bus
	router     *Router
	subscriber *eventbus.Subscriber
	stopOnce   sync.Once
}

const peerEventPattern = "peer.(register|delete)"

// NewPeerManager creates a new PeerManager.
func NewPeerManager(router *Router, bus event.Bus, logger *zap.Logger) *PeerManager {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &PeerManager{
		router: router,
		bus:    bus,
		logger: logger,
	}
}

// Start begins listening for peer node registration events.
func (m *PeerManager) Start(ctx context.Context) error {
	m.ctx = ctx

	sub, err := eventbus.NewSubscriber(
		ctx,
		m.bus,
		api.System,
		peerEventPattern,
		m.handleEvent,
	)
	if err != nil {
		return NewSubscriberError(err)
	}
	m.subscriber = sub

	return nil
}

// Stop cleans up manager resources.
func (m *PeerManager) Stop() error {
	m.stopOnce.Do(func() {
		if m.subscriber != nil {
			m.subscriber.Close()
		}
	})
	return nil
}

func (m *PeerManager) handleEvent(e event.Event) {
	switch e.Kind {
	case api.PeerRegister:
		m.handleRegister(e)
	case api.PeerDelete:
		m.handleDelete(e)
	default:
		m.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (m *PeerManager) handleRegister(e event.Event) {
	info, ok := e.Data.(api.PeerInfo)
	if !ok {
		m.logger.Error("invalid peer node payload",
			zap.String("node_id", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		m.sendReject(e.Path, "invalid payload")
		return
	}

	err := m.router.RegisterPeer(info.NodeID, info.Receiver)
	if err != nil {
		m.logger.Error("failed to register peer node",
			zap.String("node_id", info.NodeID),
			zap.Error(err))
		m.sendReject(e.Path, err.Error())
		return
	}

	m.logger.Info("peer node registered",
		zap.String("node_id", info.NodeID))
	m.sendAccept(e.Path)
}

func (m *PeerManager) handleDelete(e event.Event) {
	existed := m.router.UnregisterPeer(e.Path)

	if !existed {
		m.logger.Warn("peer node not found", zap.String("node_id", e.Path))
	} else {
		m.logger.Info("peer node unregistered", zap.String("node_id", e.Path))
	}

	m.sendAccept(e.Path)
}

func (m *PeerManager) sendAccept(path event.Path) {
	m.bus.Send(m.ctx, event.Event{
		System: api.System,
		Kind:   api.PeerAccept,
		Path:   path,
	})
}

func (m *PeerManager) sendReject(path event.Path, reason string) {
	m.bus.Send(m.ctx, event.Event{
		System: api.System,
		Kind:   api.PeerReject,
		Path:   path,
		Data:   reason,
	})
}
