package membership

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"go.uber.org/zap"
)

// Service implements cluster membership using memberlist
type Service struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        event.Bus
	config     Config
	memberlist *memberlist.Memberlist

	// Node state
	mu    sync.RWMutex
	nodes map[string]cluster.NodeInfo // NodeID -> cluster.NodeInfo
}

// Config holds membership service configuration
type Config struct {
	NodeName     string
	BindAddr     string
	BindPort     int
	JoinAddrs    []string
	SecretFile   string // Secret from file
	SecretString string // Secret as string
	AdvertiseIP  string
	VeryVerbose  bool // Enable memberlist debug logs only in very verbose mode

	// Node metadata for service discovery
	Meta cluster.NodeMeta
}

// NewService creates a new membership service
func NewService(config Config, bus event.Bus, logger *zap.Logger) *Service {
	return &Service{
		logger: logger,
		bus:    bus,
		config: config,
		nodes:  make(map[string]cluster.NodeInfo),
	}
}

// Start initializes and starts the membership service
func (s *Service) Start(ctx context.Context) error {
	s.ctx = ctx
	s.logger.Info("starting service",
		zap.String("node_name", s.config.NodeName),
		zap.String("bind_address", fmt.Sprintf("%s:%d", s.config.BindAddr, s.config.BindPort)))

	// Create memberlist config
	mlConfig := memberlist.DefaultLocalConfig()
	mlConfig.Name = s.config.NodeName
	mlConfig.BindAddr = s.config.BindAddr
	mlConfig.BindPort = s.config.BindPort
	mlConfig.Events = &eventDelegate{service: s}
	mlConfig.Delegate = &delegate{service: s}

	// Set advertise address if provided
	if s.config.AdvertiseIP != "" {
		mlConfig.AdvertiseAddr = s.config.AdvertiseIP
		mlConfig.AdvertisePort = s.config.BindPort
		s.logger.Info("using custom advertise address",
			zap.String("advertise", fmt.Sprintf("%s:%d", s.config.AdvertiseIP, s.config.BindPort)))
	}

	// Only enable memberlist debug logs in very verbose mode
	if s.config.VeryVerbose {
		// Keep default logging (goes to stderr)
		s.logger.Debug("memberlist debug logging enabled")
	} else {
		// Completely disable memberlist logging
		mlConfig.LogOutput = io.Discard
	}

	// Load secret key if provided
	if s.config.SecretFile != "" || s.config.SecretString != "" {
		secretKey, err := s.loadSecretKey()
		if err != nil {
			return fmt.Errorf("failed to load cluster secret key: %w", err)
		}
		mlConfig.SecretKey = secretKey
		s.logger.Info("encryption enabled", zap.Int("key_size", len(secretKey)))
	} else {
		s.logger.Warn("encryption disabled - no secret key provided")
	}

	// Create memberlist
	ml, err := memberlist.Create(mlConfig)
	if err != nil {
		return fmt.Errorf("failed to create memberlist: %w", err)
	}
	s.memberlist = ml

	// Join cluster if addresses provided
	if len(s.config.JoinAddrs) > 0 {
		s.logger.Info("joining existing cluster",
			zap.Strings("join_addresses", s.config.JoinAddrs))

		n, err := ml.Join(s.config.JoinAddrs)
		if err != nil {
			s.logger.Error("failed to join cluster",
				zap.Error(err),
				zap.Strings("join_addresses", s.config.JoinAddrs))
			return fmt.Errorf("failed to join cluster: %w", err)
		}

		s.logger.Info("successfully joined cluster",
			zap.Int("discovered_nodes", n))
	} else {
		s.logger.Info("starting as cluster bootstrap node")
	}

	// Log initial cluster state
	members := ml.Members()
	s.logger.Info("membership active",
		zap.String("local_node", s.config.NodeName),
		zap.Int("total_members", len(members)))

	return nil
}

// Stop gracefully shuts down the membership service
func (s *Service) Stop() error {
	s.logger.Info("shutting down cluster membership service")

	if s.memberlist != nil {
		// Leave cluster gracefully
		s.logger.Info("leaving cluster gracefully")
		if err := s.memberlist.Leave(3 * time.Second); err != nil {
			s.logger.Warn("failed to leave cluster gracefully", zap.Error(err))
		} else {
			s.logger.Info("left cluster successfully")
		}

		// Shutdown memberlist
		if err := s.memberlist.Shutdown(); err != nil {
			s.logger.Warn("failed to shutdown memberlist cleanly", zap.Error(err))
		}
	}

	s.logger.Info("membership service stopped")
	return nil
}

// Nodes returns current cluster members (implements cluster.Membership interface)
func (s *Service) Nodes() []cluster.NodeInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nodes := make([]cluster.NodeInfo, 0, len(s.nodes))
	for _, node := range s.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// LocalNode returns information about the local node.
func (s *Service) LocalNode() cluster.NodeInfo {
	if s.memberlist == nil {
		// Return info from config if memberlist isn't up yet
		return cluster.NodeInfo{
			ID:   s.config.NodeName,
			Addr: s.config.BindAddr,
			Meta: s.config.Meta,
		}
	}

	local := s.memberlist.LocalNode()
	addr := local.Address()
	return cluster.NodeInfo{
		ID:   local.Name,
		Addr: addr,
		Meta: s.config.Meta, // The delegate provides the meta, but we have it here directly.
	}
}

