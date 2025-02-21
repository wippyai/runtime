package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	apiCtx "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	apiLog "github.com/ponyruntime/pony/api/logs"
	apiReg "github.com/ponyruntime/pony/api/registry"
	apiLua "github.com/ponyruntime/pony/api/runtime/lua"
	topologyApi "github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/runtime/lua/code"
	bteaApps "github.com/ponyruntime/pony/runtime/lua/component/btea"
	luaFunc "github.com/ponyruntime/pony/runtime/lua/component/function"
	"github.com/ponyruntime/pony/runtime/lua/component/library"
	luaProcess "github.com/ponyruntime/pony/runtime/lua/component/process"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	"github.com/ponyruntime/pony/runtime/lua/modules/base64"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea"
	"github.com/ponyruntime/pony/runtime/lua/modules/env"
	httpMod "github.com/ponyruntime/pony/runtime/lua/modules/http"
	httpClient "github.com/ponyruntime/pony/runtime/lua/modules/httpclient"
	jsonMod "github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/ponyruntime/pony/runtime/lua/modules/lfs"
	"github.com/ponyruntime/pony/runtime/lua/modules/logger"
	processmod "github.com/ponyruntime/pony/runtime/lua/modules/processapi"
	"github.com/ponyruntime/pony/runtime/lua/modules/tasks"
	timeMod "github.com/ponyruntime/pony/runtime/lua/modules/time"
	"github.com/ponyruntime/pony/runtime/lua/modules/treesitter"
	"github.com/ponyruntime/pony/runtime/lua/modules/upstream"
	"github.com/ponyruntime/pony/runtime/lua/modules/uuid"
	"github.com/ponyruntime/pony/runtime/lua/modules/websocket"
	"github.com/ponyruntime/pony/runtime/noop"
	processHosts "github.com/ponyruntime/pony/service/host"
	"github.com/ponyruntime/pony/service/http"
	service "github.com/ponyruntime/pony/service/supervisor"
	"github.com/ponyruntime/pony/service/terminal"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/ponyruntime/pony/system/function"
	"github.com/ponyruntime/pony/system/logs"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/process"
	pubsub "github.com/ponyruntime/pony/system/pubsub"
	"github.com/ponyruntime/pony/system/registry"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
	"github.com/ponyruntime/pony/system/registry/history"
	"github.com/ponyruntime/pony/system/registry/loader"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	"github.com/ponyruntime/pony/system/registry/runner"
	"github.com/ponyruntime/pony/system/registry/topology"
	"github.com/ponyruntime/pony/system/resource"
	"github.com/ponyruntime/pony/system/supervisor"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	httpbase "net/http"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"
)

type App struct {
	ctx         context.Context
	cancel      context.CancelFunc
	logger      *zap.Logger
	logCore     apiLog.Core
	logManager  *logs.Manager
	eventBus    events.Bus
	eventRouter *eventbus.EventRouter
	services    eventbus.RouterOption
	dtt         *transcoder.Transcoder
	reg         apiReg.Registry
	supervisor  *supervisor.Supervisor
	funcs       *function.Registry
	processes   *process.Manager
	prototypes  *process.PrototypeRegistry
	hosts       *process.HostRegistry

	resources *resource.Registry

	// mesh
	node *pubsub.NodeManager

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
		forceShutdown: make(chan struct{}),
	}

	return app, nil
}

func (a *App) Initialize() error {
	// LaunchProcess log manager first for proper logging
	if err := a.logManager.Start(a.ctx); err != nil {
		return fmt.Errorf("failed to start log manager: %w", err)
	}

	// Initialize core components
	a.reg = registry.NewRegistry(
		history.NewMemory(),
		runner.NewBusRunner(a.eventBus, a.logger.Named("runner")),
		topology.NewStateBuilder(a.logger),
		a.logger.Named("registry"),
	)

	a.supervisor = supervisor.NewSupervisor(a.eventBus, a.logger.Named("core"))

	// Initialize core function registry
	a.funcs = function.NewExecutor(a.eventBus, a.logger.Named("funcs"))
	a.prototypes = process.NewPrototypeFactory(a.eventBus, a.logger.Named("prototypes"))
	a.hosts = process.NewHostRegistry(a.eventBus, a.logger.Named("hosts"))

	a.resources = resource.NewResourceRegistry(a.eventBus, a.logger.Named("resources"))

	// groups, links, monitor and other topology level stuff
	control := process.NewTopology(a.ctx, a.logger.Named("control"), a.node)

	// this is host dedicated to internal control messages
	err := a.node.Node().RegisterHost(topologyApi.ControlHost, pubsub.NewHost(a.ctx, pubsub.HostConfig{
		BufferSize:      1024,
		WorkerCount:     16,
		Logger:          a.logger.Named("control"),
		RetryTimeout:    500 * time.Millisecond,
		DeliveryTimeout: 500 * time.Millisecond,
	}))

	if err != nil {
		return fmt.Errorf("failed to register control host: %w", err)
	}

	a.processes = process.NewProcessManager(
		a.hosts,
		a.prototypes,
		control,
		a.node.Node().ID(), // for pid generation of managed processes
		a.logger.Named("processes"),
	)

	return nil
}

