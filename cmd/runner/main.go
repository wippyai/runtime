package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
	"github.com/ponyruntime/pony/runtime/lua/component"
	"github.com/ponyruntime/pony/runtime/lua/modules/ctx"
	"github.com/ponyruntime/pony/runtime/lua/modules/events"
	"github.com/ponyruntime/pony/runtime/lua/modules/exec"
	securitymod "github.com/ponyruntime/pony/runtime/lua/modules/security"
	"github.com/ponyruntime/pony/runtime/lua/modules/store"
	luatemplate "github.com/ponyruntime/pony/runtime/lua/modules/template"
	yamlmod "github.com/ponyruntime/pony/runtime/lua/modules/yaml"
	native "github.com/ponyruntime/pony/service/exec"
	"github.com/ponyruntime/pony/service/http/cors"
	"github.com/ponyruntime/pony/service/http/firewall"
	"github.com/ponyruntime/pony/service/processfunc"
	"github.com/ponyruntime/pony/service/template"
	"github.com/ponyruntime/pony/service/tokenstore"
	httpbase "net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/event"
	fsapi "github.com/ponyruntime/pony/api/fs"
	funcapi "github.com/ponyruntime/pony/api/function"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	procapi "github.com/ponyruntime/pony/api/process"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	regapi "github.com/ponyruntime/pony/api/registry"
	resourceapi "github.com/ponyruntime/pony/api/resource"
	luaapi "github.com/ponyruntime/pony/api/runtime/lua"
	secapi "github.com/ponyruntime/pony/api/security"
	topapi "github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/command"
	bteaapp "github.com/ponyruntime/pony/runtime/lua/component/btea"
	funclua "github.com/ponyruntime/pony/runtime/lua/component/function"
	"github.com/ponyruntime/pony/runtime/lua/component/library"
	proclua "github.com/ponyruntime/pony/runtime/lua/component/process"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	"github.com/ponyruntime/pony/runtime/lua/engine/upstream"
	"github.com/ponyruntime/pony/runtime/lua/modules/base64"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea"
	"github.com/ponyruntime/pony/runtime/lua/modules/cloudstorage"
	"github.com/ponyruntime/pony/runtime/lua/modules/crypto"
	"github.com/ponyruntime/pony/runtime/lua/modules/env"
	"github.com/ponyruntime/pony/runtime/lua/modules/excel"
	fsmod "github.com/ponyruntime/pony/runtime/lua/modules/fs"
	"github.com/ponyruntime/pony/runtime/lua/modules/funcmod"
	fncallmod "github.com/ponyruntime/pony/runtime/lua/modules/funcs"
	"github.com/ponyruntime/pony/runtime/lua/modules/hash"
	httpapimod "github.com/ponyruntime/pony/runtime/lua/modules/http"
	"github.com/ponyruntime/pony/runtime/lua/modules/httpclient"
	jsonmod "github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/ponyruntime/pony/runtime/lua/modules/logger"
	"github.com/ponyruntime/pony/runtime/lua/modules/ostime"
	payloadmod "github.com/ponyruntime/pony/runtime/lua/modules/payload"
	processmod "github.com/ponyruntime/pony/runtime/lua/modules/process"
	processmodapi "github.com/ponyruntime/pony/runtime/lua/modules/processmod"
	registrymod "github.com/ponyruntime/pony/runtime/lua/modules/registry"
	sqlmod "github.com/ponyruntime/pony/runtime/lua/modules/sql"
	timemod "github.com/ponyruntime/pony/runtime/lua/modules/time"
	"github.com/ponyruntime/pony/runtime/lua/modules/treesitter"
	"github.com/ponyruntime/pony/runtime/lua/modules/uuid"
	"github.com/ponyruntime/pony/runtime/lua/modules/websocket"
	"github.com/ponyruntime/pony/runtime/lua/task"
	"github.com/ponyruntime/pony/runtime/noop"
	"github.com/ponyruntime/pony/service/aws/config"
	"github.com/ponyruntime/pony/service/aws/s3"
	fsdir "github.com/ponyruntime/pony/service/directory"
	prochost "github.com/ponyruntime/pony/service/host"
	"github.com/ponyruntime/pony/service/http"
	"github.com/ponyruntime/pony/service/http/websocket_relay"
	"github.com/ponyruntime/pony/service/memstore"
	"github.com/ponyruntime/pony/service/policy"
	"github.com/ponyruntime/pony/service/sql"
	service "github.com/ponyruntime/pony/service/supervisor"
	"github.com/ponyruntime/pony/service/terminal"
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
	reghandler "github.com/ponyruntime/pony/system/registry/events"
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

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

