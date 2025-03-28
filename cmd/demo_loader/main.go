package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	"github.com/ponyruntime/pony/system/registry/topology"

	"github.com/ponyruntime/pony/system/payload/lua"
	"go.uber.org/zap/zapcore"

	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/payload"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/registry/loader"
)

func createTranscoder() payload.Transcoder {
	tr := transcoder.NewTranscoder()

	json.Register(tr)
	yaml.Register(tr)
	lua.Register(tr)

	return tr
}

func main() {
	// 1. Configure Logger and Transcoder:
	logger := initDevelopmentLogger()
	defer func() {
		_ = logger.Sync()
	}()

	dtt := createTranscoder()

	// 2. Get Folder Alias from Kind-Line Argument:
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <folder_path> [namespace]")
		return
	}
	folderPath := os.Args[1]

	// 3. Spawn Loader:
	folderLoader := loader.NewLoader(dtt, logger, interpolate.NewEntryInterpolator(dtt,
		interpolate.WithInterpolator(interpolate.LoadVars),
		interpolate.WithInterpolator(interpolate.LoadFile),
	))

	// --- Load Environment Variables into Variables map ---
	vars := interpolate.Variables{}
	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
		vars[pair[0]] = pair[1]
	}

	// 4. Load List:
	entries, err := folderLoader.LoadFS(folderPath, vars) // Pass vars to Load
	if err != nil {
		logger.Fatal("Failed to load entries", zap.Error(err))
	}

	entries = topology.SortEntriesByDependency(entries)

	// invert the list
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	// 5. Dump List to Console:
	fmt.Println("Loaded Registry List (YAML):")
	for _, entry := range entries {
		p, err := dtt.Transcode(entry.Data, payload.YAML)
		if err != nil {
			logger.Error("Failed to transcode entry to YAML", zap.String("id", entry.ID.String()), zap.Error(err))
			continue // Skip to the next entry if transcoding fails
		}

		// Print the entry:
		fmt.Println("---")
		fmt.Printf("Alias: \x1b[32m%s\x1b[0m\n", entry.ID)
		fmt.Printf("Kind: \x1b[35m%s\x1b[0m\n", entry.Kind)
		fmt.Println("Data:")
		fmt.Printf("\x1b[33m%s\x1b[0m", p.Data().(string))
	}
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
