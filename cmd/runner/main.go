package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/fatih/color"
	"github.com/pkg/errors"
	"github.com/ponyruntime/pony/moduleloader"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	// supported dbs
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// Default cluster port
const (
	DefaultClusterPort = 7946
)

var (
	appStartTime = time.Now()

	levelColors = map[zapcore.Level]*color.Color{
		zapcore.DebugLevel: color.New(color.FgCyan),
		zapcore.InfoLevel:  color.New(color.FgGreen),
		zapcore.WarnLevel:  color.New(color.FgYellow),
		zapcore.ErrorLevel: color.New(color.FgRed),
		zapcore.PanicLevel: color.New(color.FgMagenta),
		zapcore.FatalLevel: color.New(color.FgMagenta, color.Bold),
	}

	componentColors = []*color.Color{
		color.New(color.FgBlue),
		color.New(color.FgMagenta),
		color.New(color.FgCyan),
		color.New(color.FgYellow),
		color.New(color.FgGreen),
		color.New(color.FgRed),
	}
)

// Config holds all application configuration
type Config struct {
	// Core application
	FolderPath string
	UseEmbed   bool

	// Logging
	Verbose        bool
	VeryVerbose    bool
	ConsoleLogging bool
	LogEvents      bool

	// Performance
	EnableProfiling bool

	// Cluster membership
	ClusterEnabled    bool
	ClusterName       string
	ClusterBind       string
	ClusterPort       int
	ClusterJoin       string
	ClusterSecret     string // Secret as string
	ClusterSecretFile string // Secret from file
	ClusterAdvertise  string // Advertise IP

	// Lock file path
	LockFile string
}

func consoleTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	elapsed := t.Sub(appStartTime)
	enc.AppendString(fmt.Sprintf("%6.2fs", elapsed.Seconds()))
}

func consoleLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	if levelColor, ok := levelColors[level]; ok {
		enc.AppendString(levelColor.Sprintf("%-5s", level.CapitalString()))
	} else {
		enc.AppendString(fmt.Sprintf("%-5s", level.CapitalString()))
	}
}

func consoleNameEncoder(loggerName string, enc zapcore.PrimitiveArrayEncoder) {
	if loggerName == "" {
		enc.AppendString("")
		return
	}

	hash := 0
	for _, r := range loggerName {
		hash = hash*31 + int(r)
	}
	colorIndex := (hash % len(componentColors))
	if colorIndex < 0 {
		colorIndex = -colorIndex
	}

	componentColor := componentColors[colorIndex]
	enc.AppendString(componentColor.Sprintf("%-12s", loggerName))
}

func initMainLogger(verbose, veryVerbose, console bool) (*zap.Logger, error) {
	var cfg zap.Config

	if console {
		cfg = zap.Config{
			Level:       zap.NewAtomicLevelAt(zapcore.InfoLevel),
			Development: true,
			Encoding:    "console",
			EncoderConfig: zapcore.EncoderConfig{
				TimeKey:       "time",
				LevelKey:      "level",
				NameKey:       "name",
				CallerKey:     "",
				MessageKey:    "msg",
				StacktraceKey: "",
				LineEnding:    zapcore.DefaultLineEnding,
				EncodeTime:    consoleTimeEncoder,
				EncodeLevel:   consoleLevelEncoder,
				EncodeName:    consoleNameEncoder,
				EncodeCaller:  nil,
			},
			OutputPaths:      []string{"stdout"},
			ErrorOutputPaths: []string{"stderr"},
		}
	} else {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.DateTime)
		cfg.DisableCaller = true
	}

	switch {
	case veryVerbose:
		cfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		if !console {
			cfg.DisableStacktrace = false
		}
	case verbose:
		cfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		cfg.DisableStacktrace = true
	default:
		cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
		cfg.DisableStacktrace = true
	}

	log, err := cfg.Build()
	if err != nil {
		return nil, errors.Wrap(err, "failed to build logger")
	}

	return log, nil
}