type App struct {
	ctx         context.Context
	cancel      context.CancelFunc
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

	// mesh
	node   *pubsub.NodeManager
	topo   *topology.Topology
	pidReg *topology.PIDRegistry

	shuttingDown  bool
	forceShutdown chan struct{}
}

func NewApp(verbose, veryVerbose bool) (*App, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize event bus
	bus := eventbus.NewBus()

	// Initialize logger
	l, core := initLogger(verbose, veryVerbose, bus)
	if l == nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize logger")
	}
	appLogger := l.Named("")

	level := zapcore.InfoLevel
	if verbose || veryVerbose {
		level = zapcore.DebugLevel
	}

	// Initialize log manager
	logManager := logs.NewManager(bus, core, l.Named("logs"), level)

	// Initialize transcoder
	dtt := transcoder.GlobalTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)

	hostname, err := os.Hostname()
	if err != nil {
		cancel()
		return nil, err
	}

	// mesh layer: our node
	node := pubsub.NewNodeManager(
		pubsub.NewNode(hostname, nil), // no upstream for now
		bus,
		appLogger.Named("pubsub"),
	)

	topo := topology.NewTopology(node.Node())
	pidReg := topology.NewPIDRegistry(topology.PIDRegistryConfig{
		Parent: nil,
		Logger: appLogger.Named("pid"),
	})

	app := &App{
		ctx:           ctx,
		cancel:        cancel,
		logger:        appLogger,
		logCore:       core,
		logManager:    logManager,
		eventBus:      bus,
		services:      nil,
		dtt:           dtt,
		node:          node,
		topo:          topo,
		pidReg:        pidReg,
		forceShutdown: make(chan struct{}),
	}

	return app, nil
}

func (a *App) Initialize() error {
	debug.SetTraceback("single")

	// LaunchProcess log manager first for proper logging
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

	// -- msg hosts

	// this is host dedicated to internal control messages
	err := a.node.Node().RegisterHost(topapi.ControlHost, pubsub.NewHost(a.ctx, pubsub.HostConfig{
		BufferSize:  1024,
		WorkerCount: 16,
		Logger:      a.logger.Named("control"),
	}))

	if err != nil {
		return fmt.Errorf("failed to register control host: %w", err)
	}

	// this is host dedicated to internal control messages
	funcHost := pubsub.NewHost(a.ctx, pubsub.HostConfig{
		BufferSize:  1024,
		WorkerCount: 16,
		Logger:      a.logger.Named("functions"),
	})

	err = a.node.Node().RegisterHost(funcapi.HostID, funcHost)

	if err != nil {
		return fmt.Errorf("failed to register function host: %w", err)
	}

	// -- end of msg hosts

	// fs
	a.fsRegistry = fs.NewFSRegistry(a.eventBus, a.logger.Named("fs"))

	// Initialize core function registry
	a.funcs = function.NewFunctionRegistry(a.eventBus, funcHost, a.logger.Named("funcs"))
	a.prototypes = process.NewPrototypeFactory(a.eventBus, a.logger.Named("prototypes"))
	a.hosts = process.NewHostRegistry(a.eventBus, a.logger.Named("hosts"))
	a.resources = resource.NewResourceRegistry(a.eventBus, a.logger.Named("resources"))

	a.processes = process.NewProcessManager(
		a.hosts,
		a.prototypes,
		a.node.Node().ID(), // for pid generation of managed processes
		a.logger.Named("processes"),
	)

	return nil
}

