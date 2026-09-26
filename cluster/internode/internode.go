// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// orphanSweepInterval is how often the internode service reconciles its
// managed-node set against the authoritative membership snapshot. It's a
// defensive backstop against missed `cluster.NodeLeft` events under
// gossip storm or chaos partition; at steady state every tick reports
// zero evictions.
const orphanSweepInterval = 60 * time.Second

// Membership metadata keys a node publishes for internode connections: the
// listener port, dialed at the member's membership address, and the identity
// key the handshake pins.
const (
	MetadataPort      = "internode_port"
	MetadataPublicKey = "internode_public_key"
)

// PackageCallback takes ownership only when it returns nil. On error it must
// leave the package owned by Service, which releases it after rejection.
type PackageCallback func(*relay.Package) error

type Service struct {
	ctx              context.Context
	cancel           context.CancelFunc
	logger           *zap.Logger
	connMan          ConnectionManager
	codec            cluster.MessageCodec
	deliveryCallback PackageCallback
	sessionEnded     func(cluster.NodeID)
	bus              event.Bus
	membership       cluster.Membership
	subscriber       *eventbus.Subscriber
	localNodeID      cluster.NodeID
}

// NewService wires the transport to local delivery. sessionEnded runs
// synchronously when a session with a node ends, after the last frame of that
// session was delivered and before any frame of a later session is; it must
// break local links and monitors of the node's processes.
func NewService(
	logger *zap.Logger,
	connMan ConnectionManager,
	codec cluster.MessageCodec,
	pkgCallback PackageCallback,
	sessionEnded func(cluster.NodeID),
	bus event.Bus,
	membership cluster.Membership,
) *Service {
	return &Service{
		logger:           logger.Named("internode"),
		connMan:          connMan,
		codec:            codec,
		deliveryCallback: pkgCallback,
		sessionEnded:     sessionEnded,
		bus:              bus,
		membership:       membership,
	}
}

func (s *Service) Start(ctx context.Context) error {
	ctx, s.cancel = context.WithCancel(ctx)
	s.ctx = ctx
	s.localNodeID = s.membership.LocalNode().ID
	s.logger.Info("Starting inter-node service...")

	onMessage := func(nodeID cluster.NodeID, data []byte) {
		s.logger.Debug("Received raw message from remote node",
			zap.String("from_node", nodeID),
			zap.Int("data_len", len(data)))
		pkg, err := s.codec.Decode(data)
		if err != nil {
			s.logger.Error("Failed to decode incoming message",
				zap.String("from_node", nodeID),
				zap.Error(err))
			s.connMan.RecordDropReason("decode_failed")
			return
		}
		// Preserve the logical source, which may represent a virtual peer or
		// be anonymous. Record the connection peer separately from the wire
		// envelope so services need not trust a claimed Source.Node.
		pkg.IngressNode = nodeID
		s.logger.Debug("Decoded message, delivering",
			zap.String("from_node", nodeID),
			zap.String("target_host", pkg.Target.Host))
		if err := s.deliveryCallback(pkg); err != nil {
			relay.ReleasePackage(pkg)
			// Hot path under partition: the local PG host may have torn down
			// while a peer is still sending to it. Counted as a drop with
			// no per-message log to avoid the chaos-time spam we observed.
			s.connMan.RecordDropReason("delivery_failed")
			s.logger.Debug("delivery failed",
				zap.String("from_node", nodeID),
				zap.Error(err))
		}
	}

	if err := s.connMan.Start(ctx, onMessage, s.sessionEnded); err != nil {
		s.cancel()
		return NewStartConnectionManagerError(err)
	}

	sub, err := eventbus.NewSubscriber(ctx, s.bus, cluster.System, "node.(joined|left|updated)", s.handleMembershipEvent)
	if err != nil {
		s.cancel()
		_ = s.connMan.Stop()
		return NewSubscribeMembershipError(err)
	}
	s.subscriber = sub

	// Process nodes that are already in the cluster at startup.
	for _, nodeInfo := range s.membership.Nodes() {
		if nodeInfo.ID != s.localNodeID {
			s.logger.Info("Processing pre-existing cluster member", zap.String("node_id", nodeInfo.ID))
			s.connMan.AddManagedNode(nodeInfo.ID)
			s.connectToNode(nodeInfo)
		}
	}

	go s.orphanSweepLoop(ctx)

	s.logger.Info("Inter-node service started successfully")
	return nil
}

// orphanSweepLoop periodically reconciles the connection manager's set
// of managed nodes against the authoritative membership snapshot.
//
// Steady state: every tick reports zero evictions. Non-zero means a
// `cluster.NodeLeft` event was missed (gossip storm, missed delivery,
// chaos partition) and entries would have leaked otherwise — the sweep
// is the only thing keeping `internode.NodeStateManager.nodeStates`
// bounded under prolonged churn.
func (s *Service) orphanSweepLoop(ctx context.Context) {
	t := time.NewTicker(orphanSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			members := s.membership.Nodes()
			known := make(map[cluster.NodeID]struct{}, len(members)+1)
			known[s.localNodeID] = struct{}{}
			for _, n := range members {
				known[n.ID] = struct{}{}
			}
			if evicted := s.connMan.EvictOrphanNodes(known); evicted > 0 {
				s.logger.Warn("orphan-sweep evicted unmanaged nodes",
					zap.Int("count", evicted))
			}
		}
	}
}

func (s *Service) Stop() error {
	s.logger.Info("Stopping inter-node service...")
	if s.cancel != nil {
		s.cancel()
	}
	if s.subscriber != nil {
		s.subscriber.Close()
	}
	return s.connMan.Stop()
}

