package boot

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/event"
	funcapi "github.com/ponyruntime/pony/api/function"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	topapi "github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/ponyruntime/pony/system/logs"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/pubsub"
	"github.com/ponyruntime/pony/system/topology"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewInfrastructure initializes core infrastructure (AppContext, EventBus, wrapped Logger)
// BEFORE component loading. This ensures all components receive the same wrapped logger.
//
// The logger is wrapped with event streaming capabilities, allowing runtime control
// of log propagation and streaming to the event bus.
func NewInfrastructure(logger *zap.Logger, cfg boot.Config) (context.Context, error) {
	// Create AppContext
	appCtx := contextapi.NewAppContext()
	ctx := contextapi.WithAppContext(context.Background(), appCtx)

	// Attach config if provided
	if cfg != nil {
		ctx = boot.WithConfig(ctx, cfg)
	}

	// Create EventBus (infrastructure, not a component)
	bus := eventbus.NewBus()
	ctx = event.WithBus(ctx, bus)

	// Initialize transcoder (infrastructure, not a component)
	dtt := transcoder.GlobalTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)
	ctx = payload.WithTranscoder(ctx, dtt)

	// Wrap logger with event streaming capabilities
	wrappedLogger, logManager := wrapLogger(logger, bus, cfg)
	ctx = logapi.WithLogger(ctx, wrappedLogger)

	// Create pubsub infrastructure (single-node by default)
	nodeName := "local"
	if cfg != nil {
		if name := cfg.Sub("pubsub").GetString("node_name", ""); name != "" {
			nodeName = name
		}
	}

	node := pubsub.NewNode(nodeName)
	nodeManager := pubsub.NewNodeManager(node, bus, wrappedLogger.Named("pubsub"))
	router := pubsub.NewRouter(node, nil)
	topo := topology.NewTopology(node)
	pidReg := topology.NewPIDRegistry(topology.PIDRegistryConfig{
		Logger: wrappedLogger.Named("pid"),
	})

	// Register control host for monitoring and management
	controlHost := pubsub.NewHost(ctx, pubsub.HostConfig{
		BufferSize:  1024,
		WorkerCount: 16,
		Logger:      wrappedLogger.Named("control"),
	})
	if err := node.RegisterHost(topapi.ControlHost, controlHost); err != nil {
		return ctx, err
	}

	// Register function host for function execution
	funcHost := pubsub.NewHost(ctx, pubsub.HostConfig{
		BufferSize:  1024,
		WorkerCount: 16,
		Logger:      wrappedLogger.Named("functions"),
	})
	if err := node.RegisterHost(funcapi.HostID, funcHost); err != nil {
		return ctx, err
	}

	ctx = pubsubapi.WithNode(ctx, node)
	ctx = pubsubapi.WithRouter(ctx, router)
	ctx = pubsubapi.WithHost(ctx, funcHost)
	ctx = topapi.WithTopology(ctx, topo)
	ctx = topapi.WithRegistry(ctx, pidReg)

	// Store managers in context for later Start/Stop
	ctx = context.WithValue(ctx, logManagerKey, logManager)
	ctx = context.WithValue(ctx, nodeManagerKey, nodeManager)

	return ctx, nil
}

// logManagerKey is used to store the log manager in context
var logManagerKey = &struct{ name string }{"logManager"}

// nodeManagerKey is used to store the node manager in context
var nodeManagerKey = &struct{ name string }{"nodeManager"}

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
			MinLevel:            zapcore.Level(cfgSub.GetInt("min_level", int(baseMinLevel))),
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

// StartInfrastructure starts infrastructure services (log manager, node manager)
func StartInfrastructure(ctx context.Context) error {
	if lm := ctx.Value(logManagerKey); lm != nil {
		if logManager, ok := lm.(*logs.Manager); ok {
			if err := logManager.Start(ctx); err != nil {
				return err
			}
		}
	}

	if nm := ctx.Value(nodeManagerKey); nm != nil {
		if nodeManager, ok := nm.(*pubsub.NodeManager); ok {
			if err := nodeManager.Start(ctx); err != nil {
				return err
			}
		}
	}

	return nil
}

// StopInfrastructure stops infrastructure services (node manager, log manager)
func StopInfrastructure(ctx context.Context) error {
	if nm := ctx.Value(nodeManagerKey); nm != nil {
		if nodeManager, ok := nm.(*pubsub.NodeManager); ok {
			if err := nodeManager.Stop(); err != nil {
				return err
			}
		}
	}

	if lm := ctx.Value(logManagerKey); lm != nil {
		if logManager, ok := lm.(*logs.Manager); ok {
			return logManager.Stop()
		}
	}

	return nil
}