func (a *App) Start(folderPath string) error {
	// Spawn context with values
	ctx := a.ctx
	ctx = event.WithBus(ctx, a.eventBus)
	ctx = secapi.WithRegistry(ctx, a.security)
	ctx = fsapi.WithFSRegistry(ctx, a.fsRegistry)
	ctx = regapi.WithRegistry(ctx, a.reg)
	ctx = payload.WithTranscoder(ctx, a.dtt)
	ctx = funcapi.WithFunctions(ctx, a.funcs)
	ctx = procapi.WithProcesses(ctx, a.processes)
	ctx = resourceapi.WithResources(ctx, a.resources)
	ctx = pubsubapi.WithNode(ctx, a.node.Node())
	ctx = topapi.WithTopology(ctx, a.topo)
	ctx = topapi.WithPIDRegistry(ctx, a.pidReg)
	ctx = logapi.WithLogger(ctx, a.logger)

	// Spawn environment context
	envCtx := ctxapi.NewContexter[string]()
	for _, en := range os.Environ() {
		pair := strings.SplitN(en, "=", 2)
		if len(pair) == 2 {
			envCtx.SetValue(pair[0], pair[1])
		}
	}
	ctx = context.WithValue(ctx, ctxapi.EnvCtx, envCtx)

	if err := a.fsRegistry.Start(ctx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start filesystem service: %w", err)
	}

	if err := a.resources.Start(ctx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start resource service: %w", err)
	}

	// LaunchProcess core function registry
	if err := a.funcs.Start(ctx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start function executor: %w", err)
	}

	if err := a.prototypes.Start(ctx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start prototype registry: %w", err)
	}

	if err := a.hosts.Start(ctx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start host registry: %w", err)
	}

	if err := a.node.Start(ctx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start node mesh: %w", err)
	}

	// LaunchProcess supervisor
	if err := a.supervisor.Start(ctx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start supervisor: %w", err)
	}

	// LaunchProcess secondary services
	router, err := eventbus.StartRouter(ctx, a.eventBus, a.services)
	if err != nil {
		a.cancel()
		return fmt.Errorf("failed to create event router: %w", err)
	}
	a.eventRouter = router

	// Load and apply initial state
	appState, err := loadApplicationState(folderPath, a.dtt, a.logger)
	if err != nil {
		a.cancel()
		return fmt.Errorf("failed to load application state: %w", err)
	}

	bootCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	if _, err := a.reg.Apply(bootCtx, appState); err != nil {
		return fmt.Errorf("failed to apply initial state: %w", err)
	}

	return nil
}

