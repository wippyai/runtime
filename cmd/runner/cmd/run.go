package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	appbuild "github.com/ponyruntime/pony/cmd/runner/app"
	"github.com/ponyruntime/pony/deps"
	"github.com/ponyruntime/pony/internal/runtimeconfig"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the application",
	Long:  "Run the smart application runtime using paths from the lock file.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger, err := createLogger()
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		// Parse runtime configuration flags
		runtimeConfigFlags, _ := cmd.Flags().GetStringSlice("runtime-config")
		runtimeCfg := runtimeconfig.New()

		for _, configEntry := range runtimeConfigFlags {
			if err := runtimeCfg.SetFromString(configEntry); err != nil {
				logger.Error("failed to parse runtime configuration",
					zap.String("entry", configEntry),
					zap.Error(err))
				return fmt.Errorf("invalid runtime configuration '%s': %w", configEntry, err)
			}
		}

		if len(runtimeConfigFlags) > 0 {
			logger.Info("Loaded runtime configuration",
				zap.Int("entries", len(runtimeConfigFlags)),
				zap.Strings("namespaces", runtimeCfg.GetAllNamespaces()))
		}

		lockFile, _ := cmd.Flags().GetString("lock-file")
		folderPath := "."

		lockPath, err := deps.FindLockFile(folderPath, lockFile)
		if err != nil {
			logger.Error("lock file not found", zap.String("file", lockFile), zap.Error(err))
			os.Exit(1)
		}

		lockFileObj, err := deps.LoadLockFile(lockPath)
		if err != nil {
			logger.Error("failed to load lock file", zap.Error(err))
			os.Exit(1)
		}

		logger.Info("Starting application with lock file paths",
			zap.String("src_dir", lockFileObj.Directories.Src),
			zap.String("modules_dir", lockFileObj.Directories.Modules))

		fullLockPath := lockPath
		if !filepath.IsAbs(fullLockPath) {
			fullLockPath = filepath.Join(".", fullLockPath)
			fullLockPath, err = filepath.Abs(fullLockPath)
			if err != nil {
				logger.Error("failed to resolve absolute lock file path", zap.Error(err))
				os.Exit(1)
			}
		}
		lockDir := filepath.Dir(fullLockPath)

		appDir := filepath.Join(lockDir, lockFileObj.Directories.Src)
		// Use vendor path (modules + "/vendor")
		vendorPath := lockFileObj.GetModulesVendorPath()
		modulesDir := filepath.Join(lockDir, vendorPath)

		if !strings.HasSuffix(appDir, string(os.PathSeparator)) {
			appDir += string(os.PathSeparator)
		}
		if !strings.HasSuffix(modulesDir, string(os.PathSeparator)) {
			modulesDir += string(os.PathSeparator)
		}

		logger.Info("Resolved paths from lock file",
			zap.String("lock_file_dir", lockDir),
			zap.String("source_dir", appDir),
			zap.String("modules_dir", modulesDir))

		absModulesDir, err := filepath.Abs(modulesDir)
		if err != nil {
			logger.Error("failed to resolve absolute modules directory path", zap.Error(err))
			os.Exit(1)
		}

		if _, err := os.Stat(absModulesDir); os.IsNotExist(err) {
			if err := os.MkdirAll(absModulesDir, 0o755); err != nil {
				logger.Error("failed to create modules directory", zap.Error(err))
				os.Exit(1)
			}
			logger.Info("Created modules directory", zap.String("modules_dir", absModulesDir))
		} else if err != nil {
			logger.Error("failed to stat modules directory", zap.Error(err))
			os.Exit(1)
		}

		absLockDir, err := filepath.Abs(lockDir)
		if err != nil {
			logger.Error("failed to resolve absolute lock file directory path", zap.Error(err))
			os.Exit(1)
		}

		absLockPath, err := filepath.Abs(fullLockPath)
		if err != nil {
			logger.Error("failed to resolve absolute lock file path", zap.Error(err))
			os.Exit(1)
		}

		enableProfiling, _ := cmd.Flags().GetBool("profiling")
		useEmbed, _ := cmd.Flags().GetBool("use-embed")

		clusterEnabled, _ := cmd.Flags().GetBool("cluster")
		clusterName, _ := cmd.Flags().GetString("cluster-name")
		clusterBind, _ := cmd.Flags().GetString("cluster-bind")
		clusterPort, _ := cmd.Flags().GetInt("cluster-port")
		clusterJoin, _ := cmd.Flags().GetString("cluster-join")
		clusterSecret, _ := cmd.Flags().GetString("cluster-secret")
		clusterSecretFile, _ := cmd.Flags().GetString("cluster-secret-file")
		clusterAdvertise, _ := cmd.Flags().GetString("cluster-advertise")

		if clusterName == "" {
			if hostname, err := os.Hostname(); err == nil {
				clusterName = hostname
			} else {
				logger.Error("failed to get hostname and no cluster name provided", zap.Error(err))
				os.Exit(1)
			}
		}

		if clusterEnabled {
			if clusterSecret != "" && clusterSecretFile != "" {
				logger.Error("cannot specify both --cluster-secret and --cluster-secret-file")
				os.Exit(1)
			}
		}

		consoleLogging, eventStreaming := GetLoggingConfig()
		minLevel := GetVerboseLevel()

		app, err := appbuild.NewApp(
			logger,
			appbuild.WithPaths(appDir, absLockPath, absModulesDir, absLockDir, useEmbed),
			appbuild.WithLogging(consoleLogging, eventStreaming, minLevel),
			appbuild.WithProfiling(enableProfiling),
			appbuild.WithCluster(clusterEnabled, clusterName, clusterBind, clusterPort, clusterJoin, clusterSecret, clusterSecretFile, clusterAdvertise),
			appbuild.WithRuntimeConfig(runtimeCfg),
		)
		if err != nil {
			logger.Error("failed to create application", zap.Error(err))
			os.Exit(1)
		}

		if err := app.Initialize(); err != nil {
			logger.Error("failed to initialize application", zap.Error(err))
			os.Exit(1)
		}

		runtime.GC()

		if err := app.Start(); err != nil {
			logger.Error("failed to start application", zap.Error(err))
			os.Exit(1)
		}

		app.StartProfiler()

		logger.Info("Application started successfully")

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		sig := <-sigChan
		logger.Info("received shutdown signal, starting graceful shutdown", zap.String("signal", sig.String()))

		go func() {
			sig := <-sigChan
			logger.Warn("received second shutdown signal, forcing immediate shutdown", zap.String("signal", sig.String()))
			app.ForceShutdown()
		}()

		if err := app.Shutdown(); err != nil {
			logger.Error("error during shutdown", zap.Error(err))
			os.Exit(1)
		}

		logger.Info("shutdown completed")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().StringP("lock-file", "l", "wippy.lock", "path to lock file")
	runCmd.Flags().BoolP("profiling", "p", false, "enable performance profiling")
	runCmd.Flags().Bool("use-embed", false, "use embedded files")

	runCmd.Flags().StringSliceP("runtime-config", "r", []string{}, "runtime configuration in format namespace:entry:field=value (can be specified multiple times). Entry and field can contain dots.")

	runCmd.Flags().BoolP("cluster", "C", false, "enable cluster membership")
	runCmd.Flags().StringP("cluster-name", "n", "", "cluster node name (defaults to hostname)")
	runCmd.Flags().String("cluster-bind", "0.0.0.0", "cluster bind address")
	runCmd.Flags().Int("cluster-port", 7946, "cluster bind port")
	runCmd.Flags().StringP("cluster-join", "j", "", "comma-separated addresses to join")
	runCmd.Flags().String("cluster-secret", "", "cluster secret key (base64 encoded string)")
	runCmd.Flags().String("cluster-secret-file", "", "path to file containing cluster secret key")
	runCmd.Flags().String("cluster-advertise", "", "cluster advertise IP address")
}
