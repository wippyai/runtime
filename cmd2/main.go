package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ponyruntime/pony/api/boot"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/boot/core"
	"github.com/ponyruntime/pony/boot/service"
	"github.com/ponyruntime/pony/boot/system"
	"github.com/spf13/cobra"
)

var (
	verbose      bool
	veryVerbose  bool
	console      bool
	silent       bool
	eventStreams bool

	lockFile      string
	profiling     bool
	useEmbed      bool
	runtimeConfig []string

	cluster           bool
	clusterName       string
	clusterBind       string
	clusterPort       int
	clusterJoin       []string
	clusterSecret     string
	clusterSecretFile string
	clusterAdvertise  string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "wippy",
		Short: "Wippy runtime",
		Long:  `Wippy - modular application runtime with plugin architecture`,
	}

	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&veryVerbose, "very-verbose", false, "Enable debug logging with stack traces")
	rootCmd.PersistentFlags().BoolVarP(&console, "console", "c", false, "Colorful console logging")
	rootCmd.PersistentFlags().BoolVarP(&silent, "silent", "s", false, "Disable console logging")
	rootCmd.PersistentFlags().BoolVarP(&eventStreams, "event-streams", "e", false, "Enable event streaming")

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the application",
		RunE:  runApp,
	}

	runCmd.Flags().StringVarP(&lockFile, "lock-file", "l", "wippy.lock", "Path to lock file")
	runCmd.Flags().BoolVarP(&profiling, "profiling", "p", false, "Enable pprof profiling")
	runCmd.Flags().BoolVar(&useEmbed, "use-embed", false, "Use embedded files")
	runCmd.Flags().StringArrayVarP(&runtimeConfig, "runtime-config", "r", []string{}, "Runtime config overrides")

	runCmd.Flags().BoolVarP(&cluster, "cluster", "C", false, "Enable clustering")
	runCmd.Flags().StringVarP(&clusterName, "cluster-name", "n", "", "Node name")
	runCmd.Flags().StringVar(&clusterBind, "cluster-bind", "0.0.0.0", "Bind address")
	runCmd.Flags().IntVar(&clusterPort, "cluster-port", 7946, "Bind port")
	runCmd.Flags().StringArrayVarP(&clusterJoin, "cluster-join", "j", []string{}, "Join addresses")
	runCmd.Flags().StringVar(&clusterSecret, "cluster-secret", "", "Encryption key")
	runCmd.Flags().StringVar(&clusterSecretFile, "cluster-secret-file", "", "Secret file path")
	runCmd.Flags().StringVar(&clusterAdvertise, "cluster-advertise", "", "Advertise IP")

	rootCmd.AddCommand(runCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runApp(cmd *cobra.Command, args []string) error {
	fmt.Println("Wippy CMD2 - Modular Plugin Architecture")
	fmt.Println("=========================================")

	// Combine all plugins
	plugins := []boot.Plugin{}
	plugins = append(plugins, core.All()...)
	plugins = append(plugins, system.All()...)
	plugins = append(plugins, service.All()...)

	fmt.Printf("Loading %d plugins...\n", len(plugins))

	loader, err := bootpkg.NewLoader(plugins...)
	if err != nil {
		return fmt.Errorf("failed to create loader: %w", err)
	}

	ctx := context.Background()
	cfg := createBootConfig()
	ctx = boot.WithConfig(ctx, cfg)

	ctx, err = loader.Load(ctx)
	if err != nil {
		return fmt.Errorf("failed to load plugins: %w", err)
	}

	fmt.Println("Starting plugins...")
	err = loader.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start plugins: %w", err)
	}

	fmt.Println("Application started successfully!")
	fmt.Println("Press Ctrl+C to stop...")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")
	err = loader.Shutdown(ctx)
	if err != nil {
		return fmt.Errorf("failed to shutdown: %w", err)
	}

	fmt.Println("Shutdown complete")
	return nil
}

func createBootConfig() boot.Config {
	cfg := make(map[string]interface{})

	if verbose || veryVerbose {
		cfg["logger"] = map[string]interface{}{
			"mode":     "development",
			"level":    "debug",
			"encoding": "console",
		}
	}

	if profiling {
		cfg["profiler"] = map[string]interface{}{
			"enabled": true,
			"address": "localhost:6060",
		}
	}

	if eventStreams {
		cfg["logmanager"] = map[string]interface{}{
			"stream_to_events": true,
		}
	}

	// Flatten nested maps for boot.Config
	flatCfg := make(map[string]interface{})
	for key, val := range cfg {
		if m, ok := val.(map[string]interface{}); ok {
			for subKey, subVal := range m {
				flatCfg[key+"."+subKey] = subVal
			}
		} else {
			flatCfg[key] = val
		}
	}
	return boot.NewConfig(flatCfg)
}
