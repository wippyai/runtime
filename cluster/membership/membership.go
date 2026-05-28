// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
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

// UserDelegate is the contract that subsystems satisfy to ride memberlist's
// UDP user-broadcast (deltas) and TCP push/pull (state sync) channels. The
// membership Service multiplexes multiple UserDelegates over a single
// memberlist Delegate using length-prefixed frames `kind:1|len:4|payload`,
// so eventualreg, eventbus state sync, etc. can coexist.
type UserDelegate interface {
	// Kind is a stable 1-byte identifier for this delegate's frames.
	// 0 is reserved.
	Kind() byte
	// GetBroadcasts pulls outgoing deltas. Returns up to `limit` frames each
	// of size ≤ overhead. Called by memberlist on its gossip tick.
	GetBroadcasts(overhead, limit int) [][]byte
	// NotifyMsg handles one incoming delta frame (UDP user-broadcast).
	NotifyMsg(payload []byte)
	// LocalState returns the bulk-transfer payload for outgoing TCP push/pull.
	// `join` is true on the initial sync after Join().
	LocalState(join bool) []byte
	// MergeRemoteState applies a peer's bulk-transfer payload received over
	// TCP push/pull.
	MergeRemoteState(buf []byte, join bool)
}

// Service implements cluster membership using memberlist
type Service struct {
	ctx            context.Context
	bus            event.Bus
	transport      memberlist.Transport
	logger         *zap.Logger
	memberlist     *memberlist.Memberlist
	nodes          map[string]cluster.NodeInfo
	nodeStates     map[string]memberlist.NodeStateType
	tel            *telemetry
	userDelegates  map[byte]UserDelegate
	lastChangeAt   time.Time
	config         Config
	userDelegateMu sync.RWMutex
	mu             sync.RWMutex
}

// RegisterUserDelegate wires a UserDelegate so its frames are multiplexed
// on the gossip channel. Must be called before Start. Returns an error if
// the kind is 0 or already registered.
func (s *Service) RegisterUserDelegate(d UserDelegate) error {
	if d.Kind() == 0 {
		return errors.New("membership: UserDelegate kind 0 is reserved")
	}
	s.userDelegateMu.Lock()
	defer s.userDelegateMu.Unlock()
	if s.userDelegates == nil {
		s.userDelegates = make(map[byte]UserDelegate, 4)
	}
	if _, exists := s.userDelegates[d.Kind()]; exists {
		return fmt.Errorf("membership: UserDelegate kind %d already registered", d.Kind())
	}
	s.userDelegates[d.Kind()] = d
	return nil
}

