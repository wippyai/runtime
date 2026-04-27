// SPDX-License-Identifier: MPL-2.0

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
	"github.com/wippyai/runtime/api/metrics"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Service implements cluster membership using memberlist
type Service struct {
	lastChangeAt time.Time
	ctx          context.Context
	bus          event.Bus
	transport    memberlist.Transport
	logger       *zap.Logger
	memberlist   *memberlist.Memberlist
	nodes        map[string]cluster.NodeInfo
	nodeStates   map[string]memberlist.NodeStateType
	tel          *telemetry
	config       Config
	mu           sync.RWMutex
}

// Config holds membership service configuration
type Config struct {
	Transport    memberlist.Transport
	Meta         cluster.NodeMeta
	NodeName     string
	BindAddr     string
	SecretFile   string
	SecretString string
	AdvertiseIP  string
	JoinAddrs    []string
	BindPort     int
	VeryVerbose  bool
}

// NewService creates a new membership service.
//
// coll, mp, and tp wire the metrics collector and OTel providers used for
// gossip/membership instrumentation. Any of them may be nil; nil disables the
// corresponding emission path.
func NewService(config Config, bus event.Bus, logger *zap.Logger, coll metrics.Collector, mp otelmetric.MeterProvider, tp trace.TracerProvider) *Service {
	return &Service{
		logger:     logger,
		bus:        bus,
		config:     config,
		transport:  config.Transport,
		nodes:      make(map[string]cluster.NodeInfo),
		nodeStates: make(map[string]memberlist.NodeStateType),
		tel:        newTelemetry(coll, mp, tp),
	}
}

// New creates a membership service with functional options.
func New(opts ...Option) *Service {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	svc := &Service{
		logger: o.logger,
		bus:    o.bus,
		config: Config{
			NodeName:     o.nodeName,
			BindAddr:     o.bindAddr,
			BindPort:     o.bindPort,
			JoinAddrs:    o.joinAddrs,
			SecretFile:   o.secretFile,
			SecretString: o.secretString,
			AdvertiseIP:  o.advertiseIP,
			VeryVerbose:  o.veryVerbose,
			Meta:         o.meta,
		},
		transport:  o.transport,
		nodes:      make(map[string]cluster.NodeInfo),
		nodeStates: make(map[string]memberlist.NodeStateType),
	}
	svc.tel = newTelemetry(o.coll, o.mp, o.tp)

	return svc
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

	// Use custom transport if provided (for testing)
	if s.transport != nil {
		mlConfig.Transport = s.transport
		s.logger.Debug("using custom transport")
	}

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
			return NewLoadSecretKeyError(err)
		}
		mlConfig.SecretKey = secretKey
		s.logger.Info("encryption enabled", zap.Int("key_size", len(secretKey)))
	} else if s.transport == nil {
		// Only warn about encryption when using real network
		s.logger.Warn("encryption disabled - no secret key provided")
	}

	// Create memberlist
	ml, err := memberlist.Create(mlConfig)
	if err != nil {
		return NewCreateMemberlistError(err)
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
			s.tel.recordJoin(err)
			return NewJoinClusterError(err)
		}

		s.logger.Info("successfully joined cluster",
			zap.Int("discovered_nodes", n))
	} else {
		s.logger.Info("starting as cluster bootstrap node")
	}

	s.tel.recordJoin(nil)

	// Log initial cluster state
	members := ml.Members()
	s.logger.Info("membership active",
		zap.String("local_node", s.config.NodeName),
		zap.Int("total_members", len(members)))

	s.refreshMemberStateGauges()

	go s.emitHealthLoop(s.ctx)

	return nil
}

// emitHealthLoop periodically samples memberlist's GetHealthScore (0 = healthy,
// larger = degraded) and emits it as a probe-duration histogram. The score is
// dimensionless but maps cleanly onto a "ms-equivalent" axis for dashboards.
func (s *Service) emitHealthLoop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.memberlist == nil {
				continue
			}

			score := s.memberlist.GetHealthScore()
			s.tel.recordProbe(nil, time.Duration(score)*time.Millisecond)
		}
	}
}

// Stop gracefully shuts down the membership service
func (s *Service) Stop() error {
	s.logger.Info("shutting down cluster membership service")

	s.tel.recordLeave()

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
	s.mu.RLock()
	meta := cloneMeta(s.config.Meta)
	s.mu.RUnlock()

	if s.memberlist == nil {
		// Return info from config if memberlist isn't up yet
		return cluster.NodeInfo{
			ID:   s.config.NodeName,
			Addr: s.config.BindAddr,
			Meta: meta,
		}
	}

	local := s.memberlist.LocalNode()
	return cluster.NodeInfo{
		ID:   local.Name,
		Addr: local.Address(),
		Meta: meta,
	}
}

