// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"strconv"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type PackageCallback func(*relay.Package) error

type Service struct {
	ctx              context.Context
	logger           *zap.Logger
	connMan          ConnectionManager
	codec            cluster.MessageCodec
	deliveryCallback PackageCallback
	bus              event.Bus
	membership       cluster.Membership
	subscriber       *eventbus.Subscriber
}

func NewService(
	logger *zap.Logger,
	connMan ConnectionManager,
	codec cluster.MessageCodec,
	pkgCallback PackageCallback,
	bus event.Bus,
	membership cluster.Membership,
) *Service {
	return &Service{
		logger:           logger.Named("internode"),
		connMan:          connMan,
		codec:            codec,
		deliveryCallback: pkgCallback,
		bus:              bus,
		membership:       membership,
	}
}

func (s *Service) Start(ctx context.Context) error {
	s.ctx = ctx
	s.logger.Info("Starting inter-node service...")

	onMessage := func(nodeID cluster.NodeID, data []byte) {
		pkg, err := s.codec.Decode(data)
		if err != nil {
			s.logger.Error("Failed to decode incoming message",
				zap.String("from_node", nodeID),
				zap.Error(err))
			return
		}
		if err := s.deliveryCallback(pkg); err != nil {
			s.logger.Error("Failed to deliver message from remote node",
				zap.String("from_node", nodeID),
				zap.Error(err))
		}
	}

	if err := s.connMan.Start(ctx, onMessage); err != nil {
		return NewStartConnectionManagerError(err)
	}

	sub, err := eventbus.NewSubscriber(ctx, s.bus, cluster.System, "node.(joined|left)", s.handleMembershipEvent)
	if err != nil {
		_ = s.connMan.Stop()
		return NewSubscribeMembershipError(err)
	}
	s.subscriber = sub

	// Process nodes that are already in the cluster at startup.
	for _, nodeInfo := range s.membership.Nodes() {
		if nodeInfo.ID != s.membership.LocalNode().ID {
			s.logger.Info("Processing pre-existing cluster member", zap.String("node_id", nodeInfo.ID))
			s.connMan.AddManagedNode(nodeInfo.ID)
			s.connectToNode(nodeInfo)
		}
	}

	s.logger.Info("Inter-node service started successfully")
	return nil
}

func (s *Service) Stop() error {
	s.logger.Info("Stopping inter-node service...")
	if s.subscriber != nil {
		s.subscriber.Close()
	}
	return s.connMan.Stop()
}

func (s *Service) Send(pkg *relay.Package) error {
	data, err := s.codec.Encode(pkg)
	targetNode := pkg.Target
	relay.ReleasePackage(pkg)
	if err != nil {
		return NewEncodePackageError(targetNode.Node, err)
	}
	return s.connMan.SendToNode(targetNode.Node, data)
}

func (s *Service) handleMembershipEvent(e event.Event) {
	nodeEvent, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		s.logger.Error("Received invalid node event data", zap.Any("data", e.Data))
		return
	}
	nodeInfo := nodeEvent.Node
	if nodeInfo.ID == s.membership.LocalNode().ID {
		return
	}

	switch e.Kind {
	case cluster.NodeJoined:
		s.logger.Info("Node joined cluster, preparing state and connection",
			zap.String("node_id", nodeInfo.ID))
		s.connMan.AddManagedNode(nodeInfo.ID)
		s.connectToNode(nodeInfo)
	case cluster.NodeLeft:
		s.logger.Info("Node left cluster, cleaning up state and connection",
			zap.String("node_id", nodeInfo.ID))
		s.connMan.RemoveManagedNode(nodeInfo.ID)
	}
}

func (s *Service) connectToNode(nodeInfo cluster.NodeInfo) {
	portStr, ok := nodeInfo.Meta["internode_port"]
	if !ok {
		s.logger.Warn("Node metadata missing 'internode_port', cannot connect",
			zap.String("node_id", nodeInfo.ID))
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		s.logger.Error("Invalid 'internode_port' metadata for node",
			zap.String("node_id", nodeInfo.ID), zap.String("port", portStr), zap.Error(err))
		return
	}
	s.connMan.EnsureConnection(nodeInfo.ID, nodeInfo.Addr, port)
}
