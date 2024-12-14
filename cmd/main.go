package main

import (
	"fmt"
	"go.uber.org/zap/zapcore"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ponyruntime/pony/api/payload"
	transcoder "github.com/ponyruntime/pony/core/payload"
	"github.com/ponyruntime/pony/core/payload/json"
	"github.com/ponyruntime/pony/core/payload/yaml"
	"github.com/ponyruntime/pony/core/registry/loader"
)

func createTestTranscoder() payload.Transcoder {
	tr := transcoder.NewTranscoder()

	// Register JSON
	tr.RegisterTranscoder(payload.Json, payload.Golang, 1, &json.ToGolang{})
	tr.RegisterTranscoder(payload.Golang, payload.Json, 1, &json.FromGolang{})
	tr.RegisterUnmarshaler(payload.Json, &json.ToGolang{})

	// Register YAML
	tr.RegisterTranscoder(payload.Yaml, payload.Golang, 1, &yaml.ToGolang{})
	tr.RegisterTranscoder(payload.Golang, payload.Yaml, 1, &yaml.FromGolang{})
	tr.RegisterUnmarshaler(payload.Yaml, &yaml.ToGolang{})

	return tr
}

func main() {
	// 1. Configure Logger and Transcoder:
	logger := initDevelopmentLogger()
	defer logger.Sync()

	dtt := createTestTranscoder()

	// 2. Get Folder Path from Command-Line Argument:
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <folder_path> [namespace]")
		os.Exit(1)
	}
	folderPath := os.Args[1]

	namespace := ""
	if len(os.Args) > 2 {
		namespace = os.Args[2]
	}

	// 3. Create FolderLoader:
	folderLoader := loader.NewFolderLoader(dtt, logger)

	// --- Load Environment Variables into Variables map ---
	vars := loader.Variables{}
	for _, env := range os.Environ() {
		pair := strings.SplitN(env, "=", 2)
		vars[pair[0]] = pair[1]
	}

	// 4. Load Entries:
	entries, err := folderLoader.Load(folderPath, namespace, vars) // Pass vars to Load
	if err != nil {
		logger.Fatal("Failed to load entries", zap.Error(err))
	}

	// 5. Dump Entries to Console:
	fmt.Println("Loaded Registry Entries (YAML):")
	for _, entry := range entries {
		p, err := dtt.Transcode(entry.Data, payload.Yaml)
		if err != nil {
			logger.Error("Failed to transcode entry to YAML", zap.String("path", string(entry.Path)), zap.Error(err))
			continue // Skip to the next entry if transcoding fails
		}

		// Print the entry:
		fmt.Println("---")
		fmt.Printf("Path: %s\n", entry.Path)
		fmt.Printf("Kind: %s\n", entry.Kind)
		fmt.Println("Data:")
		fmt.Println(string(p.Data().(string)))
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
