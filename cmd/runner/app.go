package main

import (
	"context"
	"errors"
	"fmt"
	iofs "io/fs"
	httpbase "net/http"
	"net/http/pprof"
	"os"
	"runtime/debug"
	"strings"
	"time"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/contract"
	"github.com/ponyruntime/pony/api/event"
	fsapi "github.com/ponyruntime/pony/api/fs"
	funcapi "github.com/ponyruntime/pony/api/function"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	procapi "github.com/ponyruntime/pony/api/process"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	regapi "github.com/ponyruntime/pony/api/registry"
	resourceapi "github.com/ponyruntime/pony/api/resource"
	secapi "github.com/ponyruntime/pony/api/security"
	topapi "github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/embed"
	contractsys "github.com/ponyruntime/pony/system/contract"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/ponyruntime/pony/system/fs"
	"github.com/ponyruntime/pony/system/function"
	"github.com/ponyruntime/pony/system/logs"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/process"
	"github.com/ponyruntime/pony/system/pubsub"
	"github.com/ponyruntime/pony/system/registry"
	"github.com/ponyruntime/pony/system/registry/history"
	"github.com/ponyruntime/pony/system/registry/loader"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	"github.com/ponyruntime/pony/system/registry/runner"
	regtop "github.com/ponyruntime/pony/system/registry/topology"
	"github.com/ponyruntime/pony/system/resource"
	"github.com/ponyruntime/pony/system/security"
	"github.com/ponyruntime/pony/system/supervisor"
	"github.com/ponyruntime/pony/system/topology"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	// supported dbs
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

type App struct {
	ctx         context.Context
	cancel      context.CancelFunc
	config      *Config
	logger      *zap.Logger
	logCore     logapi.Core
	logManager  *logs.Manager
	eventBus    event.Bus
	eventRouter *eventbus.EventRouter
	security    *security.PolicyRegistry
	services    eventbus.RouterOption
	dtt         *transcoder.Transcoder
	reg         regapi.Registry
	supervisor  *supervisor.Supervisor
	funcs       *function.Registry
	processes   *process.Manager
	prototypes  *process.PrototypeRegistry
	hosts       *process.HostRegistry
	fsRegistry  *fs.Registry
	resources   *resource.Registry

	// contract system
	contractRegistry     *contractsys.ContractRegistry
	contractInstantiator *contractsys.Instantiator

	// mesh layer
	node   *pubsub.NodeManager
	topo   *topology.Topology
	pidReg *topology.PIDRegistry

	shuttingDown  bool
	forceShutdown chan struct{}
}

func NewApp(config *Config) (*App, error) {
	// Set memory limit only if GOMEMLIMIT is not already set
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(1 * 1024 * 1024 * 1024) // 1GB
	}

	appCtx, cancel := context.WithCancel(context.Background())

	// Initialize event bus
	bus := eventbus.NewBus()

	// Initialize logger
	l, core := initLogger(config.Verbose, config.VeryVerbose, bus)
	if l == nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize logger")
	}
	appLogger := l.Named("")

	level := zapcore.InfoLevel
	if config.Verbose || config.VeryVerbose {
		level = zapcore.DebugLevel
	}

	// Initialize log manager
	logManager := logs.NewManager(bus, core, l.Named("logs"), level)

	// Initialize transcoder
	dtt := transcoder.GlobalTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)

	app := &App{
		ctx:           appCtx,
		cancel:        cancel,
		config:        config,
		logger:        appLogger,
		logCore:       core,
		logManager:    logManager,
		eventBus:      bus,
		services:      nil,
		dtt:           dtt,
		forceShutdown: make(chan struct{}),
	}

	// Initialize mesh layer based on cluster mode
	if config.ClusterEnabled {
		if err := app.initClusterMesh(); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to initialize cluster mesh: %w", err)
		}
	} else {
		if err := app.initSingleNodeMesh(); err != nil {
			cancel()
			return nil, fmt.Errorf("failed to initialize single node mesh: %w", err)
		}
	}

	return app, nil
}