func (a *App) Start(folderPath string) error {
	// Spawn context with values
	ctx := a.ctx
	ctx = context.WithValue(ctx, apiCtx.RegistryCtx, a.reg)
	ctx = context.WithValue(ctx, apiCtx.LoggerCtx, a.logger)
	ctx = context.WithValue(ctx, apiCtx.TranscoderCtx, a.dtt)
	ctx = context.WithValue(ctx, apiCtx.BusCtx, a.eventBus)
	ctx = context.WithValue(ctx, apiCtx.FunctionsCtx, a.funcs)
	ctx = context.WithValue(ctx, apiCtx.ProcessesCtx, a.processes)
	ctx = context.WithValue(ctx, apiCtx.ResourcesCtx, a.resources)
	ctx = context.WithValue(ctx, apiCtx.NodeCtx, a.node.Node())

	// Spawn environment context
	envCtx := apiCtx.NewContexter[string]()
	for _, en := range os.Environ() {
		pair := strings.SplitN(en, "=", 2)
		if len(pair) == 2 {
			envCtx.WithValue(pair[0], pair[1])
		}
	}
	ctx = context.WithValue(ctx, apiCtx.EnvCtx, envCtx)

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

	bootCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
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

	// Close services in reverse order
	if err := a.eventRouter.Stop(); err != nil {
		a.logger.Error("failed to stop router", zap.Error(err))
	}

	// Close supervisor
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

	// Close log manager last
	if err := a.logManager.Stop(); err != nil {
		a.logger.Error("failed to stop log manager", zap.Error(err))
	}

	// Cancel main context and clean up
	a.cancel()
	_ = a.logger.Sync()

	return nil
}

// Add this method to your App struct
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

func main() {
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

	if err := app.Initialize(); err != nil {
		fmt.Printf("Failed to initialize application: %v\n", err)
		os.Exit(1)
	}

	// ------ This is main service initiation point ------
	app.services = eventbus.WithHandlers(append(
		WithLuaRuntime(app),
		WithHTTPService(app),
		WithTerminalManager(app),
		WithProcessSupervisor(app),
		WithEphemeralHost(app),
	)...)
	// --------------------------------------------------

	// collect gc
	runtime.GC()

	// Start profiler if enabled
	if *enableProfiling {
		app.StartProfiler()
	}

	// LaunchProcess application
	if err := app.Start(folderPath); err != nil {
		app.logger.Fatal("failed to start application", zap.Error(err))
	}

	app.logger.Info("application started successfully")

	//todo: see os.OpenRoot()

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

func initLogger(verbose, veryVerbose bool, bus events.Bus) (*zap.Logger, apiLog.Core) {
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
) (apiReg.ChangeSet, error) {
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

	boot, err := topology.NewStateBuilder(mainLogger).BuildDelta(apiReg.State{}, entries)
	if err != nil {
		return nil, fmt.Errorf("failed to build state delta: %w", err)
	}

	return boot, nil
}

// ---- Services ----

func WithHTTPService(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("http.*", http.NewHTTPManager(
		a.eventBus,
		a.dtt,
		a.funcs,
		a.logger.Named("http"),
	))
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
		a.logger.Named("supervisor"),
	))
}

func WithEphemeralHost(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("process.host", processHosts.NewHostManager(
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

func WithLuaRuntime(a *App) []eventbus.EventHandler {
	codeManager, err := code.NewCodeManager(
		a.logger.Named("lua"),
		a.eventBus,
		code.Config{
			Modules: []apiLua.Module{
				channel.NewChannelModule(),
				timeMod.NewTimeModule(),
				logger.NewLoggerModule(a.logger.Named("app")),
				base64.NewBase64Module(),
				jsonMod.NewJSONModule(),
				lfs.NewLFSModule(),
				uuid.NewUUIDModule(),
				upstream.NewUpstreamModule(),
				processmod.NewProcessControlModule(a.logger.Named("process")),
				tasks.NewTaskModule(),
				subscribe.NewSubscribeModule(),
				env.NewEnvModule(a.logger.Named("env")),
				httpClient.NewHTTPClientModule(a.logger.Named("http"), httpbase.DefaultClient),
				websocket.NewWebSocketModule(a.logger.Named("websocket")),
				httpMod.NewHTTPContextModule(a.logger.Named("http")),
				treesitter.NewTreeSitterModule(a.logger.Named("treesitter")),
				btea.NewBteaModule(a.logger.Named("btea")),
			},
			ProtoCacheSize: 600,
			MainCacheSize:  100,
		},
	)
	if err != nil {
		panic(err)
	}

	funcs := luaFunc.NewManager(a.logger.Named("lua.funcs"), codeManager, a.eventBus)
	libraries := library.NewManager(a.logger.Named("lua.libs"), codeManager)
	processes := luaProcess.NewProcessManager(a.logger.Named("lua.proc"), codeManager, a.eventBus)
	terminalApps := bteaApps.NewBteaManager(a.logger.Named("lua.bteaApps"), codeManager, a.eventBus)

	return []eventbus.EventHandler{
		reghandler.NewTransactionHandler(codeManager),
		reghandler.NewRegistryHandler("function.lua", funcs),
		reghandler.NewRegistryHandler("library.lua", libraries),
		reghandler.NewRegistryHandler("process.lua", processes),
		reghandler.NewRegistryHandler("btea.app.lua", terminalApps),
	}
}
