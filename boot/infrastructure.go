// SPDX-License-Identifier: MPL-2.0

package boot

import (
	"context"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/boot"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	relayapi "github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/logs"
	syspayload "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/relay"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var peerManagerKey = &contextapi.Key{Name: "boot.peer_manager"}

// withPeerManager stores the peer manager in context.
func withPeerManager(ctx context.Context, pm *relay.PeerManager) context.Context {
	ac := contextapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	ac.With(peerManagerKey, pm)
	return ctx
}

// getPeerManager retrieves the peer manager from context.
func getPeerManager(ctx context.Context) *relay.PeerManager {
	ac := contextapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if pm, ok := ac.Get(peerManagerKey).(*relay.PeerManager); ok {
		return pm
	}
	return nil
}

// NewBootstrapContext initializes core infrastructure (AppContext, EventBus, wrapped Logger)
// BEFORE component loading. This ensures all components receive the same wrapped logger.
//
// The logger is wrapped with event streaming capabilities, allowing runtime control
// of log propagation and streaming to the event bus.
func NewBootstrapContext(logger *zap.Logger, cfg boot.Config) (context.Context, error) {
	// Create AppContext and attach config
	appCtx := contextapi.NewAppContext()
	ctx := contextapi.WithAppContext(context.Background(), appCtx)
	if cfg != nil {
		ctx = boot.WithConfig(ctx, cfg)
	}

	// Create EventBus and Transcoder
	bus := eventbus.NewBus()
	ctx = event.WithBus(ctx, bus)

	// Create AwaitService for request-response over pub-sub
	awaitSvc := eventbus.NewAwaitService(bus)
	ctx = event.WithAwaitService(ctx, awaitSvc)

	dtt := ConfigureTranscoder(ctx, syspayload.NewTranscoder())
	ctx = payload.WithTranscoder(ctx, dtt)

	// Setup event infrastructure (logger with event streaming)
	ctx, logManager := createEventInfrastructure(ctx, logger, bus, cfg)

	// Setup relay infrastructure (node, router, managers)
	ctx, nodeManager, peerManager := createRelayInfrastructure(ctx, bus, cfg)

	// Setup hosts for message handling
	if err := createHosts(ctx, cfg); err != nil {
		return ctx, err
	}

	// Create HandlerRegistry for component event handlers
	handlerRegistry := NewHandlerRegistry()
	ctx = WithHandlerRegistry(ctx, handlerRegistry)
	ctx = WithReadiness(ctx, NewReadiness())

	// Store managers in AppContext for later Start/Stop
	ctx = logapi.WithManager(ctx, logManager)
	ctx = relayapi.WithNodeManager(ctx, nodeManager)
	ctx = withPeerManager(ctx, peerManager)

	return ctx, nil
}

// createEventInfrastructure sets up logger with event streaming
func createEventInfrastructure(ctx context.Context, logger *zap.Logger, bus event.Bus, cfg boot.Config) (context.Context, *logs.Manager) {
	wrappedLogger, logManager := wrapLogger(logger, bus, cfg)
	ctx = logapi.WithLogger(ctx, wrappedLogger)
	return ctx, logManager
}

// createRelayInfrastructure sets up relay node, router, and managers
func createRelayInfrastructure(ctx context.Context, bus event.Bus, cfg boot.Config) (context.Context, *relay.NodeManager, *relay.PeerManager) {
	logger := logapi.GetLogger(ctx)

	nodeName := defaultNodeName()
	if cfg != nil {
		if name := cfg.Sub("relay").GetString("node_name", ""); name != "" {
			nodeName = name
		}
	}

	node := relay.NewNode(nodeName)
	router := relay.NewRouter(node, nil)
	nodeManager := relay.NewNodeManager(node, bus, logger.Named("relay"))
	peerManager := relay.NewPeerManager(router, bus, logger.Named("peer"))

	ctx = relayapi.WithNode(ctx, node)
	ctx = relayapi.WithRouter(ctx, router)

	return ctx, nodeManager, peerManager
}

// defaultNodeName derives a relay node identity that is stable across restarts
// yet unique per co-located instance, without persisting any state. An explicit
// WIPPY_NODE_ID / WIPPY_RELAY_NODE_NAME always wins. Otherwise the id is a UUIDv5
// of the host identity (machine-id, then hostname) combined with the instance's
// working directory: restarts of the same instance reproduce the same id, while
// other instances on the same host run from different directories and never
// collide — unlike a bare machine-id/hostname derivation that yields one shared
// id per host.
func defaultNodeName() string {
	for _, key := range []string{"WIPPY_NODE_ID", "WIPPY_RELAY_NODE_NAME"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}

	host, dir := hostIdentity(), workingDirectory()
	if host == "" && dir == "" {
		return uuid.New().String()
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("wippy-node:"+host+"\x00"+dir)).String()
}

// hostIdentity returns a stable per-host seed: the machine-id when available,
// otherwise the hostname, or "" when neither can be read.
func hostIdentity() string {
	if raw, err := os.ReadFile("/etc/machine-id"); err == nil {
		if id := strings.TrimSpace(string(raw)); id != "" {
			return id
		}
	}
	if host, err := os.Hostname(); err == nil {
		if h := strings.TrimSpace(host); h != "" {
			return h
		}
	}
	return ""
}

// workingDirectory returns the absolute working directory used to distinguish
// co-located instances, or "" when it cannot be determined.
func workingDirectory() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// createHosts sets up control and function hosts
func createHosts(ctx context.Context, cfg boot.Config) error {
	logger := logapi.GetLogger(ctx)
	node := relayapi.GetNode(ctx)

	// Control host for supervisor (monitoring and management)
	supervisorBufferSize := 1024
	supervisorWorkerCount := 16
	if cfg != nil {
		supervisorCfg := cfg.Sub("supervisor")
		supervisorBufferSize = supervisorCfg.GetInt("host.buffer_size", supervisorBufferSize)
		supervisorWorkerCount = supervisorCfg.GetInt("host.worker_count", supervisorWorkerCount)
	}
	controlHost := relay.NewMailbox(ctx,
		relay.WithBufferSize(supervisorBufferSize),
		relay.WithWorkerCount(supervisorWorkerCount),
		relay.WithLogger(logger.Named("control")),
	)
	if err := node.RegisterHost(topapi.ControlHost, controlHost); err != nil {
		return err
	}

	return nil
}

// wrapLogger wraps the base logger with event streaming capabilities
func wrapLogger(logger *zap.Logger, bus event.Bus, cfg boot.Config) (*zap.Logger, *logs.Manager) {
	// Create log core that can stream to event bus
	logCore := logs.NewCore(logger.Core(), bus)
	wrappedLogger := logger.WithOptions(zap.WrapCore(func(zapcore.Core) zapcore.Core {
		return logCore
	}))

	// Use DebugLevel as default - downstream core handles actual level filtering.
	// This ensures verbose mode works correctly after log manager starts.
	baseMinLevel := zapcore.DebugLevel

	// Parse log manager configuration
	var logConfig logapi.Config
	if cfg != nil {
		cfgSub := cfg.Sub("logmanager")
		logConfig = logapi.Config{
			PropagateDownstream: cfgSub.GetBool("propagate_downstream", true),
			StreamToEvents:      cfgSub.GetBool("stream_to_events", false),
			MinLevel:            zapcore.Level(cfgSub.GetInt("min_level", int(baseMinLevel))),
		}
	} else {
		logConfig = logapi.Config{
			PropagateDownstream: true,
			StreamToEvents:      false,
			MinLevel:            baseMinLevel,
		}
	}

	// Apply config immediately to Core so Load phase logs respect the level
	logCore.Configure(logConfig)

	// Create log manager for runtime control
	logManager := logs.NewManager(bus, logCore, wrappedLogger.Named("logs"), logConfig)

	return wrappedLogger, logManager
}

// StartRuntimeServices starts infrastructure services (log manager, node manager, peer manager, await service)
func StartRuntimeServices(ctx context.Context) error {
	if logManager := logapi.GetManager(ctx); logManager != nil {
		if err := logManager.Start(ctx); err != nil {
			return err
		}
	}

	if nodeManager := relayapi.GetNodeManager(ctx); nodeManager != nil {
		if err := nodeManager.Start(ctx); err != nil {
			return err
		}
	}

	if peerManager := getPeerManager(ctx); peerManager != nil {
		if err := peerManager.Start(ctx); err != nil {
			return err
		}
	}

	if awaitSvc := event.GetAwaitService(ctx); awaitSvc != nil {
		if err := awaitSvc.Start(ctx); err != nil {
			return err
		}
	}

	return nil
}

// StopRuntimeServices stops infrastructure services (await service, peer manager, node manager, log manager)
func StopRuntimeServices(ctx context.Context) error {
	if awaitSvc := event.GetAwaitService(ctx); awaitSvc != nil {
		if err := awaitSvc.Stop(); err != nil {
			return err
		}
	}

	if peerManager := getPeerManager(ctx); peerManager != nil {
		if err := peerManager.Stop(); err != nil {
			return err
		}
	}

	if nodeManager := relayapi.GetNodeManager(ctx); nodeManager != nil {
		if err := nodeManager.Stop(); err != nil {
			return err
		}
	}

	if logManager := logapi.GetManager(ctx); logManager != nil {
		return logManager.Stop()
	}

	return nil
}