func (a *App) Stop() error {
	a.shuttingDown = true

	// Spawn shutdown context with timeout
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()

	// LaunchProcess a goroutine to handle force shutdown
	go func() {
		select {
		case <-a.forceShutdown:
			a.logger.Warn("force shutdown triggered, canceling context")
			a.cancel()
		case <-ctx.Done():
		}
	}()

	// close services in reverse order
	if err := a.eventRouter.Stop(); err != nil {
		a.logger.Error("failed to stop router", zap.Error(err))
	}

	// close supervisor
	if err := a.supervisor.Stop(); err != nil {
		a.logger.Error("failed to stop supervisor", zap.Error(err))
	}

	// Functions
	if err := a.funcs.Stop(); err != nil {
		a.logger.Error("failed to stop function executor", zap.Error(err))
	}

	if err := a.prototypes.Stop(); err != nil {
		a.logger.Error("failed to stop prototype registry", zap.Error(err))
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

	// close log manager last
	if err := a.logManager.Stop(); err != nil {
		a.logger.Error("failed to stop log manager", zap.Error(err))
	}

	// Cancel main context and clean up
	a.cancel()
	_ = a.logger.Sync()

	return nil
}

// AddCleanup this method to your App struct
func (a *App) StartProfiler() {
	// Memory profiling
	runtime.MemProfileRate = 1 // Profile all allocations

	// Create directory for profiles if it doesn't exist
	if err := os.MkdirAll("profiles", 0755); err != nil {
		a.logger.Error("failed to create profiles directory", zap.Error(err))
		return
	}

	// CPU profiling
	cpuFile, err := os.Create("profiles/cpu.prof")
	if err != nil {
		a.logger.Error("failed to create CPU profile", zap.Error(err))
		return
	}
	err = pprof.StartCPUProfile(cpuFile)
	if err != nil {
		a.logger.Error("failed to start CPU profile", zap.Error(err))
		return
	}
	defer pprof.StopCPUProfile()

	// Periodic heap profiling
	go func() {
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()

		for i := 1; ; i++ {
			select {
			case <-a.ctx.Done():
				return
			case <-tick.C:
				heapFile, err := os.Create(fmt.Sprintf("profiles/heap_%d.prof", i))
				if err != nil {
					a.logger.Error("failed to create heap profile", zap.Error(err))
					continue
				}
				wErr := pprof.WriteHeapProfile(heapFile)
				if wErr != nil {
					a.logger.Error("failed to write heap profile", zap.Error(err))
				}
				cErr := heapFile.Close()
				if cErr != nil {
					a.logger.Error("failed to close heap profile file", zap.Error(err))
				}
			}
		}
	}()

	// HTTP server for live profiling
	go func() {
		profilerAddr := "localhost:6060"
		a.logger.Info("starting pprof server", zap.String("address", profilerAddr))
		if err := httpbase.ListenAndServe(profilerAddr, nil); err != nil {
			if !errors.Is(err, httpbase.ErrServerClosed) {
				a.logger.Error("pprof server failed", zap.Error(err))
			}
		}
	}()
}

// loadDotEnv loads environment variables from .env files
func loadDotEnv(logger *zap.Logger, paths ...string) {
	// First try explicitly provided paths
	for _, path := range paths {
		envPath := filepath.Join(path, ".env")
		err := godotenv.Load(envPath)
		if err == nil {
			if logger != nil {
				logger.Info(".env file loaded successfully", zap.String("path", envPath))
			} else {
				fmt.Printf(".env file loaded successfully from: %s\n", envPath)
			}
			return // Found and loaded a .env file, no need to try others
		}
	}

	// If no specific paths provided or none worked, try the default location
	err := godotenv.Load()
	if err != nil {
		logger.Debug("Could not load .env file from default location", zap.Error(err))
	} else {
		logger.Info(".env file loaded successfully from default location")
	}
}

func main() {
	sqlite_vec.Auto()
	debug.SetMemoryLimit(500 * 1024 * 1024) // 500mb

	// Parse command line flags
	verbose := flag.Bool("v", false, "enable verbose debug logging")
	veryVerbose := flag.Bool("vv", false, "enable very verbose debug logging with stack traces")
	enableProfiling := flag.Bool("p", false, "enable performance profiling")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: go run main.go [-v] [-vv] [-p] <folder_path> [namespace]")
		os.Exit(1)
	}

	folderPath := args[0]

	// Spawn and initialize application
	app, err := NewApp(
		*verbose,
		*veryVerbose,
	)
	if err != nil {
		fmt.Printf("Failed to create application: %v\n", err)
		os.Exit(1)
	}

	// Load environment variables from .env files
	loadDotEnv(app.logger)

	if err := app.Initialize(); err != nil {
		fmt.Printf("Failed to initialize application: %v\n", err)
		os.Exit(1)
	}

	// ------ This is main service initiation point ------
	app.services = eventbus.WithHandlers(append(
		WithLuaRuntime(app),
		WithYamlPolicies(app),
		WithDirectoryManager(app),
		WithHTTPService(app),
		WithTokenStoreManager(app),
		WithTerminalManager(app),
		WithProcessSupervisor(app),
		WithEphemeralHost(app),
		WithSQLManager(app),
		WithAWSConfigManager(app),
		WithS3Manager(app),
		WithProcessFunctionBridge(app),
		WithMemStore(app),
		WithNativeExecutor(app),
		WithJetTemplates(app),
	)...)
	// --------------------------------------------------

	// collect gc
	runtime.GC()

	// Serve profiler if enabled
	if *enableProfiling {
		app.StartProfiler()
	}

	// LaunchProcess application
	if err := app.Start(folderPath); err != nil {
		app.logger.Fatal("failed to start application", zap.Error(err))
	}

	app.logger.Named("wippy").Info("application started successfully")

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for first shutdown signal
	sig := <-sigChan
	app.logger.Info("received shutdown signal, starting graceful shutdown", zap.String("signal", sig.String()))

	// LaunchProcess goroutine to handle second signal
	go func() {
		sig := <-sigChan
		app.logger.Warn("received second shutdown signal, forcing immediate shutdown", zap.String("signal", sig.String()))
		close(app.forceShutdown)
	}()

	// Graceful shutdown
	if err := app.Stop(); err != nil {
		app.logger.Error("error during shutdown", zap.Error(err))
		os.Exit(1)
	}

	if app.shuttingDown {
		app.logger.Info("graceful shutdown completed")
	} else {
		app.logger.Info("force shutdown completed")
	}
}

func initLogger(verbose, veryVerbose bool, bus event.Bus) (*zap.Logger, logapi.Core) {
	config := zap.NewDevelopmentConfig()

	switch {
	case veryVerbose:
		config.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	case verbose:
		config.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		config.DisableStacktrace = true
	default:
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
		config.DisableStacktrace = true
	}

	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.DateTime)

	log, err := config.Build()
	if err != nil {
		fmt.Printf("Failed to build logger: %v\n", err)
		return nil, nil
	}

	core := logs.NewCore(log.Core(), bus)
	return zap.New(core), core
}