// SendUserMessage delivers `payload` to `targetNodeID` over memberlist's
// reliable TCP user-message channel, wrapped with the same
// `kind:1|len:4|payload` multiplex header that the broadcast path
// uses — so the receiver's NotifyMsg dispatches it to whatever
// UserDelegate registered `kind`. Used for targeted request/response
// patterns (e.g. eventualreg's shard-pull anti-entropy) where
// broadcasting would create N² traffic. Returns an error if the
// target is not currently a known member.
func (s *Service) SendUserMessage(targetNodeID string, kind byte, payload []byte) error {
	if kind == 0 {
		return errors.New("membership: SendUserMessage kind 0 is reserved")
	}
	if s.memberlist == nil {
		return errors.New("membership: not started")
	}
	if uint64(len(payload)) > uint64(^uint32(0)) {
		return fmt.Errorf("membership: payload too large: %d", len(payload))
	}
	wrapped := make([]byte, 0, len(payload)+5)
	wrapped = append(wrapped, kind)
	n := uint32(len(payload))
	wrapped = append(wrapped, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
	wrapped = append(wrapped, payload...)
	for _, m := range s.memberlist.Members() {
		if m.Name == targetNodeID {
			return s.memberlist.SendReliable(m, wrapped)
		}
	}
	return fmt.Errorf("membership: target node %q not in member list", targetNodeID)
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
	// DefaultLocalConfig gossips every 100ms — tuned for loopback. On a real
	// multi-node cluster that is 10 mostly-empty gossip fan-outs/sec/node;
	// 500ms still propagates deltas promptly and cuts idle wakeups 5x.
	mlConfig.GossipInterval = 500 * time.Millisecond
	// DeadNodeReclaimTime lets a node that has been dead longer than this be
	// replaced by a node with the SAME name but a DIFFERENT address. This is
	// exactly the k8s StatefulSet pod-restart case: a pod is killed (hard,
	// no graceful Leave, so peers mark it StateDead at its old IP), the
	// StatefulSet recreates it under the same name with a fresh pod IP, and
	// it tries to rejoin. With the memberlist default of 0, peers reject the
	// rejoin with "Conflicting address" until the dead entry ages out via
	// GossipToTheDeadTime (~30s) — which is a large slice of post-kill
	// reconvergence latency. 3s is long enough that a brief same-IP flap
	// won't trigger a spurious reclaim, short enough that a restarted pod
	// rejoins promptly.
	mlConfig.DeadNodeReclaimTime = 3 * time.Second
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

	// Pipe memberlist logs into our zap logger. In non-verbose mode we only
	// surface [ERR] and [WARN] lines; [DEBUG]/[INFO] are dropped. ERR is
	// never silenced — discarding lines like "Failed to decode user
	// message" hides real protocol faults.
	mlConfig.LogOutput = newMemberlistLogWriter(s.logger.Named("memberlist"), s.config.VeryVerbose)

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

	// Join cluster if addresses provided.
	//
	// Retry with exponential backoff up to s.ctx cancellation. A
	// single-shot Join used to fail boot when DNS was transiently
	// unavailable — observed under DNSChaos: every seed address
	// returned "connection refused on 10.43.x.x:53" within the chaos
	// window, the boot exited with an unrecoverable error, and the
	// pod went into CrashLoopBackOff right when peers came back.
	//
	// memberlist.Join is idempotent (a re-join just discovers more
	// members), so retrying is safe. We cap the per-attempt log
	// volume by only logging the first 3 failures verbosely, then
	// switching to one log per 30s.
	if len(s.config.JoinAddrs) > 0 {
		s.logger.Info("joining existing cluster",
			zap.Strings("join_addresses", s.config.JoinAddrs))

		if err := s.joinWithRetry(s.ctx, ml); err != nil {
			s.tel.recordJoin(err)
			return NewJoinClusterError(err)
		}
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
	if len(s.config.JoinAddrs) > 0 {
		go s.rejoinLoop(s.ctx)
	}

	return nil
}

// joinWithRetry retries memberlist.Join with exponential backoff until any
// seed address succeeds or ctx is cancelled. The exit-on-ctx clause means
// boot can still bail out cleanly on shutdown signal; under chaos the
// caller passes the long-lived service ctx so retries continue until DNS
// recovers.
//
// Backoff schedule: 500ms, 1s, 2s, 4s, 8s — capped at 8s after the 5th
// attempt. Logs verbosely for the first 3 failures, then one line per
// minute so a sustained outage doesn't flood the log.
func (s *Service) joinWithRetry(ctx context.Context, ml *memberlist.Memberlist) error {
	const maxBackoff = 8 * time.Second
	const verboseAttempts = 3
	const summaryEvery = time.Minute

	backoff := 500 * time.Millisecond
	attempt := 0
	lastSummaryAt := time.Now()

	for {
		attempt++
		n, err := ml.Join(s.config.JoinAddrs)
		if err == nil {
			s.logger.Info("successfully joined cluster",
				zap.Int("discovered_nodes", n),
				zap.Int("attempt", attempt))
			return nil
		}

		switch {
		case attempt <= verboseAttempts:
			s.logger.Warn("join attempt failed; will retry",
				zap.Int("attempt", attempt),
				zap.Duration("next_backoff", backoff),
				zap.Strings("join_addresses", s.config.JoinAddrs),
				zap.Error(err))
		case time.Since(lastSummaryAt) >= summaryEvery:
			s.logger.Warn("join still failing",
				zap.Int("attempts_so_far", attempt),
				zap.Error(err))
			lastSummaryAt = time.Now()
		}
		s.tel.recordJoin(err)

		select {
		case <-ctx.Done():
			s.logger.Error("join cancelled by ctx",
				zap.Int("attempts", attempt),
				zap.Error(err))
			return err
		case <-time.After(backoff):
		}

		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// rejoinLoop watches for full peer loss and re-attempts Join when the local
// node is the only remaining member. Memberlist has no built-in auto-rejoin —
// once a node has joined and then lost all peers (e.g., they all cycled
// through chaos pod-failure simultaneously), it sits alone forever even
// after peers come back, because the peers' fresh memberlist state has no
// record of this node and cannot reach back via gossip.
//
// We probe every 30s. The reaction time is intentionally slow: under normal
// chaos, peers cycle individually and gossip catches the join/leave; the
// rejoin is only relevant for the rare "all peers gone at once" case.
func (s *Service) rejoinLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if s.memberlist == nil {
			continue
		}
		members := s.memberlist.Members()
		if len(members) > 1 {
			continue
		}
		s.logger.Warn("memberlist isolated (only self), re-attempting Join",
			zap.Strings("join_addresses", s.config.JoinAddrs))
		n, err := s.memberlist.Join(s.config.JoinAddrs)
		if err != nil {
			s.logger.Warn("rejoin attempt failed",
				zap.Error(err),
				zap.Strings("join_addresses", s.config.JoinAddrs))
			s.tel.recordJoin(err)
			continue
		}
		s.logger.Info("rejoined cluster", zap.Int("discovered_nodes", n))
		s.tel.recordJoin(nil)
		s.refreshMemberStateGauges()
	}
}

// emitHealthLoop periodically samples memberlist's GetHealthScore (0 =
// healthy, larger = degraded) and surfaces nonzero scores as
// gossip_probe_failures_total. Sampled at 5s — health degradation
// persists across ticks, so polling faster is wasted CPU + metric writes.
//
// This loop used to also synthesize gossip_message_total{kind="ping"}
// counters at one-per-member-per-second to approximate memberlist's
// internal probe traffic for dashboards. That was fabrication, not real
// measurement, and made every pod do a 1Hz metric write proportional to
// cluster size. Removed: dashboards that need real probe counts should
// instrument memberlist's transport directly.
func (s *Service) emitHealthLoop(ctx context.Context) {
	const sampleInterval = 5 * time.Second
	t := time.NewTicker(sampleInterval)
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
			if score > 0 {
				s.tel.recordProbeFailure(s.config.NodeName)
				s.tel.recordProbe(errProbeUnhealthy, 0)
			} else {
				s.tel.recordProbe(nil, 0)
			}
		}
	}
}

