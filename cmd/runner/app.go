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
	"strconv"
	"strings"
	"time"

	"github.com/ponyruntime/pony/api/cluster"
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
	"github.com/ponyruntime/pony/cluster/internode"
	"github.com/ponyruntime/pony/cluster/membership"
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
	node       *pubsub.NodeManager
	router     pubsubapi.Receiver // The main message router for the application
	topo       *topology.Topology
	pidReg     *topology.PIDRegistry
	membership *membership.Service // cluster membership service

	// internode communication (cluster only)
	internodeService *internode.Service
	connManager      internode.ConnectionManager
	messageCodec     *internode.MessageCodec

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
	localNode := pubsub.NewNode(nodeName)

	a.node = pubsub.NewNodeManager(
		localNode,
		a.eventBus,
		a.logger.Named("pubsub"),
	)
	// Create a router that only knows about the local node
	a.router = pubsub.NewRouter(localNode, nil)

	a.topo = topology.NewTopology(a.node.Node())
	a.pidReg = topology.NewPIDRegistry(topology.PIDRegistryConfig{
		Parent: nil,
		Logger: a.logger.Named("pid"),
	})

	return nil
}

// initClusterMesh initializes mesh layer for cluster mode with internode integration
func (a *App) initClusterMesh() error {
	// Parse join addresses
	var joinAddrs []string
	if a.config.ClusterJoin != "" {
		for _, addr := range strings.Split(a.config.ClusterJoin, ",") {
			joinAddrs = append(joinAddrs, strings.TrimSpace(addr))
		}
	}

	// STEP 1: Create and PRE-START connection manager to allocate port
	a.messageCodec = internode.NewMessageCodec(a.dtt)

	connManagerConfig := internode.DefaultManagerConfig()
	connManagerConfig.LocalNodeID = cluster.NodeID(a.config.ClusterName)
	connManagerConfig.BindAddr = "0.0.0.0"
	connManagerConfig.AutoPort = true
	connManagerConfig.Logger = a.logger

	a.connManager = internode.NewConnectionManager(connManagerConfig)

	// Pre-start the connection manager to allocate the port
	// We'll use a dummy callback for now since we don't have the delivery callback yet
	tempCtx, tempCancel := context.WithCancel(context.Background())
	dummyCallback := func(nodeID cluster.NodeID, data []byte) {
		// This won't be called during port allocation
	}

	if err := a.connManager.Start(tempCtx, dummyCallback); err != nil {
		tempCancel()
		return fmt.Errorf("failed to pre-start connection manager for port allocation: %w", err)
	}

	// STEP 2: Get the actual allocated port
	actualPort := a.connManager.GetListenPort()
	a.logger.Info("Pre-allocated internode port", zap.Int("port", actualPort))

	// STEP 3: Stop the connection manager (we'll restart it later with proper callback)
	if err := a.connManager.Stop(); err != nil {
		tempCancel()
		return fmt.Errorf("failed to stop connection manager after port allocation: %w", err)
	}
	tempCancel()

	// STEP 4: Build node metadata with the correct port
	nodeMeta := cluster.NodeMeta{
		"version":        "1.0.0",
		"role":           "wippy",
		"internode_port": strconv.Itoa(actualPort), // Use the actual port!
	}

	// STEP 5: Create membership service with correct metadata
	memberConfig := membership.Config{
		NodeName:     a.config.ClusterName,
		BindAddr:     a.config.ClusterBind,
		BindPort:     a.config.ClusterPort,
		JoinAddrs:    joinAddrs,
		SecretFile:   a.config.ClusterSecretFile,
		SecretString: a.config.ClusterSecret,
		AdvertiseIP:  a.config.ClusterAdvertise,
		VeryVerbose:  a.config.VeryVerbose,
		Meta:         nodeMeta, // Metadata has correct port!
	}

	a.membership = membership.NewService(memberConfig, a.eventBus, a.logger.Named("cluster"))

	// STEP 6: Create pubsub components using the Router architecture
	a.logger.Info("Initializing pubsub router architecture")

	// Create the local-only node. It does not know about any upstreams.
	localNode := pubsub.NewNode(a.config.ClusterName)

	// The delivery callback for the internode service passes incoming packages
	// to the local node for final delivery.
	deliveryCallback := func(pkg *pubsubapi.Package) error {
		return localNode.Send(pkg)
	}

	// Create the internode service which handles communication with other nodes.
	a.internodeService = internode.NewService(
		a.logger,
		a.connManager,
		a.messageCodec,
		deliveryCallback,
		a.eventBus,
		a.membership,
	)

	// Create the router. It's the central point for sending messages.
	// It knows about the local node and the internode service for remote messages.
	a.router = pubsub.NewRouter(localNode, a.internodeService)

	// The NodeManager still manages the lifecycle of the local node.
	a.node = pubsub.NewNodeManager(
		localNode,
		a.eventBus,
		a.logger.Named("pubsub"),
	)

	// Create topology components
	a.topo = topology.NewTopology(a.node.Node())
	a.pidReg = topology.NewPIDRegistry(topology.PIDRegistryConfig{
		Parent: nil,
		Logger: a.logger.Named("pid"),
	})

	a.logger.Info("cluster mesh with internode initialized",
		zap.String("node_name", a.config.ClusterName),
		zap.Int("internode_port", actualPort),
		zap.Strings("join_addresses", joinAddrs),
		zap.Bool("encryption_enabled", a.config.ClusterSecretFile != "" || a.config.ClusterSecret != ""))

	return nil
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

	// Put both the Router and the local Node into the context.
	// The Router is the general-purpose sender.
	// The Node is for local management (e.g., registering hosts).
	appCtx = pubsubapi.WithRouter(appCtx, a.router)
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

	// Start core services IN ORDER
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

	// Start cluster services if enabled - PROPER ORDER
	if a.config.ClusterEnabled {
		// Start membership service first (with correct port metadata)
		if a.membership != nil {
			if err := a.membership.Start(appCtx); err != nil {
				a.cancel()
				return fmt.Errorf("failed to start membership service: %w", err)
			}
		}

		// Start internode service after membership (will restart connection manager properly)
		if a.internodeService != nil {
			if err := a.internodeService.Start(appCtx); err != nil {
				a.cancel()
				return fmt.Errorf("failed to start internode service: %w", err)
			}
		}
	}

	// Start node mesh after cluster services
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

	// Stop services in REVERSE order of startup
	if err := a.eventRouter.Stop(); err != nil {
		a.logger.Error("failed to stop router", zap.Error(err))
	}

	if err := a.supervisor.Stop(); err != nil {
		a.logger.Error("failed to stop supervisor", zap.Error(err))
	}

	if err := a.node.Stop(); err != nil {
		a.logger.Error("failed to stop node", zap.Error(err))
	}

	// Stop cluster services in reverse order
	if a.config.ClusterEnabled {
		// Stop internode service before membership
		if a.internodeService != nil {
			if err := a.internodeService.Stop(); err != nil {
				a.logger.Error("failed to stop internode service", zap.Error(err))
			}
		}

		// Stop membership service last
		if a.membership != nil {
			if err := a.membership.Stop(); err != nil {
				a.logger.Error("failed to stop membership service", zap.Error(err))
			}
		}
	}

	if err := a.contractRegistry.Stop(); err != nil {
		a.logger.Error("failed to stop contract registry", zap.Error(err))
	}

	if err := a.hosts.Stop(); err != nil {
		a.logger.Error("failed to stop hosts registry", zap.Error(err))
	}

	if err := a.prototypes.Stop(); err != nil {
		a.logger.Error("failed to stop prototype registry", zap.Error(err))
	}

	if err := a.funcs.Stop(); err != nil {
		a.logger.Error("failed to stop function executor", zap.Error(err))
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