func loadApplicationState(
	folderPath string,
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

	entries, err := folderLoader.LoadFolder(folderPath, vars)
	if err != nil {
		return nil, fmt.Errorf("failed to load entries: %w", err)
	}

	boot, err := regtop.NewStateBuilder(mainLogger).BuildDelta(regapi.State{}, entries)
	if err != nil {
		return nil, fmt.Errorf("failed to build state delta: %w", err)
	}

	return boot, nil
}

// ---- Services ----
func WithTokenStoreManager(a *App) eventbus.EventHandler {
	// Create token store manager
	manager := tokenstore.NewManager(
		a.eventBus,
		a.dtt,
		a.resources,
		a.security,
		a.logger.Named("tstore"),
	)

	// Register manager for token store related entries
	return reghandler.NewRegistryHandler("security.token_store", manager)
}

func WithHTTPService(a *App) eventbus.EventHandler {
	// Create factories
	endpointFactory, err := http.NewEndpointFactory(a.funcs)
	if err != nil {
		panic(fmt.Errorf("failed to create endpoint factory: %w", err))
	}

	staticFactory, err := http.NewStaticFactory(a.fsRegistry)
	if err != nil {
		panic(fmt.Errorf("failed to create static factory: %w", err))
	}

	// Create websocket relay manager
	relayManager := websocket_relay.NewWebSocketRelay(a.ctx, a.logger.Named("ws"))

	// Create middleware factory with all standard middleware
	midFactory := http.NewDefaultMiddlewareFactory(
		http.WithLogger(a.logger.Named("http.md")),

		http.WithMiddlewareCreator(cors.MiddlewareName, cors.CreateCORSMiddleware),

		// Standard Chi middlewares
		http.WithMiddleware("recoverer", middleware.Recoverer),
		http.WithMiddleware("request_id", middleware.RequestID),
		http.WithMiddleware("real_ip", middleware.RealIP),

		// Timeout middleware with options
		http.WithMiddlewareCreator("timeout", func(options map[string]string) func(handler httpbase.Handler) httpbase.Handler {
			timeoutVal := options["timeout"]
			if timeoutVal == "" {
				timeoutVal = "60s"
			}
			duration, err := time.ParseDuration(timeoutVal)
			if err != nil {
				return nil
			}
			return middleware.Timeout(duration)
		}),

		// WebSocket relay middleware
		http.WithMiddleware("websocket_relay", relayManager.Middleware),
		http.WithMiddlewareCreator(tokenstore.MiddlewareName, tokenstore.CreateTokenAuthMiddleware),
		http.WithMiddlewareCreator(firewall.ResourceMiddlewareName, firewall.CreateResourceFirewallMiddleware),
		http.WithMiddlewareCreator(firewall.EndpointMiddlewareName, firewall.CreateEndpointFirewallMiddleware),
	)

	// Create manager with all required factories
	manager, err := http.NewManager(
		a.dtt,
		a.eventBus,
		http.NewServerFactory(midFactory),
		endpointFactory,
		staticFactory,
		a.logger.Named("http"),
	)
	if err != nil {
		panic(fmt.Errorf("failed to create http manager: %w", err))
	}

	return reghandler.NewRegistryHandler("http.*", manager)
}

func WithTerminalManager(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("terminal.host", terminal.NewTerminalManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("terminal"),
	))
}

func WithProcessSupervisor(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("process.service", service.NewSupervisorServiceManager(
		a.eventBus,
		a.processes,
		a.logger.Named("super"),
	))
}

func WithEphemeralHost(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("process.host", prochost.NewHostManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("hosts"),
	))
}

func WithNoopRuntime(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("(function|workflow|process|library).*", noop.NewNoopRuntime(
		a.eventBus,
		a.logger.Named("noop"),
	))
}

func WithDirectoryManager(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("fs.directory", fsdir.NewDirectoryManager(
		a.eventBus,
		a.dtt,
		nil,
		a.logger.Named("fs.dir"),
	))
}

func WithAWSConfigManager(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("config.aws", config.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("config.aws"),
	))
}

func WithS3Manager(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("cloudstorage.s3", s3.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("cloudstorage.s3"),
	))
}

func WithSQLManager(a *App) eventbus.EventHandler {
	// Create manager with required dependencies
	manager, err := sql.NewManager(
		a.dtt,
		a.eventBus,
		a.logger.Named("sql"),
	)
	if err != nil {
		panic(fmt.Errorf("failed to create sql manager: %w", err))
	}

	// Register handler for all SQL-related kinds
	return reghandler.NewRegistryHandler("db.sql.*", manager)
}