// errProbeUnhealthy is a sentinel passed to recordProbe when the memberlist
// health score is non-zero. It marks the histogram observation as result=err
// so dashboards can plot probe-failure rate.
var errProbeUnhealthy = errors.New("memberlist health score > 0")

// HealthScore returns the underlying memberlist health score:
// 0 means healthy, larger values indicate failed probes / suspect peers.
// Returns -1 if memberlist is not yet running.
func (s *Service) HealthScore() int {
	if s.memberlist == nil {
		return -1
	}
	return s.memberlist.GetHealthScore()
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
	ed.service.tel.recordMessage("join", "rx", len(node.Meta))
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
	ed.service.tel.recordMessage("leave", "rx", 0)
	ed.service.tel.recordLeave()
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
	ed.service.tel.recordMessage("update", "rx", len(node.Meta))
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

	if d.service.config.VeryVerbose {
		d.service.logger.Debug("received gossip message", zap.Int("size", len(data)))
	}

	d.service.tel.recordMessage("user", "rx", len(data))

	// Multiplex: kind:1 | len:4 | payload
	if len(data) < 5 {
		return
	}
	kind := data[0]
	plen := uint32(data[1]) | uint32(data[2])<<8 | uint32(data[3])<<16 | uint32(data[4])<<24
	if int(plen)+5 != len(data) {
		return
	}
	if ud := d.service.lookupUserDelegate(kind); ud != nil {
		ud.NotifyMsg(data[5:])
	}
}

