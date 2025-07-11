package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/joho/godotenv"
	"go.uber.org/zap"

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
}

// loadDotEnv loads environment variables from .env files
func loadDotEnv(logger *zap.Logger, paths ...string) {
	// First try explicitly provided paths
	for _, path := range paths {
		envPath := filepath.Join(path, ".env")
		err := godotenv.Load(envPath)
		if err == nil {
			if logger != nil {
				logger.Info(".env file loaded successfully", zap.String("path", envPath))
			} else {
				fmt.Printf(".env file loaded successfully from: %s\n", envPath)
			}
			return
		}
	}

	// Try default location
	err := godotenv.Load()
	if err != nil {
		if logger != nil {
			logger.Debug("Could not load .env file from default location", zap.Error(err))
		}
	} else if logger != nil {
		logger.Info(".env file loaded successfully from default location")
	}
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

	// Create and initialize application
	app, err := NewApp(config)
	if err != nil {
		fmt.Printf("Failed to create application: %v\n", err)
		os.Exit(1)
	}

	// Load environment variables
	loadDotEnv(app.logger)

	if err := app.Initialize(); err != nil {
		fmt.Printf("Failed to initialize application: %v\n", err)
		os.Exit(1)
	}

	// Configure services
	app.services = createServiceHandlers(app)
	runtime.GC()

	// Start profiler if enabled
	if config.EnableProfiling {
		app.StartProfiler()
	}

	// Start application
	if err := app.Start(config.FolderPath, config.UseEmbed); err != nil {
		app.logger.Fatal("failed to start application", zap.Error(err))
	}

	app.logger.Named("wippy").Info("application started successfully")

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for first shutdown signal
	sig := <-sigChan
	app.logger.Info("received shutdown signal, starting graceful shutdown", zap.String("signal", sig.String()))

	// Handle second signal for force shutdown
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