// initSingleNodeMesh initializes mesh layer for single node mode
func (a *App) initSingleNodeMesh() error {
	nodeName := a.config.ClusterName
	a.node = pubsub.NewNodeManager(
		pubsub.NewNode(nodeName, nil), // no upstream
		a.eventBus,
		a.logger.Named("pubsub"),
	)

	a.topo = topology.NewTopology(a.node.Node())
	a.pidReg = topology.NewPIDRegistry(topology.PIDRegistryConfig{
		Parent: nil,
		Logger: a.logger.Named("pid"),
	})

	return nil
}

// initClusterMesh initializes mesh layer for cluster mode
func (a *App) initClusterMesh() error {
	// TODO: Implement cluster mesh initialization
	// For now, fallback to single node mesh
	a.logger.Info("cluster mode requested but not implemented yet, using single node mesh")
	return a.initSingleNodeMesh()
}

func (a *App) Initialize() error {
	// Start log manager first for proper logging
	if err := a.logManager.Start(a.ctx); err != nil {
		return fmt.Errorf("failed to start log manager: %w", err)
	}

	a.security = security.NewPolicyRegistry(a.eventBus, a.logger.Named("security"))
	if err := a.security.Start(a.ctx); err != nil {
		return fmt.Errorf("failed to start security manager: %w", err)
	}

	// Initialize core components
	a.reg = registry.NewRegistry(
		history.NewMemory(),
		runner.NewBusRunner(a.eventBus, a.logger.Named("runner")),
		regtop.NewStateBuilder(a.logger),
		a.logger.Named("registry"),
	)

	a.supervisor = supervisor.NewSupervisor(a.eventBus, a.logger.Named("core"))

	// Initialize mesh hosts
	// Control host for internal control messages
	err := a.node.Node().RegisterHost(topapi.ControlHost, pubsub.NewHost(a.ctx, pubsub.HostConfig{
		BufferSize:  1024,
		WorkerCount: 16,
		Logger:      a.logger.Named("control"),
	}))
	if err != nil {
		return fmt.Errorf("failed to register control host: %w", err)
	}

	// Function host for function execution
	funcHost := pubsub.NewHost(a.ctx, pubsub.HostConfig{
		BufferSize:  1024,
		WorkerCount: 16,
		Logger:      a.logger.Named("functions"),
	})
	err = a.node.Node().RegisterHost(funcapi.HostID, funcHost)
	if err != nil {
		return fmt.Errorf("failed to register function host: %w", err)
	}

	// Initialize filesystem registry
	a.fsRegistry = fs.NewFSRegistry(a.eventBus, a.logger.Named("fs"))

	// Initialize core registries
	a.funcs = function.NewFunctionRegistry(a.eventBus, funcHost, a.logger.Named("funcs"))
	a.prototypes = process.NewPrototypeFactory(a.eventBus, a.logger.Named("prototypes"))
	a.hosts = process.NewHostRegistry(a.eventBus, a.logger.Named("hosts"))
	a.resources = resource.NewResourceRegistry(a.eventBus, a.logger.Named("resources"))

	a.processes = process.NewProcessManager(
		a.hosts,
		a.prototypes,
		a.node.Node().ID(), // use node ID for PID generation
		a.logger.Named("processes"),
	)

	// Initialize contract system
	a.contractRegistry = contractsys.NewContractRegistry(a.eventBus, a.logger.Named("contracts"))
	a.contractInstantiator = contractsys.NewContractInstantiator(a.contractRegistry, a.funcs)

	return nil
}

