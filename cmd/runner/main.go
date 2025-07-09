package main

import (
	"flag"
	"fmt"
	iofs "io/fs"
	httpbase "net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/ponyruntime/pony/requirementresolver"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
	ctxapi "github.com/ponyruntime/pony/api/context"
	envapi "github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	fsapi "github.com/ponyruntime/pony/api/fs"
	funcapi "github.com/ponyruntime/pony/api/function"
	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	procapi "github.com/ponyruntime/pony/api/process"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	regapi "github.com/ponyruntime/pony/api/registry"
	resourceapi "github.com/ponyruntime/pony/api/resource"
	luaapi "github.com/ponyruntime/pony/api/runtime/lua"
	secapi "github.com/ponyruntime/pony/api/security"
	topapi "github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/embed"
	"github.com/ponyruntime/pony/moduleloader"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/command"
	"github.com/ponyruntime/pony/runtime/lua/component"
	bteaapp "github.com/ponyruntime/pony/runtime/lua/component/btea"
	funclua "github.com/ponyruntime/pony/runtime/lua/component/function"
	"github.com/ponyruntime/pony/runtime/lua/component/library"
	proclua "github.com/ponyruntime/pony/runtime/lua/component/process"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	"github.com/ponyruntime/pony/runtime/lua/engine/upstream"
	"github.com/ponyruntime/pony/runtime/lua/modules/base64"
	"github.com/ponyruntime/pony/runtime/lua/modules/btea"
	"github.com/ponyruntime/pony/runtime/lua/modules/cloudstorage"
	"github.com/ponyruntime/pony/runtime/lua/modules/crypto"
	"github.com/ponyruntime/pony/runtime/lua/modules/ctx"
	envlua "github.com/ponyruntime/pony/runtime/lua/modules/env"
	"github.com/ponyruntime/pony/runtime/lua/modules/events"
	"github.com/ponyruntime/pony/runtime/lua/modules/excel"
	"github.com/ponyruntime/pony/runtime/lua/modules/exec"
	fsmod "github.com/ponyruntime/pony/runtime/lua/modules/fs"
	"github.com/ponyruntime/pony/runtime/lua/modules/funcmod"
	fncallmod "github.com/ponyruntime/pony/runtime/lua/modules/funcs"
	"github.com/ponyruntime/pony/runtime/lua/modules/hash"
	httpapimod "github.com/ponyruntime/pony/runtime/lua/modules/http"
	"github.com/ponyruntime/pony/runtime/lua/modules/httpclient"
	jsonmod "github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/ponyruntime/pony/runtime/lua/modules/logger"
	"github.com/ponyruntime/pony/runtime/lua/modules/ostime"
	otelmod "github.com/ponyruntime/pony/runtime/lua/modules/otel"
	payloadmod "github.com/ponyruntime/pony/runtime/lua/modules/payload"
	processmod "github.com/ponyruntime/pony/runtime/lua/modules/process"
	processmodapi "github.com/ponyruntime/pony/runtime/lua/modules/processmod"
	registrymod "github.com/ponyruntime/pony/runtime/lua/modules/registry"
	securitymod "github.com/ponyruntime/pony/runtime/lua/modules/security"
	sqlmod "github.com/ponyruntime/pony/runtime/lua/modules/sql"
	"github.com/ponyruntime/pony/runtime/lua/modules/store"
	"github.com/ponyruntime/pony/runtime/lua/modules/system"
	luatemplate "github.com/ponyruntime/pony/runtime/lua/modules/template"
	"github.com/ponyruntime/pony/runtime/lua/modules/text"
	timemod "github.com/ponyruntime/pony/runtime/lua/modules/time"
	"github.com/ponyruntime/pony/runtime/lua/modules/treesitter"
	"github.com/ponyruntime/pony/runtime/lua/modules/uuid"
	"github.com/ponyruntime/pony/runtime/lua/modules/websocket"
	yamlmod "github.com/ponyruntime/pony/runtime/lua/modules/yaml"
	"github.com/ponyruntime/pony/runtime/lua/task"
	"github.com/ponyruntime/pony/runtime/noop"
	"github.com/ponyruntime/pony/service/aws/config"
	"github.com/ponyruntime/pony/service/aws/s3"
	fsdir "github.com/ponyruntime/pony/service/directory"
	envservice "github.com/ponyruntime/pony/service/env"
	native "github.com/ponyruntime/pony/service/exec"
	prochost "github.com/ponyruntime/pony/service/host"
	"github.com/ponyruntime/pony/service/http"
	"github.com/ponyruntime/pony/service/http/cors"
	"github.com/ponyruntime/pony/service/http/firewall"
	"github.com/ponyruntime/pony/service/http/websocketrelay"
	"github.com/ponyruntime/pony/service/memstore"
	"github.com/ponyruntime/pony/service/policy"
	"github.com/ponyruntime/pony/service/processfunc"
	"github.com/ponyruntime/pony/service/sql"
	"github.com/ponyruntime/pony/service/sqlstore"
	service "github.com/ponyruntime/pony/service/supervisor"
	"github.com/ponyruntime/pony/service/template"
	"github.com/ponyruntime/pony/service/terminal"
	"github.com/ponyruntime/pony/service/tokenstore"
	"github.com/ponyruntime/pony/system/env"
	"github.com/ponyruntime/pony/system/eventbus"
	"github.com/ponyruntime/pony/system/fs"
	"github.com/ponyruntime/pony/system/function"
	"github.com/ponyruntime/pony/system/interceptor"
	"github.com/ponyruntime/pony/system/logs"
	transcoder "github.com/ponyruntime/pony/system/payload"
	"github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	"github.com/ponyruntime/pony/system/process"
	"github.com/ponyruntime/pony/system/pubsub"
	"github.com/ponyruntime/pony/system/registry"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
	"github.com/ponyruntime/pony/system/registry/history"
	"github.com/ponyruntime/pony/system/registry/loader"
	"github.com/ponyruntime/pony/system/registry/loader/interpolate"
	"github.com/ponyruntime/pony/system/registry/runner"
	regtop "github.com/ponyruntime/pony/system/registry/topology"
	"github.com/ponyruntime/pony/system/resource"
	"github.com/ponyruntime/pony/system/security"
	"github.com/ponyruntime/pony/system/supervisor"
	"github.com/ponyruntime/pony/system/topology"
	"github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	"github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otelresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	// supported dbs
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
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

// parseJoinAddrs parses comma-separated join addresses
func parseJoinAddrs(joinStr string) []string {
	if joinStr == "" {
		return nil
	}

	addrs := strings.Split(joinStr, ",")
	for i, addr := range addrs {
		addrs[i] = strings.TrimSpace(addr)
	}
	return addrs
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