// loadSecretKey loads encryption key from file or string
func (s *Service) loadSecretKey() ([]byte, error) {
	var keyStr string

	// Load from file if specified
	switch {
	case s.config.SecretFile != "":
		data, err := os.ReadFile(s.config.SecretFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read secret file: %w", err)
		}
		keyStr = strings.TrimSpace(string(data))
	case s.config.SecretString != "":
		// Use string directly
		keyStr = s.config.SecretString
	default:
		return nil, fmt.Errorf("no secret key provided")
	}

	// Decode base64 key
	return base64.StdEncoding.DecodeString(keyStr)
}

// publishEvent publishes a cluster event to the event bus
func (s *Service) publishEvent(kind event.Kind, node cluster.NodeInfo) {
	s.bus.Send(s.ctx, event.Event{
		System: cluster.System,
		Kind:   kind,
		Path:   node.ID,
		Data:   cluster.NodeEvent{Node: node},
	})
}

// eventDelegate handles memberlist events
type eventDelegate struct {
	service *Service
}

func (ed *eventDelegate) NotifyJoin(node *memberlist.Node) {
	// Skip events for our own node
	if node.Name == ed.service.config.NodeName {
		return
	}

	nodeInfo := cluster.NodeInfo{
		ID:   node.Name,
		Addr: node.Addr.String(), // Just the IP address
		Meta: ed.parseNodeMeta(node.Meta),
	}

	ed.service.mu.Lock()
	ed.service.nodes[node.Name] = nodeInfo
	ed.service.mu.Unlock()

	ed.service.logger.Info("node joined",
		zap.String("node_id", node.Name),
		zap.String("address", nodeInfo.Addr),
		zap.Any("metadata", nodeInfo.Meta))

	ed.service.publishEvent(cluster.NodeJoinedEventKind, nodeInfo)
}

func (ed *eventDelegate) NotifyLeave(node *memberlist.Node) {
	// Skip events for our own node
	if node.Name == ed.service.config.NodeName {
		return
	}

	nodeInfo := cluster.NodeInfo{
		ID:   node.Name,
		Addr: node.Addr.String(), // Just the IP address
		Meta: ed.parseNodeMeta(node.Meta),
	}

	ed.service.mu.Lock()
	delete(ed.service.nodes, node.Name)
	ed.service.mu.Unlock()

	ed.service.logger.Info("node left",
		zap.String("node_id", node.Name),
		zap.String("address", nodeInfo.Addr))

	ed.service.publishEvent(cluster.NodeLeftEventKind, nodeInfo)
}

func (ed *eventDelegate) NotifyUpdate(node *memberlist.Node) {
	// Skip events for our own node
	if node.Name == ed.service.config.NodeName {
		return
	}

	nodeInfo := cluster.NodeInfo{
		ID:   node.Name,
		Addr: node.Addr.String(), // Just the IP address
		Meta: ed.parseNodeMeta(node.Meta),
	}

	ed.service.mu.Lock()
	ed.service.nodes[node.Name] = nodeInfo
	ed.service.mu.Unlock()

	ed.service.logger.Info("node updated",
		zap.String("node_id", node.Name),
		zap.String("address", nodeInfo.Addr),
		zap.Any("metadata", nodeInfo.Meta))

	ed.service.publishEvent(cluster.NodeUpdatedEventKind, nodeInfo)
}

func (ed *eventDelegate) parseNodeMeta(meta []byte) cluster.NodeMeta {
	if len(meta) == 0 {
		return make(cluster.NodeMeta)
	}

	var nodeMeta cluster.NodeMeta
	if err := json.Unmarshal(meta, &nodeMeta); err != nil {
		// Fallback to string representation
		return cluster.NodeMeta{"raw": string(meta)}
	}

	return nodeMeta
}

// delegate handles memberlist delegate functions
type delegate struct {
	service *Service
}

func (d *delegate) NodeMeta(limit int) []byte {
	if len(d.service.config.Meta) == 0 {
		return []byte{}
	}

	data, err := json.Marshal(d.service.config.Meta)
	if err != nil {
		d.service.logger.Error("failed to marshal node metadata", zap.Error(err))
		return []byte{}
	}

	if len(data) > limit {
		d.service.logger.Warn("node metadata exceeds memberlist limit",
			zap.Int("size", len(data)),
			zap.Int("limit", limit))
		return []byte{}
	}

	return data
}

func (d *delegate) NotifyMsg(data []byte) {
	// Handle incoming gossip messages - could be used for custom protocols
	if d.service.config.VeryVerbose {
		d.service.logger.Debug("received gossip message", zap.Int("size", len(data)))
	}
}

func (d *delegate) GetBroadcasts(_, _ int) [][]byte {
	// Return messages to broadcast - could be used for custom state sync
	// For now, no custom broadcasts
	return nil
}

func (d *delegate) LocalState(_ bool) []byte {
	// TODO: Can be used later for gossip state sync
	return []byte("{}")
}

func (d *delegate) MergeRemoteState(buf []byte, join bool) {
	// TODO: Can be used later for gossip state sync
	if len(buf) > 0 && d.service.config.VeryVerbose {
		d.service.logger.Debug("merging remote state",
			zap.Int("size", len(buf)),
			zap.Bool("join", join))
	}
}