func (a *App) Start(folderPath string, useEmbed bool) error {
	// Build context with all required services
	appCtx := a.ctx
	appCtx = event.WithBus(appCtx, a.eventBus)
	appCtx = secapi.WithRegistry(appCtx, a.security)
	appCtx = fsapi.WithFSRegistry(appCtx, a.fsRegistry)
	appCtx = regapi.WithRegistry(appCtx, a.reg)
	appCtx = payload.WithTranscoder(appCtx, a.dtt)
	appCtx = funcapi.WithFunctions(appCtx, a.funcs)
	appCtx = procapi.WithProcesses(appCtx, a.processes)
	appCtx = resourceapi.WithResources(appCtx, a.resources)
	appCtx = pubsubapi.WithNode(appCtx, a.node.Node())
	appCtx = topapi.WithTopology(appCtx, a.topo)
	appCtx = topapi.WithPIDRegistry(appCtx, a.pidReg)
	appCtx = logapi.WithLogger(appCtx, a.logger)
	appCtx = contract.WithServices(appCtx, a.contractRegistry, a.contractInstantiator)

	// Add environment context
	envCtx := ctxapi.NewContexter[string]()
	for _, en := range os.Environ() {
		pair := strings.SplitN(en, "=", 2)
		if len(pair) == 2 {
			envCtx.SetValue(pair[0], pair[1])
		}
	}
	appCtx = context.WithValue(appCtx, ctxapi.EnvCtx, envCtx)

	// Start core services
	if err := a.fsRegistry.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start filesystem service: %w", err)
	}

	if err := a.resources.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start resource service: %w", err)
	}

	if err := a.funcs.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start function executor: %w", err)
	}

	if err := a.prototypes.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start prototype registry: %w", err)
	}

	if err := a.hosts.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start host registry: %w", err)
	}

	if err := a.contractRegistry.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start contract registry: %w", err)
	}

	if err := a.node.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start node mesh: %w", err)
	}

	if err := a.supervisor.Start(appCtx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start supervisor: %w", err)
	}

	// Start service router
	router, err := eventbus.StartRouter(appCtx, a.eventBus, a.services)
	if err != nil {
		a.cancel()
		return fmt.Errorf("failed to create event router: %w", err)
	}
	a.eventRouter = router

	// Load filesystem
	var fSys iofs.FS
	if useEmbed {
		fSys, err = iofs.Sub(embed.FS(), folderPath)
		if err != nil {
			a.cancel()
			return fmt.Errorf("open embedded sub-filesystem (use . to open from root): %w", err)
		}
	} else {
		osRoot, err := os.OpenRoot(folderPath)
		if err != nil {
			a.cancel()
			return fmt.Errorf("open folder %s: %w", folderPath, err)
		}
		fSys = osRoot.FS()
	}

	// Load and apply application state
	bootCtx, cancel := context.WithTimeout(appCtx, 300*time.Second)
	defer cancel()

	appState, err := loadApplicationState(bootCtx, fSys, a.dtt, a.logger)
	if err != nil {
		a.cancel()
		return fmt.Errorf("load application state: %w", err)
	}

	if _, err := a.reg.Apply(bootCtx, appState); err != nil {
		return fmt.Errorf("failed to apply initial state: %w", err)
	}

	return nil
}

func (a *App) Stop() error {
	a.shuttingDown = true

	// Create shutdown context with timeout
	cancelCtx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()

	// Handle force shutdown
	go func() {
		select {
		case <-a.forceShutdown:
			a.logger.Warn("force shutdown triggered, canceling context")
			a.cancel()
		case <-cancelCtx.Done():
		}
	}()

	// Stop services in reverse order
	if err := a.eventRouter.Stop(); err != nil {
		a.logger.Error("failed to stop router", zap.Error(err))
	}

	if err := a.supervisor.Stop(); err != nil {
		a.logger.Error("failed to stop supervisor", zap.Error(err))
	}

	if err := a.funcs.Stop(); err != nil {
		a.logger.Error("failed to stop function executor", zap.Error(err))
	}

	if err := a.prototypes.Stop(); err != nil {
		a.logger.Error("failed to stop prototype registry", zap.Error(err))
	}

	if err := a.contractRegistry.Stop(); err != nil {
		a.logger.Error("failed to stop contract registry", zap.Error(err))
	}

	if err := a.node.Stop(); err != nil {
		a.logger.Error("failed to stop node", zap.Error(err))
	}

	if err := a.hosts.Stop(); err != nil {
		a.logger.Error("failed to stop hosts registry", zap.Error(err))
	}

	if err := a.resources.Stop(); err != nil {
		a.logger.Error("failed to stop resource service", zap.Error(err))
	}

	if err := a.fsRegistry.Stop(); err != nil {
		a.logger.Error("failed to stop filesystem registry", zap.Error(err))
	}

	if err := a.security.Stop(); err != nil {
		a.logger.Error("failed to stop security manager", zap.Error(err))
	}

	// Stop log manager last
	if err := a.logManager.Stop(); err != nil {
		a.logger.Error("failed to stop log manager", zap.Error(err))
	}

	// Clean up
	a.cancel()
	_ = a.logger.Sync()

	return nil
}

