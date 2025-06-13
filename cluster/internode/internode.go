package internode

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ponyruntime/pony/api/cluster"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

type MessageDeliveryCallback func(*pubsub.Package) error

type Service struct {
	ctx              context.Context
	logger           *zap.Logger
	connMan          ConnectionManager
	codec            *MessageCodec
	deliveryCallback MessageDeliveryCallback
	bus              event.Bus
	membership       cluster.Membership
	subscriber       *eventbus.Subscriber
}

func NewService(
	logger *zap.Logger,
	connMan ConnectionManager,
	codec *MessageCodec,
	deliveryCallback MessageDeliveryCallback,
	bus event.Bus,
	membership cluster.Membership,
) *Service {
	return &Service{
		logger:           logger.Named("internode"),
		connMan:          connMan,
		codec:            codec,
		deliveryCallback: deliveryCallback,
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
				zap.String("from_node", string(nodeID)),
				zap.Error(err))
			return
		}

		if err := s.deliveryCallback(pkg); err != nil {
			s.logger.Error("Failed to deliver message from remote node",
				zap.String("from_node", string(nodeID)),
				zap.Error(err))
		}
	}

	if err := s.connMan.Start(ctx, onMessage); err != nil {
		return fmt.Errorf("failed to start connection manager: %w", err)
	}

	sub, err := eventbus.NewSubscriber(ctx, s.bus, cluster.System, "node.(joined|left)", s.handleMembershipEvent)
	if err != nil {
		s.connMan.Stop()
		return fmt.Errorf("failed to subscribe to membership events: %w", err)
	}
	s.subscriber = sub

	// Connect to existing nodes
	for _, nodeInfo := range s.membership.Nodes() {
		if nodeInfo.ID != s.membership.LocalNode().ID {
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

func (s *Service) Send(pkg *pubsub.Package) error {
	data, err := s.codec.Encode(pkg)
	pubsub.ReleasePackage(pkg)

	targetNode := pkg.Target
	if err != nil {
		return fmt.Errorf("failed to encode package for node %s: %w", targetNode.Node, err)
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
	case cluster.NodeJoinedEventKind:
		s.logger.Info("Node joined cluster, ensuring connection",
			zap.String("node_id", string(nodeInfo.ID)))
		s.connectToNode(nodeInfo)
	case cluster.NodeLeftEventKind:
		s.logger.Info("Node left cluster, disconnecting",
			zap.String("node_id", string(nodeInfo.ID)))
		s.connMan.HandleNodeLeft(nodeInfo.ID)
	}
}

func (s *Service) connectToNode(nodeInfo cluster.NodeInfo) {
	portStr, ok := nodeInfo.Meta["internode_port"]
	if !ok {
		s.logger.Warn("Node joined without 'internode_port' metadata, cannot connect",
			zap.String("node_id", string(nodeInfo.ID)))
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		s.logger.Error("Invalid 'internode_port' metadata for node",
			zap.String("node_id", string(nodeInfo.ID)),
			zap.String("port", portStr),
			zap.Error(err))
		return
	}

	s.connMan.EnsureConnection(nodeInfo.ID, nodeInfo.Addr, port)
}