// parseFlags parses command line flags into Config
func parseFlags() *Config {
	config := &Config{}

	// Core flags (existing)
	flag.BoolVar(&config.Verbose, "v", false, "enable verbose debug logging")
	flag.BoolVar(&config.VeryVerbose, "vv", false, "enable very verbose debug logging with stack traces")
	flag.BoolVar(&config.EnableProfiling, "p", false, "enable performance profiling")
	flag.BoolVar(&config.UseEmbed, "use-embed", false, "use embedded files")

	// Console logging flags
	flag.BoolVar(&config.ConsoleLogging, "c", false, "enable colorful humanized console logging")
	flag.BoolVar(&config.ConsoleLogging, "console", false, "enable colorful humanized console logging")

	// Event forwarding flag
	flag.BoolVar(&config.LogEvents, "log-events", false, "enable forwarding logs to event bus")

	// Cluster flags (new)
	flag.BoolVar(&config.ClusterEnabled, "cluster", false, "enable cluster membership")
	flag.StringVar(&config.ClusterName, "cluster-name", "", "cluster node name (defaults to hostname)")
	flag.StringVar(&config.ClusterBind, "cluster-bind", "0.0.0.0", "cluster bind address")
	flag.IntVar(&config.ClusterPort, "cluster-port", DefaultClusterPort, "cluster bind port")
	flag.StringVar(&config.ClusterJoin, "cluster-join", "", "comma-separated addresses to join")
	flag.StringVar(&config.ClusterSecret, "cluster-secret", "", "cluster secret key (base64 encoded string)")
	flag.StringVar(&config.ClusterSecretFile, "cluster-secret-file", "", "path to file containing cluster secret key")
	flag.StringVar(&config.ClusterAdvertise, "cluster-advertise", "", "cluster advertise IP address")

	flag.StringVar(&config.LockFile, "lock-file", moduleloader.DefaultLockFile, "path to lock file")

	flag.Parse()

	// Get folder path from args
	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: go run main.go [flags] <folder_path>")
		flag.PrintDefaults()
		os.Exit(1)
	}
	config.FolderPath = args[0]

	// Set default cluster name to hostname if not provided
	if config.ClusterName == "" {
		if hostname, err := os.Hostname(); err == nil {
			config.ClusterName = hostname
		} else {
			fmt.Printf("Failed to get hostname and no cluster name provided: %v\n", err)
			os.Exit(1)
		}
	}

	// Validate cluster secret configuration
	if config.ClusterEnabled {
		if config.ClusterSecret != "" && config.ClusterSecretFile != "" {
			fmt.Printf("Error: Cannot specify both -cluster-secret and -cluster-secret-file\n")
			os.Exit(1)
		}
	}

	return config
}

func main() {
	// Initialize sqlite-vec extension
	sqlite_vec.Auto()

	config := parseFlags()

	// Initialize logger at the top level
	logger, err := initMainLogger(config.Verbose, config.VeryVerbose, config.ConsoleLogging)
	if err != nil {
		fmt.Printf("Failed to create application: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		// CLI format
		cliRunner := NewCLIRunner(config, logger)
		if err := cliRunner.Run(context.Background()); err != nil {
			logger.Fatal("failed to run CLI command", zap.Error(err))
		}
		return
	}

	// All dependency management must use the new command format:
	//   wippy install --lock-file=<path> --
	//   wippy update --lock-file=<path> --
	//   wippy init --lock-file=<path> --src-dir=<path> --modules-dir=<path> --
	//   wippy run --lock-file=<path> --
	//   wippy replace --lock-file=<path> -- <command> [args]

	fmt.Println("Error: Legacy flag-based dependency management is no longer supported.")
	fmt.Println("Please use the new command format:")
	fmt.Println("  wippy install --lock-file=<path> --")
	fmt.Println("  wippy update --lock-file=<path> --")
	fmt.Println("  wippy init --lock-file=<path> --src-dir=<path> --modules-dir=<path> --")
	fmt.Println("  wippy run --lock-file=<path> --")
	fmt.Println("  wippy replace --lock-file=<path> -- <command> [args]")
	fmt.Println("")
	fmt.Println("Use 'wippy help' for more information.")
	os.Exit(1)
}