// StartProfiler enables the pprof profiler server
func (a *App) StartProfiler() {
	go func() {
		profilerAddr := "localhost:6060"
		a.logger.Info("starting pprof server", zap.String("address", profilerAddr))

		mux := httpbase.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		//nolint:gosec // ok for now
		if err := httpbase.ListenAndServe(profilerAddr, mux); err != nil {
			if !errors.Is(err, httpbase.ErrServerClosed) {
				a.logger.Error("pprof server failed", zap.Error(err))
			}
		}
	}()
}

func initLogger(verbose, veryVerbose bool, bus event.Bus) (*zap.Logger, logapi.Core) {
	cfg := zap.NewDevelopmentConfig()

	switch {
	case veryVerbose:
		cfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	case verbose:
		cfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		cfg.DisableStacktrace = true
	default:
		cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
		cfg.DisableStacktrace = true
	}

	cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.DateTime)

	log, err := cfg.Build()
	if err != nil {
		fmt.Printf("Failed to build logger: %v\n", err)
		return nil, nil
	}

	core := logs.NewCore(log.Core(), bus)
	return zap.New(core), core
}

func loadApplicationState(
	ctx context.Context,
	fs iofs.FS,
	dtt *transcoder.Transcoder,
	mainLogger *zap.Logger,
) (regapi.ChangeSet, error) {
	folderLoader := loader.NewLoader(dtt, mainLogger, interpolate.NewEntryInterpolator(dtt,
		interpolate.WithInterpolator(interpolate.LoadVars),
		interpolate.WithInterpolator(interpolate.LoadFile),
	))

	vars := interpolate.Variables{}
	for _, en := range os.Environ() {
		pair := strings.SplitN(en, "=", 2)
		vars[pair[0]] = pair[1]
	}

	entries, err := folderLoader.LoadFS(fs, vars)
	if err != nil {
		return nil, fmt.Errorf("load entries: %w", err)
	}

	// Module registry loader
	baseURL := "https://modules.wippy.ai"
	if modulesURL := os.Getenv("WIPPY_MODULES_URL"); modulesURL != "" {
		baseURL = modulesURL
	}

	registryLoader := newModuleloaderManager(baseURL, entries, mainLogger.Named("registry-loader"))

	if err := registryLoader.Load(ctx); err != nil {
		mainLogger.Error("load modules from registry", zap.Error(err))
	} else {
		vendorDir, err := os.OpenRoot("vendor")
		if err != nil {
			return nil, fmt.Errorf("open vendor folder: %w", err)
		}

		dependencyEntries, err := folderLoader.LoadFS(vendorDir.FS(), vars)
		if err != nil {
			return nil, fmt.Errorf("load dependencies: %w", err)
		}
		entries = append(entries, dependencyEntries...)
	}

	boot, err := regtop.NewStateBuilder(mainLogger).BuildDelta(regapi.State{}, entries)
	if err != nil {
		return nil, fmt.Errorf("build state delta: %w", err)
	}

	return boot, nil
}
