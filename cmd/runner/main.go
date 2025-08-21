package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
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

// Config holds all application configuration
type Config struct {
	// Core application
	FolderPath string
	UseEmbed   bool

	// Logging
	Verbose     bool
	VeryVerbose bool

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

func initMainLogger(verbose, veryVerbose bool) (*zap.Logger, error) {
	cfg := zap.NewDevelopmentConfig()

	switch {
	case veryVerbose:
		cfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	case verbose:
		cfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
		cfg.DisableStacktrace = true
	default:
		cfg.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
		cfg.DisableStacktrace = true
	}
	// Remove file and line number from logs
	cfg.DisableCaller = true

	cfg.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout(time.DateTime)
	// Remove file and line number from logs
	cfg.DisableCaller = true

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
	logger, err := initMainLogger(config.Verbose, config.VeryVerbose)
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
