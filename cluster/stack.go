// SPDX-License-Identifier: MPL-2.0

// Package cluster exposes a self-contained AssembleStack helper that wires
// together membership, internode and a relay.Router with the same semantics
// the boot framework uses (boot/components/system/cluster.go), but without
// any dependency on the boot framework.
//
// The intended consumer is the pg-harness in cluster mode: a standalone
// process that needs to participate in the runtime cluster as a real peer
// (gossip + internode TCP) without going through the full boot pipeline.
//
// Boot consumers continue using boot/components/system/cluster.go directly;
// extracting that path is out of scope for this helper to keep the runtime
// startup unchanged.
package cluster

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	clusterapi "github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/payload"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/cluster/membership"
	"github.com/wippyai/runtime/system/relay"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Stack bundles the cluster networking primitives.
//
// Once Start succeeds the local node is reachable via internode TCP and
// visible to peers via the gossip layer. Stop tears the stack down in the
// reverse order. Stack is not safe for concurrent Start/Stop.
type Stack struct {
	Node       *relay.Node
	Router     *relay.Router
	Membership *membership.Service
	ConnMgr    internode.ConnectionManager
	Internode  *internode.Service

	mu      sync.Mutex
	started bool
}

// StackConfig is the input to AssembleStack.
//
// Required: NodeName, Logger, Bus, Transcoder, Collector. Everything else
// has reasonable defaults.
type StackConfig struct {
	Logger        *zap.Logger
	Bus           event.Bus
	Transcoder    payload.Transcoder
	Collector     metrics.Collector
	MeterProvider otelmetric.MeterProvider
	TraceProvider trace.TracerProvider
	// Meta is advertised over gossip alongside auto-injected `internode_port`.
	// raft_eligible="false" here keeps the node out of the voter set — used by
	// the harness to avoid disturbing the runtime's raft quorum.
	Meta clusterapi.NodeMeta

	NodeName            string
	MembershipBindAddr  string
	MembershipAdvertise string
	SecretKey           string
	SecretFile          string
	InternodeBindAddr   string
	JoinAddrs           []string
	MembershipBindPort  int
	InternodeBindPort   int
	InternodeAutoPort   bool
}

// AssembleStack constructs the relay node, router, membership service,
// internode connection manager, and internode service. It does NOT start
// them — call Stack.Start(ctx).
func AssembleStack(cfg StackConfig) (*Stack, error) {
	if cfg.NodeName == "" {
		return nil, fmt.Errorf("cluster: NodeName is required")
	}
	if cfg.Logger == nil {
		return nil, fmt.Errorf("cluster: Logger is required")
	}
	if cfg.Bus == nil {
		return nil, fmt.Errorf("cluster: Bus is required")
	}
	if cfg.Transcoder == nil {
		return nil, fmt.Errorf("cluster: Transcoder is required")
	}
	if cfg.Collector == nil {
		return nil, fmt.Errorf("cluster: Collector is required")
	}

	logger := cfg.Logger.Named("cluster")

	node := relay.NewNode(cfg.NodeName)
	codec := internode.NewMessageCodec(cfg.Transcoder)

	// Pre-start a temporary connection manager to discover the actual
	// internode port (especially under AutoPort). Mirrors the boot flow.
	mgrCfg := internode.DefaultManagerConfig()
	mgrCfg.LocalNodeID = cfg.NodeName
	mgrCfg.BindAddr = stringOr(cfg.InternodeBindAddr, "0.0.0.0")
	mgrCfg.BindPort = cfg.InternodeBindPort
	mgrCfg.AutoPort = cfg.InternodeAutoPort
	mgrCfg.Logger = logger.Named("internode.conn")

	tempMgr := internode.NewConnectionManager(mgrCfg, cfg.Collector)
	tempCtx, tempCancel := context.WithCancel(context.Background())
	if err := tempMgr.Start(tempCtx, func(_ clusterapi.NodeID, _ []byte) {}); err != nil {
		tempCancel()
		return nil, fmt.Errorf("cluster: pre-start connection manager: %w", err)
	}
	actualPort := tempMgr.GetListenPort()
	if err := tempMgr.Stop(); err != nil {
		tempCancel()
		return nil, fmt.Errorf("cluster: stop pre-start connection manager: %w", err)
	}
	tempCancel()

	// Pin the discovered port for the real manager.
	mgrCfg.BindPort = actualPort
	mgrCfg.AutoPort = false
	connMgr := internode.NewConnectionManager(mgrCfg, cfg.Collector)

	// Augment meta with the discovered port.
	meta := clusterapi.NodeMeta{}
	for k, v := range cfg.Meta {
		meta[k] = v
	}
	meta["internode_port"] = strconv.Itoa(actualPort)

	memCfg := membership.Config{
		NodeName:     cfg.NodeName,
		BindAddr:     stringOr(cfg.MembershipBindAddr, "0.0.0.0"),
		BindPort:     intOr(cfg.MembershipBindPort, 7946),
		JoinAddrs:    cfg.JoinAddrs,
		SecretFile:   cfg.SecretFile,
		SecretString: cfg.SecretKey,
		AdvertiseIP:  cfg.MembershipAdvertise,
		Meta:         meta,
	}

	memSvc := membership.NewService(
		memCfg, cfg.Bus, logger.Named("membership"),
		cfg.Collector, cfg.MeterProvider, cfg.TraceProvider,
	)

	// Package callback delivers RX-side messages locally via the relay node.
	pkgCallback := func(pkg *relayapi.Package) error {
		err := node.Send(pkg)
		if err != nil {
			// Counted by internode telemetry as
			// internode_dropped_total{reason="delivery_failed"}; keep the
			// log at DEBUG to avoid the chaos-time spam we saw on WARN.
			logger.Debug("internode delivery failed",
				zap.String("from", pkg.Target.Node),
				zap.Error(err))
		}
		return err
	}

	intSvc := internode.NewService(
		logger.Named("internode"),
		connMgr,
		codec,
		pkgCallback,
		cfg.Bus,
		memSvc,
	)

	router := relay.NewRouter(node, intSvc)

	return &Stack{
		Node:       node,
		Router:     router,
		Membership: memSvc,
		ConnMgr:    connMgr,
		Internode:  intSvc,
	}, nil
}

// Start brings up membership and internode. Order matches the boot path:
// membership first (so internode has a peer set ready), then internode.
func (s *Stack) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("cluster: stack already started")
	}

	if err := s.Membership.Start(ctx); err != nil {
		// memberlist.Create binds the gossip port BEFORE attempting Join,
		// so a Join failure leaks the port even though Start returned an
		// error. Tear membership down so a caller-side retry can re-bind.
		_ = s.Membership.Stop()
		return fmt.Errorf("cluster: start membership: %w", err)
	}
	if err := s.Internode.Start(ctx); err != nil {
		// Best effort: tear membership down so we don't leak a half-up stack.
		_ = s.Membership.Stop()
		return fmt.Errorf("cluster: start internode: %w", err)
	}
	s.started = true
	return nil
}

// Stop shuts internode down, then membership. Safe to call exactly once
// after Start.
func (s *Stack) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil
	}
	s.started = false

	if err := s.Internode.Stop(); err != nil {
		// Continue tearing membership down even on error.
		_ = s.Membership.Stop()
		return fmt.Errorf("cluster: stop internode: %w", err)
	}
	if err := s.Membership.Stop(); err != nil {
		return fmt.Errorf("cluster: stop membership: %w", err)
	}
	return nil
}

func stringOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func intOr(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