func (d *delegate) GetBroadcasts(overhead, limit int) [][]byte {
	d.service.userDelegateMu.RLock()
	dels := make([]UserDelegate, 0, len(d.service.userDelegates))
	for _, ud := range d.service.userDelegates {
		dels = append(dels, ud)
	}
	d.service.userDelegateMu.RUnlock()

	// Sort by Kind for deterministic iteration. Without this, Go's
	// randomized map iteration combined with one delegate consuming the
	// entire budget produces unfair starvation patterns that depend on
	// hash bucket layout (one delegate could get 0 bytes/s while another
	// hogged the cycle).
	sort.Slice(dels, func(i, j int) bool { return dels[i].Kind() < dels[j].Kind() })

	if d.service.tel != nil && d.service.tel.coll != nil {
		d.service.tel.coll.CounterInc("gossip_user_getbroadcasts_calls_total", nil)
	}

	var out [][]byte
	totalBytes := 0
	totalCost := 0
	muxOverhead := overhead + 5

	wrap := func(ud UserDelegate, frames [][]byte) {
		for _, f := range frames {
			wrapped := make([]byte, 0, len(f)+5)
			wrapped = append(wrapped, ud.Kind())
			n := uint32(len(f))
			wrapped = append(wrapped, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
			wrapped = append(wrapped, f...)
			totalCost += len(wrapped) + overhead
			totalBytes += len(wrapped)
			out = append(out, wrapped)
		}
	}

	// Pass 1: fair share. Each delegate gets limit/N — guarantees that no
	// single delegate (e.g. eventualreg with a perpetually-full 4096-entry
	// queue) can starve the others. Inner delegates re-queue undrained
	// entries, so unused budget is not data loss.
	if n := len(dels); n > 0 {
		share := limit / n
		if share > muxOverhead {
			for _, ud := range dels {
				frames := ud.GetBroadcasts(muxOverhead, share)
				wrap(ud, frames)
			}
		}
	}

	// Pass 2: hand the remaining budget (unused share from idle delegates)
	// to whoever still has pending entries. Same iteration order; the inner
	// budget contract bounds emission to what fits the remainder. This
	// recovers throughput when one delegate is silent.
	for _, ud := range dels {
		remaining := limit - totalCost
		if remaining <= muxOverhead {
			break
		}
		frames := ud.GetBroadcasts(muxOverhead, remaining)
		wrap(ud, frames)
	}

	if d.service.tel != nil && d.service.tel.coll != nil && len(out) > 0 {
		d.service.tel.coll.CounterAdd("gossip_user_getbroadcasts_frames_total", float64(len(out)), nil)
		d.service.tel.coll.CounterAdd("gossip_user_getbroadcasts_bytes_total", float64(totalBytes), nil)
		if totalCost > limit {
			d.service.tel.coll.CounterInc("gossip_user_getbroadcasts_overshoot_total", nil)
		}
	}
	return out
}

func (d *delegate) LocalState(join bool) []byte {
	d.service.userDelegateMu.RLock()
	dels := make([]UserDelegate, 0, len(d.service.userDelegates))
	for _, ud := range d.service.userDelegates {
		dels = append(dels, ud)
	}
	d.service.userDelegateMu.RUnlock()

	if len(dels) == 0 {
		// Backwards-compat shape: prior code returned `{}`. An empty multiplex
		// stream parses as zero frames so receivers tolerate either.
		return []byte("{}")
	}

	out := make([]byte, 0, 64)
	for _, ud := range dels {
		payload := ud.LocalState(join)
		if len(payload) == 0 {
			continue
		}
		n := uint32(len(payload))
		out = append(out, ud.Kind())
		out = append(out, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
		out = append(out, payload...)
	}
	return out
}

func (d *delegate) MergeRemoteState(buf []byte, join bool) {
	_, span := d.service.tel.startSpan(d.service.ctx, "gossip.sync",
		trace.WithAttributes(
			attribute.Int("bytes", len(buf)),
			attribute.Bool("join", join),
		),
	)
	defer span.End()

	if len(buf) > 0 && d.service.config.VeryVerbose {
		d.service.logger.Debug("merging remote state",
			zap.Int("size", len(buf)),
			zap.Bool("join", join))
	}

	// Tolerate the legacy "{}" payload.
	if len(buf) == 2 && buf[0] == '{' && buf[1] == '}' {
		return
	}

	for len(buf) >= 5 {
		kind := buf[0]
		plen := uint32(buf[1]) | uint32(buf[2])<<8 | uint32(buf[3])<<16 | uint32(buf[4])<<24
		if int(plen)+5 > len(buf) {
			return
		}
		payload := buf[5 : 5+plen]
		if ud := d.service.lookupUserDelegate(kind); ud != nil {
			ud.MergeRemoteState(payload, join)
		}
		buf = buf[5+plen:]
	}
}

// lookupUserDelegate returns the registered delegate for `kind`, or nil.
func (s *Service) lookupUserDelegate(kind byte) UserDelegate {
	s.userDelegateMu.RLock()
	defer s.userDelegateMu.RUnlock()
	return s.userDelegates[kind]
}