func WithYamlPolicies(a *App) eventbus.EventHandler {
	// Create manager with required dependencies
	manager := policy.NewManager(
		a.eventBus,
		policy.NewDefaultFactory(a.dtt),
		a.logger.Named("policy"),
	)

	// Register handler for all SQL-related kinds
	return reghandler.NewRegistryHandler("security.policy", manager)
}

func WithMemStore(a *App) eventbus.EventHandler {
	// Create manager with required dependencies
	manager := memstore.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("memory"),
	)

	return reghandler.NewRegistryHandler("store.memory", manager)
}

func WithNativeExecutor(a *App) eventbus.EventHandler {
	// Create manager with required dependencies
	manager := native.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("exec"),
	)

	return reghandler.NewRegistryHandler("exec.native", manager)
}

func WithProcessFunctionBridge(a *App) eventbus.EventHandler {
	return processfunc.WithProcessFunctionBridge(
		a.logger.Named("pfunc"),
		a.eventBus,
		a.processes,
	)
}

func WithJetTemplates(a *App) eventbus.EventHandler {
	// Create manager with required dependencies
	manager := template.NewManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("exec"),
	)

	return reghandler.NewRegistryHandler("template.(jet|set)", manager)
}

func WithLuaRuntime(a *App) []eventbus.EventHandler {
	codeManager, err := code.NewCodeManager(
		a.logger.Named("lua"),
		a.eventBus,
		code.Config{
			Modules: []luaapi.Module{
				env.NewEnvModule(),
				ostime.NewOSTimeModule(),
				channel.NewChannelModule(),
				timemod.NewTimeModule(),
				logger.NewLoggerModule(a.logger.Named("app")),
				base64.NewBase64Module(),
				jsonmod.NewJSONModule(),
				fsmod.NewFSModule(),
				uuid.NewUUIDModule(),
				upstream.NewUpstreamModule(),
				subscribe.NewSubscribeModule(),
				crypto.NewCryptoModule(),
				fncallmod.NewFunctionModule(),
				payloadmod.NewPayloadModule(),
				task.NewTaskModule(),
				hash.NewHashModule(),
				command.NewCommandModule(),
				yamlmod.NewYAMLModule(),
				registrymod.NewLoaderModule(a.logger.Named("loader")),
				events.NewEventsModule(a.logger.Named("events")),
				exec.NewExecModule(a.logger.Named("exec")),
				ctx.NewCtxModule(a.logger.Named("ctx")),
				store.NewStoreModule(a.logger.Named("store")),
				luatemplate.NewTemplateModule(a.logger.Named("template")),
				securitymod.NewSecurityModule(a.logger.Named("security")),
				registrymod.NewRegistryModule(a.logger.Named("registry")),
				processmod.NewProcessAPIModule(a.logger.Named("proc")),
				httpapimod.NewHTTPAPIModule(a.logger.Named("http")),
				processmodapi.NewProcessAPIModule(a.logger.Named("inbox")),
				funcmod.NewFunctionAPIModule(a.logger.Named("inbox")),
				httpclient.NewHTTPClientModule(a.logger.Named("http"), httpbase.DefaultClient),
				websocket.NewWebSocketModule(a.logger.Named("websocket")),
				treesitter.NewTreeSitterModule(a.logger.Named("tsitter")),
				btea.NewBteaModule(a.logger.Named("btea")),
				sqlmod.NewSQLModule(a.logger.Named("sql")),
				excel.NewModule(a.logger.Named("excel")),
				cloudstorage.NewModule(),
			},
			ProtoCacheSize: 600,
			MainCacheSize:  100,
		},
	)
	if err != nil {
		panic(err)
	}

	funcs := funclua.NewManager(a.logger.Named("lua.funcs"), codeManager, a.eventBus)
	libraries := library.NewManager(a.logger.Named("lua.libs"), codeManager)
	processes := proclua.NewProcessManager(a.logger.Named("lua.proc"), codeManager, a.eventBus)
	terminalApps := bteaapp.NewBteaManager(a.logger.Named("lua.bteaapp"), codeManager, a.eventBus)

	return []eventbus.EventHandler{
		reghandler.NewTransactionHandler(codeManager),
		component.NewHandler("function.lua", funcs),
		component.NewHandler("library.lua", libraries),
		component.NewHandler("process.lua", processes),
		component.NewHandler("btea.app.lua", terminalApps),
	}
}
