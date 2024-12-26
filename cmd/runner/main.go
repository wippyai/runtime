package main

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"github.com/ponyruntime/pony/pkg/payload/lua"
	"github.com/ponyruntime/pony/pkg/registry"
	"github.com/ponyruntime/pony/pkg/registry/history"
	"github.com/ponyruntime/pony/pkg/registry/runner"
	"github.com/ponyruntime/pony/pkg/supervisor"
	"go.uber.org/zap/zapcore"
	"os"
	"strings"
	"time"

	rapi "github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"

	transcoder "github.com/ponyruntime/pony/pkg/payload"
	"github.com/ponyruntime/pony/pkg/payload/json"
	"github.com/ponyruntime/pony/pkg/payload/yaml"
	"github.com/ponyruntime/pony/pkg/registry/loader"
)

func main() {
	// application service supervisor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := initDevelopmentLogger()
	defer logger.Sync()

	dtt := transcoder.NewTranscoder()
	json.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)

	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <folder_path> [namespace]")
		os.Exit(1)
	}
	folderPath := os.Args[1]

	namespace := ""
	if len(os.Args) > 2 {
		namespace = os.Args[2]
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
		logger.Fatal("Failed to load entries", zap.Error(err))
	}

	// boot delta
	boot, err := state.BuildDelta(rapi.State{}, entries) // build delta
	if err != nil {
		logger.Fatal("Failed to build state operation set", zap.Error(err))
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

	// end server configuration

	if err := sup.Start(ctx); err != nil {
		logger.Fatal("failed to start supervisor", zap.Error(err))
	}

	// boot application state
	bootCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err = reg.Apply(bootCtx, boot)
	if err != nil {
		logger.Fatal("failed to apply boot state", zap.Error(err))
	}

	// wait for shutdown
	<-ctx.Done()
}

func initDevelopmentLogger() *zap.Logger {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	config.EncoderConfig.EncodeCaller = nil
	config.EncoderConfig.EncodeName = func(loggerName string, enc zapcore.PrimitiveArrayEncoder) {
		// Simple hash function - sum ASCII values
		hash := 0
		for _, char := range loggerName {
			hash += int(char)
		}

		// cmap hash to one of 6 colors (31-36: red, green, yellow, blue, magenta, cyan)
		colorCode := 31 + (hash % 6)

		// Wrap name in ANSI color codes
		coloredName := fmt.Sprintf("\x1b[%dm%s\x1b[0m", colorCode, loggerName)
		enc.AppendString(coloredName)
	}

	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.DateTime)
	zlog, _ := config.Build()
	return zlog
}
