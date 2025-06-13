// file: cluster/internode/service.go
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

// DefaultPort is the port this service will advertise for other nodes to connect to.
const DefaultPort = 7950

// MessageDeliveryCallback is called when a message arrives from a remote node
// and needs to be delivered to the local pubsub system.
type MessageDeliveryCallback func(*pubsub.Package) error

// Service orchestrates inter-node communication. It listens for cluster membership
// events to manage connections and routes pubsub packages to the correct nodes.
// It implements the pubsub.Receiver interface, acting as an upstream for a local pubsub.Node.
type Service struct {
	ctx              context.Context
	logger           *zap.Logger
	connMan          ConnectionManager
	codec            *MessageCodec
	deliveryCallback MessageDeliveryCallback // Callback to deliver messages to local node
	bus              event.Bus
	membership       cluster.Membership
	subscriber       *eventbus.Subscriber
}

// NewService creates a new inter-node service.
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

// Start begins the service's operations. It starts the connection manager,
// subscribes to membership events, and connects to existing nodes.
func (s *Service) Start(ctx context.Context) error {
	s.ctx = ctx
	s.logger.Info("Starting inter-node service...")

	// The `onMessage` callback is where data from remote nodes enters our system.
	onMessage := func(nodeID cluster.NodeID, data []byte) {
		pkg, err := s.codec.Decode(data)
		if err != nil {
			s.logger.Error("Failed to decode incoming message", zap.String("from_node", string(nodeID)), zap.Error(err))
			return
		}

		// Deliver the decoded package to the local node via callback
		if err := s.deliveryCallback(pkg); err != nil {
			s.logger.Error("Failed to deliver message from remote node", zap.String("from_node", string(nodeID)), zap.Error(err))
		}
	}

	if err := s.connMan.Start(ctx, onMessage); err != nil {
		return fmt.Errorf("failed to start connection manager: %w", err)
	}

	// Subscribe to cluster events to know when nodes join or leave.
	sub, err := eventbus.NewSubscriber(ctx, s.bus, cluster.System, "node.(joined|left)", s.handleMembershipEvent)
	if err != nil {
		s.connMan.Stop() // Clean up on failure
		return fmt.Errorf("failed to subscribe to membership events: %w", err)
	}
	s.subscriber = sub

	// Connect to nodes that are already in the cluster.
	for _, nodeInfo := range s.membership.Nodes() {
		if nodeInfo.ID != s.membership.LocalNode().ID {
			s.connectToNode(nodeInfo)
		}
	}

	s.logger.Info("Inter-node service started successfully.")
	return nil
}

// Stop gracefully shuts down the service and its components.
func (s *Service) Stop() error {
	s.logger.Info("Stopping inter-node service...")
	if s.subscriber != nil {
		s.subscriber.Close()
	}
	return s.connMan.Stop()
}

// Send implements the pubsub.Receiver interface. This is the entry point
// for messages from the local node that are destined for other nodes.
func (s *Service) Send(pkg *pubsub.Package) error {
	// Encode the package for network transmission.
	data, err := s.codec.Encode(pkg)
	if err != nil {
		return fmt.Errorf("failed to encode package for node %s: %w", pkg.Target.Node, err)
	}

	// Send the data using the connection manager.
	return s.connMan.SendToNode(pkg.Target.Node, data)
}

// handleMembershipEvent reacts to nodes joining or leaving the cluster.
func (s *Service) handleMembershipEvent(e event.Event) {
	nodeEvent, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		s.logger.Error("Received invalid node event data", zap.Any("data", e.Data))
		return
	}

	nodeInfo := nodeEvent.Node
	if nodeInfo.ID == s.membership.LocalNode().ID {
		return // Ignore events about ourself.
	}

	switch e.Kind {
	case cluster.NodeJoinedEventKind:
		s.logger.Info("Node joined cluster, ensuring connection", zap.String("node_id", string(nodeInfo.ID)))
		s.connectToNode(nodeInfo)
	case cluster.NodeLeftEventKind:
		s.logger.Info("Node left cluster, disconnecting", zap.String("node_id", string(nodeInfo.ID)))
		s.connMan.HandleNodeLeft(nodeInfo.ID)
	}
}

// connectToNode extracts connection info from a node's metadata and
// tells the connection manager to establish a connection.
func (s *Service) connectToNode(nodeInfo cluster.NodeInfo) {
	portStr, ok := nodeInfo.Meta["internode_port"]
	if !ok {
		s.logger.Warn("Node joined without 'internode_port' metadata, cannot connect", zap.String("node_id", string(nodeInfo.ID)))
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		s.logger.Error("Invalid 'internode_port' metadata for node", zap.String("node_id", string(nodeInfo.ID)), zap.String("port", portStr))
		return
	}

	s.connMan.EnsureConnection(nodeInfo.ID, nodeInfo.Addr, port)
}
