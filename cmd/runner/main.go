package main

import (
	"context"
	"flag"
	"fmt"
	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/pkg/eventbus"
	transcoder "github.com/ponyruntime/pony/pkg/payload"
	"github.com/ponyruntime/pony/pkg/payload/json"
	"github.com/ponyruntime/pony/pkg/payload/lua"
	"github.com/ponyruntime/pony/pkg/payload/yaml"
	"github.com/ponyruntime/pony/pkg/registry"
	"github.com/ponyruntime/pony/pkg/registry/history"
	"github.com/ponyruntime/pony/pkg/registry/loader"
	services "github.com/ponyruntime/pony/pkg/registry/router"
	"github.com/ponyruntime/pony/pkg/registry/runner"
	"github.com/ponyruntime/pony/pkg/supervisor"
	"github.com/ponyruntime/pony/runtime"
	luaruntime "github.com/ponyruntime/pony/runtime/lua"
	base64M "github.com/ponyruntime/pony/runtime/lua/modules/base64"
	httpmodule "github.com/ponyruntime/pony/runtime/lua/modules/http"
	jsonmodule "github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/ponyruntime/pony/service/http"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"hash/fnv"
	httpbase "net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

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

	// application service supervisor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := initLogger(*verbose, *veryVerbose)
	if logger == nil {
		fmt.Println("Failed to initialize logger")
		os.Exit(1)
	}
	defer logger.Sync()

	mainLogger := logger.Named("main")

	dtt := transcoder.GlobalTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)

	// -- application state
	appState, err := loadApplicationState(args, dtt, mainLogger)

	// -- application core
	bus := eventbus.NewBus(logger.Named("events")) // main configuration bus
	reg := registry.NewRegistry(                   // application state controller, transactional
		history.NewMemory(),
		runner.NewBusRunner(bus, logger.Named("runner")),
		registry.NewStateBuilder(mainLogger),
		logger.Named("state"),
	)
	app := supervisor.NewSupervisor(bus, logger.Named("core"))
	// -- end of application core

	// -- core function executor, this service listens and builds routes to call functions between runtimes
	exec := runtime.NewExecutor(bus, logger.Named("exec"))
	if err := exec.Start(ctx); err != nil {
		mainLogger.Fatal("failed to start executor", zap.Error(err))
	}
	defer func() { _ = exec.Stop() }()
	// -- end of core function executor

	// -- lua lang and modules
	lua := luaruntime.NewRuntimeManager(
		bus, dtt, logger.Named("lua"),
		base64M.NewBase64Module(),
		jsonmodule.NewJsonModule(),
		httpmodule.NewHTTPModule(httpbase.DefaultClient, logger.Named("http")),
	)
	//nop := noop.NewNoopRuntime(bus, logger.Named("noop"))
	// -- end of lua lang and modules

	// -- configuration bus
	svc, err := services.NewRouter(ctx, bus,
		services.WithListener(
			"http.*",
			http.NewExecutingManager(bus, dtt, exec, logger.Named("http")),
		),
		services.WithListener("(function|library).lua", lua),
	)

	if err != nil {
		mainLogger.Fatal("failed to create router", zap.Error(err))
	}
	defer func() { _ = svc.Stop() }()
	// -- end of configuration bus

	mainLogger.Info("booting application")
	if err := app.Start(ctx); err != nil {
		mainLogger.Fatal("failed to start supervisor", zap.Error(err))
	}
	mainLogger.Info("application started, configuring state")

	// appState application stateBuilder
	bootCtx, cancelBoot := context.WithTimeout(ctx, 1*time.Second)
	defer cancelBoot()
	_, err = reg.Apply(bootCtx, appState)
	if err != nil {
		mainLogger.Fatal("failed to apply state", zap.Error(err))
	}

	mainLogger.Info("application state configured, running")

	// Handle graceful shutdown on Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for either shutdown signal or context cancellation
	select {
	case <-ctx.Done():
		mainLogger.Info("context cancelled, shutting down...")
	case sig := <-sigChan:
		mainLogger.Info("received signal, shutting down...", zap.String("signal", sig.String()))
	}

	if err := app.Stop(); err != nil {
		mainLogger.Error("failed to stop supervisor gracefully", zap.Error(err))
	} else {
		mainLogger.Info("supervisor stopped gracefully")
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
	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
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

func initLogger(verbose, veryVerbose bool) *zap.Logger {
	config := zap.NewDevelopmentConfig()

	// Set log level based on flags
	switch {
	case veryVerbose:
		config.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	case verbose:
		config.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		// Disable stack traces for -v
		config.DisableStacktrace = true
	default:
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
		// Disable stack traces by default
		config.DisableStacktrace = true
	}

	// Always use console encoding with colors
	config.Encoding = "console"
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	config.EncoderConfig.EncodeCaller = nil // Remove caller information
	config.EncoderConfig.TimeKey = "time"
	config.EncoderConfig.LevelKey = "level"
	config.EncoderConfig.NameKey = "logger"
	config.EncoderConfig.MessageKey = "msg"
	config.EncoderConfig.StacktraceKey = "stacktrace"

	config.EncoderConfig.EncodeName = func(loggerName string, enc zapcore.PrimitiveArrayEncoder) {
		// Simple hash function - sum ASCII values
		hash := 0
		for _, char := range loggerName {
			hash += int(char)
		}

		hash2 := hashString(loggerName)

		// Generate R, G, B values from the hash
		r := int(hash2 & 0xFF)         // Extract red component
		g := int((hash2 >> 8) & 0xFF)  // Extract green component
		b := int((hash2 >> 16) & 0xFF) // Extract blue component
		coloredName := fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, b, loggerName)

		// Wrap name in ANSI color codes
		//	coloredName := fmt.Sprintf("\x1b[%dm%s\x1b[0m", colorCode, loggerName)
		enc.AppendString(coloredName)
	}
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.DateTime)

	logger, err := config.Build()
	if err != nil {
		fmt.Printf("Failed to build logger: %v\n", err)
		return nil
	}

	return logger
}

func hashString(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}