var ErrContextQueueUnsupported = errors.New("internode: connection manager does not support cancellable queue admission")

// SendContext transfers package ownership only on successful queue admission.
// Cancellation stops waiting to admit; it cannot recall an accepted message.
func (s *Service) SendContext(ctx context.Context, pkg *relay.Package) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if pkg == nil {
		return errors.New("internode: nil package")
	}
	sender, ok := s.connMan.(ContextConnectionManager)
	if !ok {
		return ErrContextQueueUnsupported
	}
	data, err := s.codec.Encode(pkg)
	if err != nil {
		return NewEncodePackageError(pkg.Target.Node, err)
	}
	var topic string
	if len(pkg.Messages) > 0 {
		topic = pkg.Messages[0].Topic
	}
	// Discovery consumers may run before the transport subscriber. Wait for
	// its registration without recreating state from a membership snapshot.
	if !s.connMan.IsManaged(pkg.Target.Node) && s.membership != nil {
		for _, member := range s.membership.Nodes() {
			if member.ID != pkg.Target.Node {
				continue
			}
			if waiter, ok := s.connMan.(interface {
				WaitManaged(context.Context, cluster.NodeID) error
			}); ok {
				if err := waiter.WaitManaged(ctx, pkg.Target.Node); err != nil {
					return err
				}
			}
			break
		}
	}
	// Queue admission still checks the state: a departure may race the wait.
	if err := sender.SendToNodeContext(ctx, pkg.Target.Node, data, ClassForTopic(topic)); err != nil {
		return err
	}
	relay.ReleasePackage(pkg)
	return nil
}

var _ relay.ContextSender = (*Service)(nil)

func (s *Service) Send(pkg *relay.Package) error {
	data, err := s.codec.Encode(pkg)
	targetNode := pkg.Target
	var topic string
	if len(pkg.Messages) > 0 {
		topic = pkg.Messages[0].Topic
	}
	relay.ReleasePackage(pkg)
	if err != nil {
		return NewEncodePackageError(targetNode.Node, err)
	}

	// Ensure the target node is managed before sending. This covers the race
	// where a higher-level service (e.g. PG) reacts to a NodeJoined event
	// and sends a message before the internode event handler has had a chance
	// to call AddManagedNode + EnsureConnection.
	s.ensureNodeManaged(targetNode.Node)

	return s.connMan.SendToNode(targetNode.Node, data, ClassForTopic(topic))
}

// ensureNodeManaged verifies that the target node is registered as a managed
// node in the connection manager. If not, and the node is a current cluster
// member, it registers it and initiates a connection.
func (s *Service) ensureNodeManaged(nodeID cluster.NodeID) {
	if s.membership == nil {
		return
	}

	// Fast path: already managed (may or may not be connected yet).
	if s.connMan.IsManaged(nodeID) {
		return
	}

	// Look up the node in the membership list.
	for _, nodeInfo := range s.membership.Nodes() {
		if nodeInfo.ID == nodeID {
			s.connMan.AddManagedNode(nodeID)
			s.connectToNode(nodeInfo)
			return
		}
	}
}

func (s *Service) handleMembershipEvent(e event.Event) {
	nodeEvent, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		s.logger.Error("Received invalid node event data", zap.Any("data", e.Data))
		return
	}
	nodeInfo := nodeEvent.Node
	if nodeInfo.ID == s.localNodeID {
		return
	}

	switch e.Kind {
	case cluster.NodeJoined:
		s.logger.Info("Node joined cluster, preparing state and connection",
			zap.String("node_id", nodeInfo.ID))
		s.connMan.AddManagedNode(nodeInfo.ID)
		s.connectToNode(nodeInfo)
	case cluster.NodeUpdated:
		if s.connMan.IsManaged(nodeInfo.ID) {
			s.connectToNode(nodeInfo)
		}
	case cluster.NodeLeft:
		s.logger.Info("Node left cluster, cleaning up state and connection",
			zap.String("node_id", nodeInfo.ID))
		s.connMan.RemoveManagedNode(nodeInfo.ID, s.departingIncarnation(nodeInfo))
	}
}

// departingIncarnation reads the incarnation a departing node advertised.
// Zero means the departure names no incarnation and removes any session.
func (s *Service) departingIncarnation(nodeInfo cluster.NodeInfo) uint64 {
	raw, ok := nodeInfo.Meta[cluster.MetaIncarnation]
	if !ok {
		return 0
	}
	incarnation, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		s.logger.Error("Invalid incarnation metadata on departing node; removing its session",
			zap.String("node_id", nodeInfo.ID), zap.String("incarnation", raw), zap.Error(err))
		return 0
	}
	return incarnation
}

func (s *Service) connectToNode(nodeInfo cluster.NodeInfo) {
	portStr, ok := nodeInfo.Meta[MetadataPort]
	if !ok {
		s.logger.Warn("Node metadata missing 'internode_port', cannot connect",
			zap.String("node_id", nodeInfo.ID))
		return
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		s.logger.Error("Invalid 'internode_port' metadata for node",
			zap.String("node_id", nodeInfo.ID), zap.String("port", portStr))
		return
	}

	// A member is dialed at its membership address. A member that cannot be
	// dialed there reaches this node through its own dial.
	addr := nodeInfo.Addr
	if host, _, splitErr := net.SplitHostPort(addr); splitErr == nil {
		addr = host
	}
	s.connMan.EnsureConnection(nodeInfo.ID, addr, port)
}
