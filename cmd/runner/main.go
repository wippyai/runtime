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
	"github.com/ponyruntime/pony/pkg/registry/runner"
	"github.com/ponyruntime/pony/pkg/supervisor"
	http "github.com/ponyruntime/pony/service/http"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"strings"
	"time"
)

func main() {
	// Parse command line flags
	verbose := flag.Bool("v", false, "enable verbose debug logging")
	flag.Parse()
	args := flag.Args()

	if len(args) < 1 {
		fmt.Println("Usage: go run main.go [-v] <folder_path> [namespace]")
		os.Exit(1)
	}

	// application service supervisor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := initLogger(*verbose)
	if logger == nil {
		fmt.Println("Failed to initialize logger")
		os.Exit(1)
	}
	defer logger.Sync()

	dtt := transcoder.NewTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)

	folderPath := args[0]
	namespace := ""
	if len(args) > 1 {
		namespace = args[1]
	}

	folderLoader := loader.NewFolderLoader(dtt, logger.Named("loader"))
	vars := loader.Variables{}
	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
		vars[pair[0]] = pair[1]
	}

	state := registry.NewStateBuilder(logger.Named("builder"))     // state builder
	entries, err := folderLoader.Load(folderPath, namespace, vars) // Pass vars to Load
	if err != nil {
		logger.Named("app").Fatal("Failed to load entries", zap.Error(err))
	}

	// boot delta
	boot, err := state.BuildDelta(regapi.State{}, entries) // build delta
	if err != nil {
		logger.Named("app").Fatal("Failed to build state operation set", zap.Error(err))
	}

	// server
	bus := eventbus.NewBus(logger.Named("events"))                   // main configuration bus
	sup := supervisor.NewSupervisor(bus, logger.Named("supervisor")) // service supervisor
	reg := registry.NewRegistry(                                     // application state controller, transactional
		history.NewMemory(),
		runner.NewBusRunner(bus, logger.Named("runner")),
		state,
		logger.Named("registry"),
	)

	// services, modules, runtimes
	err = http.Init(bus, dtt, logger.Named("http")).Start(ctx)
	if err != nil {
		logger.Named("app").Fatal("failed to start http service", zap.Error(err))
	}

	// end server configuration
	if err := sup.Start(ctx); err != nil {
		logger.Named("app").Fatal("failed to start supervisor", zap.Error(err))
	}

	// boot application state
	bootCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	_, err = reg.Apply(bootCtx, boot)
	if err != nil {
		logger.Named("app").Fatal("failed to apply boot state", zap.Error(err))
	}

	// wait for shutdown
	<-ctx.Done()
}

func initLogger(verbose bool) *zap.Logger {
	config := zap.NewDevelopmentConfig()

	// Set log level based on verbose flag
	if verbose {
		config.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	} else {
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}

	// Always use console encoding with colors
	config.Encoding = "console"
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	config.EncoderConfig.EncodeCaller = nil
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
		// map hash to one of 6 colors (31-36: red, green, yellow, blue, magenta, cyan)
		colorCode := 31 + (hash % 6)
		// Wrap name in ANSI color codes
		coloredName := fmt.Sprintf("\x1b[%dm%s\x1b[0m", colorCode, loggerName)
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
