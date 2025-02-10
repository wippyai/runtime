package main

import (
	"context"
	"flag"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	logsapi "github.com/ponyruntime/pony/api/logs"
	regapi "github.com/ponyruntime/pony/api/registry"
	luaruntime "github.com/ponyruntime/pony/runtime/lua"
	b64mlib "github.com/ponyruntime/pony/runtime/lua/modules/base64"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea"
	"github.com/ponyruntime/pony/runtime/lua/modules/env"
	httplib "github.com/ponyruntime/pony/runtime/lua/modules/http"
	"github.com/ponyruntime/pony/runtime/lua/modules/httpctx"
	jsonlib "github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/ponyruntime/pony/runtime/lua/modules/lfs"
	logglib "github.com/ponyruntime/pony/runtime/lua/modules/logger"
	timelib "github.com/ponyruntime/pony/runtime/lua/modules/time"
	"github.com/ponyruntime/pony/runtime/lua/modules/treesitter"
	"github.com/ponyruntime/pony/runtime/lua/modules/uuid"
	"github.com/ponyruntime/pony/runtime/lua/modules/websocket"
	"github.com/ponyruntime/pony/service/http"
	"github.com/ponyruntime/pony/service/terminal"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/ponyruntime/pony/system/functions"
	"github.com/ponyruntime/pony/system/logs"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/registry"
	"github.com/ponyruntime/pony/system/registry/history"
	"github.com/ponyruntime/pony/system/registry/loader"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	services "github.com/ponyruntime/pony/system/registry/router"
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
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *zap.Logger
	logCore    logsapi.Core
	logManager *logs.Manager
	eventBus   events.Bus
	dtt        *transcoder.Transcoder
	reg        regapi.Registry
	supervisor *supervisor.Supervisor
	funcs      *functions.FunctionRegistry
	services   *services.Router

	shuttingDown  bool
	forceShutdown chan struct{}
}

func NewApp(verbose, veryVerbose bool) (*App, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Initialize event bus
	bus := eventbus.NewBus()

	// Initialize logger
	logger, core := initLogger(verbose, veryVerbose, bus)
	if logger == nil {
		cancel()
		return nil, fmt.Errorf("failed to initialize logger")
	}
	appLogger := logger.Named("main")

	level := zapcore.InfoLevel
	if verbose || veryVerbose {
		level = zapcore.DebugLevel
	}

	// Initialize log manager
	logManager := logs.NewManager(bus, core, logger.Named("logs"), level)

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

	return nil
}

func (a *App) Start(folderPath string) error {
	// Create context with values
	ctx := a.ctx
	ctx = context.WithValue(ctx, contextapi.LoggerCtx, a.logger)
	ctx = context.WithValue(ctx, contextapi.TranscoderCtx, a.dtt)
	ctx = context.WithValue(ctx, contextapi.BusCtx, a.eventBus)
	ctx = context.WithValue(ctx, contextapi.FunctionsCtx, a.funcs)

	// Create environment context
	envCtx := contextapi.NewContexter[string]()
	for _, en := range os.Environ() {
		pair := strings.SplitN(en, "=", 2)
		if len(pair) == 2 {
			envCtx.WithValue(pair[0], pair[1])
		}
	}
	ctx = context.WithValue(ctx, contextapi.EnvCtx, envCtx)

	// Start core function registry
	if err := a.funcs.Start(ctx); err != nil {
		return fmt.Errorf("failed to start function executor: %w", err)
	}

	// Initialize router with services
	var err error
	a.services, err = services.NewRouter(ctx, a.eventBus,
		withHTTPService(a),
		withLuaService(a),
		withTerminalService(a),
	)
	if err != nil {
		return fmt.Errorf("failed to create router: %w", err)
	}

	// Start supervisor
	if err := a.supervisor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start supervisor: %w", err)
	}

	// Load and apply initial state
	appState, err := loadApplicationState(folderPath, a.dtt, a.logger)
	if err != nil {
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

	// Create shutdown context with timeout
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
	if err := a.services.Stop(); err != nil {
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

	// Create and initialize application
	app, err := NewApp(*verbose, *veryVerbose)
	if err != nil {
		fmt.Printf("Failed to create application: %v\n", err)
		os.Exit(1)
	}

	if err := app.Initialize(); err != nil {
		fmt.Printf("Failed to initialize application: %v\n", err)
		os.Exit(1)
	}

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

func initLogger(verbose, veryVerbose bool, bus events.Bus) (*zap.Logger, logsapi.Core) {
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

	boot, err := topology.NewStateBuilder(mainLogger).BuildDelta(regapi.State{}, entries)
	if err != nil {
		return nil, fmt.Errorf("failed to build state delta: %w", err)
	}

	return boot, nil
}

// ---- Services ----

func withHTTPService(a *App) services.Option {
	return services.WithListener("http.*", http.NewHTTPManager(
		a.eventBus,
		a.dtt,
		a.funcs,
		a.logger.Named("http"),
	))
}

func withLuaService(a *App) services.Option {
	return services.WithListener("(function|library|process).lua",
		luaruntime.NewRuntimeManager(
			a.eventBus,
			a.dtt,
			a.logger.Named("lua"),
			timelib.NewTimeModule(),
			logglib.NewLoggerModule(a.logger.Named("app")),
			b64mlib.NewBase64Module(),
			jsonlib.NewJSONModule(),
			lfs.NewLFSModule(),
			uuid.NewUUIDModule(),
			env.NewEnvModule(a.logger.Named("env")),
			httplib.NewHTTPModule(a.logger.Named("http"), httpbase.DefaultClient),
			websocket.NewWebSocketModule(a.logger.Named("websocket")),
			httpctx.NewHTTPContextModule(a.logger.Named("http")),
			treesitter.NewTreeSitterModule(a.logger.Named("treesitter")),
			btea.NewBteaModule(a.logger.Named("btea")),
		))
}

func withTerminalService(a *App) services.Option {
	return services.WithListener("terminal.*",
		terminal.NewManager(a.eventBus, a.dtt, a.logger.Named("term")),
	)
}
