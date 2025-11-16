package boot

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	funcapi "github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	relayapi "github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/logs"
	transcoder "github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/payload/json"
	"github.com/wippyai/runtime/system/payload/lua"
	"github.com/wippyai/runtime/system/payload/yaml"
	"github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/topology"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

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

	dtt := transcoder.GlobalTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)
	ctx = payload.WithTranscoder(ctx, dtt)

	// Setup event infrastructure (logger with event streaming)
	ctx, logManager := createEventInfrastructure(ctx, logger, bus, cfg)

	// Setup relay infrastructure (node, router, managers)
	ctx, nodeManager := createRelayInfrastructure(ctx, bus, cfg)

	// Setup topology infrastructure
	ctx = createTopologyInfrastructure(ctx)

	// Setup hosts for message handling
	if err := createHosts(ctx, cfg); err != nil {
		return ctx, err
	}

	// Create HandlerRegistry for component event handlers
	handlerRegistry := NewHandlerRegistry()
	ctx = WithHandlerRegistry(ctx, handlerRegistry)

	// Store managers in AppContext for later Start/Stop
	ctx = logapi.WithLogManager(ctx, logManager)
	ctx = relayapi.WithNodeManager(ctx, nodeManager)

	return ctx, nil
}

// createEventInfrastructure sets up logger with event streaming
func createEventInfrastructure(ctx context.Context, logger *zap.Logger, bus event.Bus, cfg boot.Config) (context.Context, *logs.Manager) {
	wrappedLogger, logManager := wrapLogger(logger, bus, cfg)
	ctx = logapi.WithLogger(ctx, wrappedLogger)
	return ctx, logManager
}

// createRelayInfrastructure sets up relay node, router, and node manager
func createRelayInfrastructure(ctx context.Context, bus event.Bus, cfg boot.Config) (context.Context, *relay.NodeManager) {
	logger := logapi.GetLogger(ctx)

	nodeName := "local"
	if cfg != nil {
		if name := cfg.Sub("relay").GetString("node_name", ""); name != "" {
			nodeName = name
		}
	}

	node := relay.NewNode(nodeName)
	nodeManager := relay.NewNodeManager(node, bus, logger.Named("relay"))
	router := relay.NewRouter(node, nil)

	ctx = relayapi.WithNode(ctx, node)
	ctx = relayapi.WithRouter(ctx, router)

	return ctx, nodeManager
}

// createTopologyInfrastructure sets up topology and PID registry
func createTopologyInfrastructure(ctx context.Context) context.Context {
	logger := logapi.GetLogger(ctx)
	node := relayapi.GetNode(ctx)

	topo := topology.NewTopology(node)
	pidReg := topology.NewPIDRegistry(topology.PIDRegistryConfig{
		Logger: logger.Named("pid"),
	})

	ctx = topapi.WithTopology(ctx, topo)
	ctx = topapi.WithRegistry(ctx, pidReg)

	return ctx
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
	controlHost := relay.NewHost(ctx, relay.HostConfig{
		BufferSize:  supervisorBufferSize,
		WorkerCount: supervisorWorkerCount,
		Logger:      logger.Named("control"),
	})
	if err := node.RegisterHost(topapi.ControlHost, controlHost); err != nil {
		return err
	}

	// Function host for function execution
	functionsBufferSize := 1024
	functionsWorkerCount := 16
	if cfg != nil {
		functionsCfg := cfg.Sub("functions")
		functionsBufferSize = functionsCfg.GetInt("host.buffer_size", functionsBufferSize)
		functionsWorkerCount = functionsCfg.GetInt("host.worker_count", functionsWorkerCount)
	}
	funcHost := relay.NewHost(ctx, relay.HostConfig{
		BufferSize:  functionsBufferSize,
		WorkerCount: functionsWorkerCount,
		Logger:      logger.Named("functions"),
	})
	if err := node.RegisterHost(funcapi.HostID, funcHost); err != nil {
		return err
	}

	// Store function host in context (control host is not stored)
	_ = relayapi.WithHost(ctx, funcHost)

	return nil
}

// wrapLogger wraps the base logger with event streaming capabilities
func wrapLogger(logger *zap.Logger, bus event.Bus, cfg boot.Config) (*zap.Logger, *logs.Manager) {
	// Create log core that can stream to event bus
	logCore := logs.NewCore(logger.Core(), bus)
	wrappedLogger := logger.WithOptions(zap.WrapCore(func(zapcore.Core) zapcore.Core {
		return logCore
	}))

	// Extract the base logger's minimum level to preserve verbose flag behavior
	baseLevelEnabler, ok := logger.Core().(zapcore.LevelEnabler)
	baseMinLevel := zapcore.InfoLevel
	if ok {
		// Find the minimum level by checking from Debug upward
		for level := zapcore.DebugLevel; level <= zapcore.FatalLevel; level++ {
			if baseLevelEnabler.Enabled(level) {
				baseMinLevel = level
				break
			}
		}
	}

	// Parse log manager configuration
	var logConfig logapi.Config
	if cfg != nil {
		cfgSub := cfg.Sub("logmanager")
		logConfig = logapi.Config{
			PropagateDownstream: cfgSub.GetBool("propagate_downstream", true),
			StreamToEvents:      cfgSub.GetBool("stream_to_events", false),
			MinLevel:            zapcore.Level(cfgSub.GetInt("min_level", int(baseMinLevel))), //nolint:gosec // int to zapcore.Level conversion
		}
	} else {
		logConfig = logapi.Config{
			PropagateDownstream: true,
			StreamToEvents:      false,
			MinLevel:            baseMinLevel,
		}
	}

	// Create log manager for runtime control
	logManager := logs.NewManager(bus, logCore, wrappedLogger.Named("logs"), logConfig)

	return wrappedLogger, logManager
}

// StartRuntimeServices starts infrastructure services (log manager, node manager)
func StartRuntimeServices(ctx context.Context) error {
	if logManager := logapi.GetLogManager(ctx); logManager != nil {
		if err := logManager.Start(ctx); err != nil {
			return err
		}
	}

	if nodeManager := relayapi.GetNodeManager(ctx); nodeManager != nil {
		if err := nodeManager.Start(ctx); err != nil {
			return err
		}
	}

	return nil
}

// StopRuntimeServices stops infrastructure services (node manager, log manager)
func StopRuntimeServices(ctx context.Context) error {
	if nodeManager := relayapi.GetNodeManager(ctx); nodeManager != nil {
		if err := nodeManager.Stop(); err != nil {
			return err
		}
	}

	if logManager := logapi.GetLogManager(ctx); logManager != nil {
		return logManager.Stop()
	}

	return nil
}
