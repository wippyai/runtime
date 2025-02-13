package main

import (
	"context"
	"flag"
	"fmt"
	apiCtx "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	apiLog "github.com/ponyruntime/pony/api/logs"
	apiReg "github.com/ponyruntime/pony/api/registry"
	apiLua "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/manager/function"
	"github.com/ponyruntime/pony/runtime/lua/manager/library"
	"github.com/ponyruntime/pony/runtime/lua/manager/terminal"
	"github.com/ponyruntime/pony/runtime/lua/modules/base64"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea"
	"github.com/ponyruntime/pony/runtime/lua/modules/env"
	httpMod "github.com/ponyruntime/pony/runtime/lua/modules/http"
	httpClient "github.com/ponyruntime/pony/runtime/lua/modules/http_client"
	jsonMod "github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/ponyruntime/pony/runtime/lua/modules/lfs"
	"github.com/ponyruntime/pony/runtime/lua/modules/logger"
	timeMod "github.com/ponyruntime/pony/runtime/lua/modules/time"
	"github.com/ponyruntime/pony/runtime/lua/modules/treesitter"
	"github.com/ponyruntime/pony/runtime/lua/modules/uuid"
	"github.com/ponyruntime/pony/runtime/lua/modules/websocket"
	"github.com/ponyruntime/pony/runtime/noop"
	"github.com/ponyruntime/pony/service/http"
	"github.com/ponyruntime/pony/service/shell"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/ponyruntime/pony/system/functions"
	"github.com/ponyruntime/pony/system/logs"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/process"
	"github.com/ponyruntime/pony/system/registry"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
	"github.com/ponyruntime/pony/system/registry/history"
	"github.com/ponyruntime/pony/system/registry/loader"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	"github.com/ponyruntime/pony/system/registry/runner"
	"github.com/ponyruntime/pony/system/registry/topology"
	"github.com/ponyruntime/pony/system/supervisor"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	httpbase "net/http"
	"os"
	"os/signal"
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
	funcs       *functions.FunctionRegistry
	processes   *process.ProcessManager
	prototypes  *process.PrototypeRegistry
	hosts       *process.HostRegistry

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

	app := &App{
		ctx:           ctx,
		cancel:        cancel,
		logger:        appLogger,
		logCore:       core,
		logManager:    logManager,
		eventBus:      bus,
		services:      nil,
		dtt:           dtt,
		forceShutdown: make(chan struct{}),
	}

	return app, nil
}

func (a *App) Initialize() error {
	// Start log manager first for proper logging
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
	a.funcs = functions.NewExecutor(a.eventBus, a.logger.Named("funcs"))
	a.prototypes = process.NewPrototypeFactory(a.eventBus, a.logger.Named("prototypes"))
	a.hosts = process.NewHostRegistry(a.eventBus, a.logger.Named("hosts"))
	a.processes = process.NewProcessManager(a.hosts, a.prototypes, a.logger.Named("processes"))

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

	// Spawn environment context
	envCtx := apiCtx.NewContexter[string]()
	for _, en := range os.Environ() {
		pair := strings.SplitN(en, "=", 2)
		if len(pair) == 2 {
			envCtx.WithValue(pair[0], pair[1])
		}
	}
	ctx = context.WithValue(ctx, apiCtx.EnvCtx, envCtx)

	// Start core function registry
	if err := a.funcs.Start(ctx); err != nil {
		return fmt.Errorf("failed to start function executor: %w", err)
	}

	if err := a.prototypes.Start(ctx); err != nil {
		return fmt.Errorf("failed to start prototype registry: %w", err)
	}

	if err := a.hosts.Start(ctx); err != nil {
		return fmt.Errorf("failed to start host registry: %w", err)
	}

	// Start supervisor
	if err := a.supervisor.Start(ctx); err != nil {
		a.cancel()
		return fmt.Errorf("failed to start supervisor: %w", err)
	}

	// Start secondary services
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

	// launch

	return nil
}

func (a *App) Stop() error {
	a.shuttingDown = true

	// Spawn shutdown context with timeout
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()

	// Start a goroutine to handle force shutdown
	go func() {
		select {
		case <-a.forceShutdown:
			a.logger.Warn("force shutdown triggered, canceling context")
			cancel()
		case <-ctx.Done():
		}
	}()

	// Stop services in reverse order
	if err := a.eventRouter.Stop(); err != nil {
		a.logger.Error("failed to stop router", zap.Error(err))
	}

	// Stop supervisor
	if err := a.supervisor.Stop(); err != nil {
		a.logger.Error("failed to stop supervisor", zap.Error(err))
	}

	// Functions
	if err := a.funcs.Stop(); err != nil {
		a.logger.Error("failed to stop function executor", zap.Error(err))
	}

	// Stop log manager last
	if err := a.logManager.Stop(); err != nil {
		a.logger.Error("failed to stop log manager", zap.Error(err))
	}

	// Cancel main context and clean up
	a.cancel()
	_ = a.logger.Sync()

	return nil
}

func main() {
	// Parse command line flags
	verbose := flag.Bool("v", false, "enable verbose debug logging")
	veryVerbose := flag.Bool("vv", false, "enable very verbose debug logging with stack traces")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: go run main.go [-v] [-vv] <folder_path> [namespace]")
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
		WithShellManager(app),
	)...)
	// --------------------------------------------------

	// Start application
	if err := app.Start(folderPath); err != nil {
		app.logger.Fatal("Failed to start application", zap.Error(err))
	}

	app.logger.Info("application started successfully")

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for first shutdown signal
	sig := <-sigChan
	app.logger.Info("received shutdown signal, starting graceful shutdown", zap.String("signal", sig.String()))

	// Start goroutine to handle second signal
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

func WithShellManager(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("shell.host", shell.NewShellManager(
		a.eventBus,
		a.dtt,
		a.logger.Named("shell"),
	))
}

func WithNoopRuntime(a *App) eventbus.EventHandler {
	return reghandler.NewRegistryHandler("(function|process|library).*", noop.NewNoopRuntime(
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

	funcs := function.NewManager(a.logger.Named("lua.funcs"), codeManager, a.eventBus)
	libraries := library.NewManager(a.logger.Named("lua.libs"), codeManager)
	terminals := terminal.NewTerminalManager(a.logger.Named("lua.terminals"), codeManager, a.eventBus)

	return []eventbus.EventHandler{
		reghandler.NewTransactionHandler(codeManager),
		reghandler.NewRegistryHandler("function.lua", funcs),
		reghandler.NewRegistryHandler("library.lua", libraries),
		reghandler.NewRegistryHandler("terminal.lua", terminals),
	}
}