// UpdateMeta merges the supplied keys into the local node's gossip metadata
// and triggers an asynchronous re-broadcast so peers observe the change.
//
// Existing keys not present in updates are preserved; supplied keys overwrite
// existing values. Empty-string values are kept (callers that want to clear a
// key should pass it explicitly).
func (s *Service) UpdateMeta(updates map[string]string) {
	if len(updates) == 0 {
		return
	}

	s.mu.Lock()
	if s.config.Meta == nil {
		s.config.Meta = make(cluster.NodeMeta, len(updates))
	}
	for k, v := range updates {
		s.config.Meta[k] = v
	}
	ml := s.memberlist
	s.mu.Unlock()

	if ml == nil {
		// memberlist not yet started — meta will be advertised on first NodeMeta() call
		return
	}

	// UpdateNode triggers a delegate.NodeMeta() call and broadcasts the new
	// meta to peers. The timeout is for waiting on local broadcast queueing,
	// not for remote ack — keep it short.
	if err := ml.UpdateNode(time.Second); err != nil {
		s.logger.Warn("memberlist UpdateNode failed", zap.Error(err))
	}
}

// cloneMeta returns a defensive copy so callers cannot mutate internal state.
func cloneMeta(m cluster.NodeMeta) cluster.NodeMeta {
	if m == nil {
		return nil
	}
	out := make(cluster.NodeMeta, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// loadSecretKey loads encryption key from file or string
func (s *Service) loadSecretKey() ([]byte, error) {
	var keyStr string

	// Load from file if specified
	switch {
	case s.config.SecretFile != "":
		data, err := os.ReadFile(s.config.SecretFile)
		if err != nil {
			return nil, NewReadSecretFileError(err)
		}
		keyStr = strings.TrimSpace(string(data))
	case s.config.SecretString != "":
		// Use string directly
		keyStr = s.config.SecretString
	default:
		return nil, ErrNoSecretKeyProvided
	}

	// Decode base64 key
	return base64.StdEncoding.DecodeString(keyStr)
}

// refreshMemberStateGauges recomputes the gossip_members gauge across the
// current memberlist view. Memberlist's Members() filters out dead/left, so
// alive/suspect counts will reflect the live view; dead and left default to
// zero unless future memberlist changes surface them here.
//
// The refresh runs asynchronously: memberlist invokes the event delegate
// hooks while holding its internal node lock, and Members() re-acquires the
// same lock — calling it inline would deadlock.
func (s *Service) refreshMemberStateGauges() {
	if s.memberlist == nil {
		return
	}

	go s.computeMemberStateGauges()
}

func (s *Service) computeMemberStateGauges() {
	if s.memberlist == nil {
		return
	}

	alive, suspect, dead, left := 0, 0, 0, 0
	for _, m := range s.memberlist.Members() {
		switch m.State {
		case memberlist.StateAlive:
			alive++
		case memberlist.StateSuspect:
			suspect++
		case memberlist.StateDead:
			dead++
		case memberlist.StateLeft:
			left++
		}
	}

	s.tel.recordMembers("alive", alive)
	s.tel.recordMembers("suspect", suspect)
	s.tel.recordMembers("dead", dead)
	s.tel.recordMembers("left", left)
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
		Addr: node.Address(), // IP:port for transport use
		Meta: ed.parseNodeMeta(node.Meta),
	}

	convergedFrom := ed.service.recordChange(node.Name, nodeInfo, node.State)

	ed.service.logger.Info("node joined",
		zap.String("node_id", node.Name),
		zap.String("address", nodeInfo.Addr),
		zap.Any("metadata", nodeInfo.Meta))

	ed.service.publishEvent(cluster.NodeJoined, nodeInfo)
	ed.service.refreshMemberStateGauges()
	ed.service.emitConvergence(convergedFrom)
}

func (ed *eventDelegate) NotifyLeave(node *memberlist.Node) {
	// Skip events for our own node
	if node.Name == ed.service.config.NodeName {
		return
	}

	nodeInfo := cluster.NodeInfo{
		ID:   node.Name,
		Addr: node.Address(), // IP:port for transport use
		Meta: ed.parseNodeMeta(node.Meta),
	}

	convergedFrom, prevState := ed.service.removeNode(node.Name)

	// Memberlist transitions a suspect into dead/left via NotifyLeave; surface
	// that as a "dead" suspicion resolution.
	if prevState == memberlist.StateSuspect {
		ed.service.tel.recordSuspicionOutcome("dead")
	}

	ed.service.logger.Info("node left",
		zap.String("node_id", node.Name),
		zap.String("address", nodeInfo.Addr))

	ed.service.publishEvent(cluster.NodeLeft, nodeInfo)
	ed.service.refreshMemberStateGauges()
	ed.service.emitConvergence(convergedFrom)
}

func (ed *eventDelegate) NotifyUpdate(node *memberlist.Node) {
	// Skip events for our own node
	if node.Name == ed.service.config.NodeName {
		return
	}

	nodeInfo := cluster.NodeInfo{
		ID:   node.Name,
		Addr: node.Address(), // IP:port for transport use
		Meta: ed.parseNodeMeta(node.Meta),
	}

	// recordChange handles suspicion->alive resolution metrics; suspicion->dead
	// is recorded from NotifyLeave (memberlist routes that transition there).
	convergedFrom := ed.service.recordChange(node.Name, nodeInfo, node.State)

	ed.service.logger.Info("node updated",
		zap.String("node_id", node.Name),
		zap.String("address", nodeInfo.Addr),
		zap.Any("metadata", nodeInfo.Meta))

	ed.service.publishEvent(cluster.NodeUpdated, nodeInfo)
	ed.service.refreshMemberStateGauges()
	ed.service.emitConvergence(convergedFrom)
}

// recordChange updates the cached node info and state for `name`, emitting a
// suspicion-resolution metric when transitioning suspect->alive. It returns
// the previous lastChangeAt timestamp so the caller can record convergence.
func (s *Service) recordChange(name string, info cluster.NodeInfo, newState memberlist.NodeStateType) time.Time {
	s.mu.Lock()
	prevState, hadPrev := s.nodeStates[name]
	s.nodes[name] = info
	s.nodeStates[name] = newState
	prevChange := s.lastChangeAt
	s.lastChangeAt = time.Now()
	s.mu.Unlock()

	if hadPrev && prevState == memberlist.StateSuspect && newState == memberlist.StateAlive {
		s.tel.recordSuspicionOutcome("alive")
	}

	return prevChange
}

// removeNode drops cached state for `name` and returns the previous
// lastChangeAt timestamp plus the node's prior state (so callers can attribute
// suspicion->dead transitions).
func (s *Service) removeNode(name string) (time.Time, memberlist.NodeStateType) {
	s.mu.Lock()
	prevState := s.nodeStates[name]
	delete(s.nodes, name)
	delete(s.nodeStates, name)
	prevChange := s.lastChangeAt
	s.lastChangeAt = time.Now()
	s.mu.Unlock()

	return prevChange, prevState
}

// emitConvergence records the inter-event delta as a convergence sample. The
// first event after Start has a zero `from` timestamp and is skipped.
func (s *Service) emitConvergence(from time.Time) {
	if from.IsZero() {
		return
	}

	s.tel.recordConvergence(time.Since(from))
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
	d.service.mu.RLock()
	meta := cloneMeta(d.service.config.Meta)
	d.service.mu.RUnlock()

	if len(meta) == 0 {
		return []byte{}
	}

	data, err := json.Marshal(meta)
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
	_, span := d.service.tel.startSpan(d.service.ctx, "gossip.broadcast",
		trace.WithAttributes(attribute.Int("bytes", len(data))),
	)
	defer span.End()

	// Handle incoming gossip messages - could be used for custom protocols
	if d.service.config.VeryVerbose {
		d.service.logger.Debug("received gossip message", zap.Int("size", len(data)))
	}

	d.service.tel.recordMessage("user", "rx", len(data))
}

func (d *delegate) GetBroadcasts(_, _ int) [][]byte {
	// Return messages to broadcast - could be used for custom state sync
	// For now, no custom broadcasts.
	return nil
}

func (d *delegate) LocalState(_ bool) []byte {
	// TODO: Can be used later for gossip state sync
	return []byte("{}")
}

func (d *delegate) MergeRemoteState(buf []byte, join bool) {
	_, span := d.service.tel.startSpan(d.service.ctx, "gossip.sync",
		trace.WithAttributes(
			attribute.Int("bytes", len(buf)),
			attribute.Bool("join", join),
		),
	)
	defer span.End()

	// Reserved for future gossip state sync; merge logic will land alongside
	// the corresponding LocalState producer.
	if len(buf) > 0 && d.service.config.VeryVerbose {
		d.service.logger.Debug("merging remote state",
			zap.Int("size", len(buf)),
			zap.Bool("join", join))
	}
}
