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
	//"github.com/ponyruntime/pony/system/process"
	"github.com/ponyruntime/pony/system/registry"
	"github.com/ponyruntime/pony/system/registry/history"
	"github.com/ponyruntime/pony/system/registry/loader"
	services "github.com/ponyruntime/pony/system/registry/router"
	"github.com/ponyruntime/pony/system/registry/runner"
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

// This needs Endure or some other way to untangle it.

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

	bus := eventbus.NewBus() // main configuration bus

	log, core := initLogger(*verbose, *veryVerbose, bus)
	if log == nil {
		fmt.Println("Failed to initialize logger")
		os.Exit(1)
	}
	defer func() {
		_ = log.Sync()
	}()

	appLogger := log.Named("main")

	dtt := transcoder.GlobalTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)

	// application service supervisor
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, contextapi.LoggerCtx, appLogger)
	ctx = context.WithValue(ctx, contextapi.TranscoderCtx, dtt)
	ctx = context.WithValue(ctx, contextapi.BusCtx, bus)
	defer cancel()

	// -- application state
	appState, err := loadApplicationState(args, dtt, appLogger)
	if err != nil {
		appLogger.Fatal("failed to load application state", zap.Error(err))
	}

	// -- observability application
	logSrv := logs.NewManager(bus, core, log.Named("logs"))
	if err := logSrv.Start(ctx); err != nil {
		appLogger.Fatal("failed to start logs service", zap.Error(err))
	}
	defer func() { _ = logSrv.Stop(context.Background()) }()

	// -- application core
	reg := registry.NewRegistry(
		history.NewMemory(),
		runner.NewBusRunner(bus, log.Named("runner")),
		registry.NewStateBuilder(appLogger),
		log.Named("state"),
	)

	app := supervisor.NewSupervisor(bus, log.Named("core"))
	// -- end of application core

	// -- additional services
	term := terminal.NewManager(bus, dtt, log.Named("term"))
	if err := term.Start(ctx); err != nil {
		appLogger.Fatal("failed to start executor", zap.Error(err))
	}
	defer func() { _ = term.Stop() }()
	// -- end of additional services

	// -- core function executor, this service listens and builds routes to call functions between runtimes
	execReg := function.NewExecutor(bus, log.Named("execReg"))
	if err := execReg.Start(ctx); err != nil {
		appLogger.Fatal("failed to start executor", zap.Error(err))
	}
	defer func() { _ = execReg.Stop() }()
	// -- end of core function executor

	// -- workflow registry
	//processReg := process.NewRegistry(bus, log.Named("workflow"))
	//if err := processReg.Start(ctx); err != nil {
	//	appLogger.Fatal("failed to start workflow registry", zap.Error(err))
	//}
	//defer func() { _ = processReg.Stop() }()

	// todo: should we just PUT everything into Wippy?
	ctx = context.WithValue(ctx, contextapi.FunctionsCtx, execReg)
	//ctx = context.WithValue(ctx, contextapi.ProcessCtx, processReg)

	// -- lua lang and modules
	luaRuntime := luaruntime.NewRuntimeManager(
		bus, dtt, log.Named("lua"),
		timelib.NewTimeModule(),
		logglib.NewLoggerModule(log.Named("app")),
		b64mlib.NewBase64Module(),
		jsonlib.NewJSONModule(),
		lfs.NewLFSModule(),
		uuid.NewUUIDModule(),
		env.NewEnvModule(log.Named("env")),
		httplib.NewHTTPModule(log.Named("http"), httpbase.DefaultClient),
		websocket.NewWebSocketModule(log.Named("websocket")),
		httpctx.NewHTTPContextModule(log.Named("http")),
		treesitter.NewTreeSitterModule(log.Named("treesitter")),
		btea.NewBteaModule(log.Named("btea")),
	)
	// -- end of lua lang and modules

	// -- end of temporal

	// -- env
	envCtx := contextapi.NewContexter[string]()
	for _, en := range os.Environ() {
		pair := strings.SplitN(en, "=", 2)
		if len(pair) == 2 {
			envCtx.WithValue(pair[0], pair[1])
		}
	}
	ctx = context.WithValue(ctx, contextapi.EnvCtx, envCtx)
	// -- end of env

	// -- configuration bus
	svc, err := services.NewRouter(ctx, bus,
		services.WithListener("http.*", http.NewHTTPManager(bus, dtt, execReg, log.Named("http"))),
		services.WithListener("(function|library|terminal|workflow).lua", luaRuntime),
		services.WithListener("terminal.*", term),
	)

	if err != nil {
		appLogger.Fatal("failed to create router", zap.Error(err))
	}
	defer func() { _ = svc.Stop() }()
	// -- end of configuration bus

	appLogger.Info("booting application")
	if err := app.Start(ctx); err != nil {
		appLogger.Fatal("failed to start supervisor", zap.Error(err))
	}
	appLogger.Info("application started, configuring state")

	// appState application stateBuilder
	bootCtx, cancelBoot := context.WithTimeout(ctx, 1*time.Second)
	defer cancelBoot()
	_, err = reg.Apply(bootCtx, appState)
	if err != nil {
		appLogger.Fatal("failed to apply state", zap.Error(err))
	}

	appLogger.Info("application state configured, running")

	// Handle graceful shutdown on Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// wait for either shutdown signal or context cancellation
	select {
	case <-ctx.Done():
		appLogger.Info("context canceled, shutting down...")
	case sig := <-sigChan:
		appLogger.Info("received signal, shutting down...", zap.String("signal", sig.String()))
	}

	go func() {
		// hard stop
		time.Sleep(15 * time.Second)
		cancel()
	}()

	if err := app.Stop(); err != nil {
		appLogger.Error("failed to stop supervisor gracefully", zap.Error(err))
	} else {
		appLogger.Info("supervisor stopped gracefully")
	}
}

func loadApplicationState(
	args []string,
	dtt *transcoder.Transcoder,
	mainLogger *zap.Logger,
) (regapi.ChangeSet, error) {
	folderPath := args[0]
	namespace := ""
	if len(args) > 1 {
		namespace = args[1]
	}

	folderLoader := loader.NewFolderLoader(dtt, mainLogger)
	vars := loader.Variables{}
	for _, en := range os.Environ() {
		pair := strings.SplitN(en, "=", 2)
		vars[pair[0]] = pair[1]
	}

	entries, err := folderLoader.Load(folderPath, namespace, vars) // Pass vars to Load
	if err != nil {
		mainLogger.Fatal("Failed to load entries", zap.Error(err))
	}

	// boot delta
	boot, err := registry.NewStateBuilder(mainLogger).BuildDelta(regapi.State{}, entries) // build delta
	if err != nil {
		mainLogger.Fatal("Failed to build state operation set", zap.Error(err))
	}

	return boot, err
}

func initLogger(verbose, veryVerbose bool, bus events.Bus) (*zap.Logger, logsapi.Core) {
	config := zap.NewDevelopmentConfig()

	// Set log level based on flags
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
