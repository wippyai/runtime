// SPDX-License-Identifier: MPL-2.0

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
	mu         sync.Mutex
	owned      map[string]ownedPeer
	stopped    bool
	ctx        context.Context
	logger     *zap.Logger
	bus        event.Bus
	router     *Router
	subscriber *eventbus.Subscriber
	stopOnce   sync.Once
}

type ownedPeer struct {
	info    *api.PeerInfo
	release context.CancelFunc
}

const peerEventPattern = "peer.(register|delete)"

// NewPeerManager creates a new PeerManager.
func NewPeerManager(router *Router, bus event.Bus, logger *zap.Logger) *PeerManager {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &PeerManager{
		router: router,
		owned:  make(map[string]ownedPeer),
		bus:    bus,
		logger: logger,
	}
}

// Start begins listening for peer node registration events.
func (m *PeerManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return fmt.Errorf("peer manager stopped")
	}
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
		m.mu.Lock()
		m.stopped = true
		subscriber := m.subscriber
		for node, owned := range m.owned {
			owned.info.Retire()
			owned.release()
			delete(m.owned, node)
		}
		m.mu.Unlock()
		if subscriber != nil {
			subscriber.Close()
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
	info, ok := e.Data.(*api.PeerInfo)
	if !ok || info == nil || info.NodeID != e.Path {
		m.logger.Error("invalid peer node payload",
			zap.String("node_id", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		m.sendReject(e.Path, "invalid payload")
		return
	}

	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		m.sendReject(e.Path, "peer manager stopped")
		return
	}
	if info.Retired() {
		m.mu.Unlock()
		m.sendReject(e.Path, "peer registration retired")
		return
	}
	release, err := m.router.RegisterOwnedPeer(info.NodeID, info.Receiver)
	if err == nil {
		m.owned[info.NodeID] = ownedPeer{info: info, release: release}
	}
	m.mu.Unlock()
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
	info, ok := e.Data.(*api.PeerInfo)
	if !ok || info == nil || info.NodeID != e.Path {
		m.sendReject(e.Path, "delete requires exact peer registration")
		return
	}
	m.mu.Lock()
	info.Retire()
	owned, existed := m.owned[e.Path]
	existed = existed && owned.info == info
	if existed {
		delete(m.owned, e.Path)
		owned.release()
	}
	m.mu.Unlock()

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
