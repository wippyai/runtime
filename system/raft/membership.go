// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"
	"fmt"
	"net"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	raftapi "github.com/wippyai/runtime/api/raft"

	"go.uber.org/zap"
)

// MembershipHandler listens for cluster membership changes and updates
// the Raft voter set accordingly. Only the current leader performs
// AddVoter/RemoveServer.
type MembershipHandler struct {
	svc    raftapi.Service
	bus    event.Bus
	logger *zap.Logger
	stopCh chan struct{}
}

// NewMembershipHandler creates a handler that bridges cluster membership
// events to Raft voter management.
func NewMembershipHandler(svc raftapi.Service, bus event.Bus, logger *zap.Logger) *MembershipHandler {
	return &MembershipHandler{
		svc:    svc,
		bus:    bus,
		logger: logger,
		stopCh: make(chan struct{}),
	}
}

// Start begins listening for cluster node events.
func (h *MembershipHandler) Start(ctx context.Context) error {
	ch := make(chan event.Event, 32)
	subID, err := h.bus.Subscribe(ctx, cluster.System, ch)
	if err != nil {
		return fmt.Errorf("subscribe to cluster events: %w", err)
	}

	go h.loop(ctx, ch, subID)
	return nil
}

// Stop terminates the membership handler.
func (h *MembershipHandler) Stop() {
	close(h.stopCh)
}

func (h *MembershipHandler) loop(ctx context.Context, ch <-chan event.Event, subID event.SubscriberID) {
	defer h.bus.Unsubscribe(ctx, subID)

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			switch e.Kind {
			case cluster.NodeJoined:
				h.handleNodeJoined(e)
			case cluster.NodeLeft:
				h.handleNodeLeft(e)
			}
		case <-h.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (h *MembershipHandler) handleNodeJoined(e event.Event) {
	// Only the leader manages Raft membership.
	//
	// The IsLeader() check is an optimization to avoid unnecessary configuration
	// lookups on non-leaders. Between this check and the AddVoter() call below,
	// leadership can transfer to another node. This is an accepted race:
	//   - If leadership is lost, AddVoter() returns ErrNotLeader which is already logged.
	//   - AddVoter is idempotent, so duplicate calls from old/new leaders are harmless.
	//   - The new leader will receive the same NodeJoined event and handle it.
	if !h.svc.IsLeader() {
		return
	}

	nodeEvt, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		return
	}
	nodeInfo := nodeEvt.Node

	raftPort, ok := nodeInfo.Meta["raft_port"]
	if !ok || raftPort == "" {
		h.logger.Debug("node joined without raft_port, skipping", zap.String("node", nodeInfo.ID))
		return
	}

	raftAddr := net.JoinHostPort(nodeInfo.Addr, raftPort)

	// Check if already a voter.
	servers, err := h.svc.GetConfiguration()
	if err != nil {
		h.logger.Error("get raft configuration", zap.Error(err))
		return
	}
	for _, srv := range servers {
		if srv.ID == nodeInfo.ID {
			h.logger.Debug("node already in raft cluster", zap.String("node", nodeInfo.ID))
			return
		}
	}

	h.logger.Info("adding raft voter", zap.String("node", nodeInfo.ID), zap.String("addr", raftAddr))
	if err := h.svc.AddVoter(nodeInfo.ID, raftAddr, defaultTimeout()); err != nil {
		h.logger.Error("failed to add raft voter", zap.String("node", nodeInfo.ID), zap.Error(err))
	}
}

func (h *MembershipHandler) handleNodeLeft(e event.Event) {
	if !h.svc.IsLeader() {
		return
	}

	nodeEvt, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		return
	}

	h.logger.Info("removing raft server", zap.String("node", nodeEvt.Node.ID))
	if err := h.svc.RemoveServer(nodeEvt.Node.ID, defaultTimeout()); err != nil {
		h.logger.Error("failed to remove raft server", zap.String("node", nodeEvt.Node.ID), zap.Error(err))
	}
}
