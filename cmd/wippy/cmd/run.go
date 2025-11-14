package cmd

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	identityv1connect "github.com/wippyai/module-registry-proto-go/registry/identity/v1/identityv1connect"
	modulev1connect "github.com/wippyai/module-registry-proto-go/registry/module/v1/modulev1connect"
	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	bootpkg "github.com/wippyai/runtime/boot"
	cli "github.com/wippyai/runtime/boot/cli"
	"github.com/wippyai/runtime/boot/deps/client"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"go.uber.org/zap"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the runtime from lock file",
	Long: `Start the Wippy runtime environment from wippy.lock file

Loads entries from lock file, runs full pipeline (Override, Disable, Link),
and starts the runtime.

Examples:
  wippy run
  wippy run --override app:gateway:addr=:9090
  wippy run -o app:db:host=localhost -o app:db:port=5432`,
	RunE: runApp,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringSliceP("override", "o", nil, "Override entry values (format: namespace:entry:field=value)")
}

func runApp(cmd *cobra.Command, args []string) error {
	printBanner()

	logger, err := CreateLogger()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer logger.Sync()

	logger.Info("initializing runtime")

	cfg, err := loadBootConfig()
	if err != nil {
		logger.Error("failed to load config", zap.Error(err))
		return err
	}

	if cfg == nil {
		cfg = createDefaultConfig()
	}

	overrides, _ := cmd.Flags().GetStringSlice("override")
	if len(overrides) > 0 {
		cfg, err = applyOverrideFlags(cfg, overrides, logger)
		if err != nil {
			logger.Error("failed to apply override flags", zap.Error(err))
			return err
		}
	}

	ctx, err := bootpkg.NewInfrastructure(logger, cfg)
	if err != nil {
		logger.Error("failed to initialize infrastructure", zap.Error(err))
		return fmt.Errorf("initialize infrastructure: %w", err)
	}

	// Initialize registry client for module installation
	baseURL := os.Getenv("WIPPY_MODULES_URL")
	if baseURL == "" {
		baseURL = cli.DefaultRegistryURL
	}

	httpClient := &http.Client{}
	registryClient := client.NewRegistryClient(
		identityv1connect.NewOrganizationServiceClient(httpClient, baseURL),
		modulev1connect.NewModuleServiceClient(httpClient, baseURL),
		modulev1connect.NewLabelServiceClient(httpClient, baseURL),
		modulev1connect.NewDownloadServiceClient(httpClient, baseURL),
	)
	ctx = cli.WithRegistryClient(ctx, registryClient)

	logger = logapi.GetLogger(ctx).Named("run")
	logger.Info("infrastructure initialized")

	components := StandardComponents()
	logger.Info("registered components", zap.Int("count", len(components)))

	loader, err := bootpkg.NewLoader(components...)
	if err != nil {
		logger.Error("failed to create loader", zap.Error(err))
		return fmt.Errorf("failed to create loader: %w", err)
	}

	ctx, err = loader.Load(ctx)
	if err != nil {
		logger.Error("load failed", zap.Error(err))
		return fmt.Errorf("failed to load components: %w", err)
	}
	logger.Info("components loaded successfully")

	// Bridge file loader from AppContext to CLI context for entries.go compatibility
	fileLdr := boot.GetLoader(ctx)
	if fileLdr == nil {
		logger.Error("file loader not found in AppContext after component loading")
		return fmt.Errorf("file loader not found in AppContext")
	}
	ctx = cli.WithLoader(ctx, fileLdr)

	err = bootpkg.StartInfrastructure(ctx)
	if err != nil {
		logger.Error("failed to start infrastructure", zap.Error(err))
		return fmt.Errorf("start infrastructure: %w", err)
	}

	err = loader.Start(ctx)
	if err != nil {
		logger.Error("start failed", zap.Error(err))
		return fmt.Errorf("failed to start components: %w", err)
	}

	if err := loadEntriesFromLockFile(ctx, logger); err != nil {
		logger.Error("entry loading failed", zap.Error(err))
		return fmt.Errorf("failed to load entries: %w", err)
	}

	if !silentLogs {
		logger.Info("runtime ready")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	if !silentLogs {
		logger.Info("shutting down")
	}

	err = loader.Shutdown(ctx)
	if err != nil {
		logger.Error("shutdown error", zap.Error(err))
		return fmt.Errorf("shutdown failed: %w", err)
	}

	err = bootpkg.StopInfrastructure(ctx)
	if err != nil {
		logger.Error("failed to stop infrastructure", zap.Error(err))
		return fmt.Errorf("stop infrastructure: %w", err)
	}

	if !silentLogs {
		logger.Info("stopped")
	}
	return nil
}

func loadBootConfig() (boot.Config, error) {
	if configFile == "" {
		configFile = ".wippy.yaml"
	}

	cfg, err := bootconfig.Load(configFile)
	if err != nil {
		return nil, err
	}

	defaults := createDefaultConfig()
	if cfg == nil {
		return defaults, nil
	}

	return bootconfig.Merge(defaults, cfg), nil
}

func createDefaultConfig() boot.Config {
	opts := []boot.ConfigOption{}

	if verbose || veryVerbose || console {
		loggerCfg := map[string]interface{}{}

		if verbose || veryVerbose {
			loggerCfg["mode"] = "development"
			loggerCfg["level"] = "debug"
		}

		if console {
			loggerCfg["encoding"] = "console"
		}

		if len(loggerCfg) > 0 {
			opts = append(opts, boot.WithSection("logger", loggerCfg))
		}
	}

	if eventStreams {
		opts = append(opts, boot.WithSection("logmanager", map[string]interface{}{
			"stream_to_events": true,
		}))
	}

	return boot.NewConfig(opts...)
}

func applyOverrideFlags(cfg boot.Config, overrides []string, logger *zap.Logger) (boot.Config, error) {
	overrideMap := make(map[string]interface{})

	// Get existing overrides from config if any
	if cfg != nil {
		sub := cfg.Sub("override")
		if sub != nil {
			for _, key := range sub.Keys() {
				if val, ok := sub.Get(key); ok {
					overrideMap[key] = val
				}
			}
		}
	}

	// Parse and add CLI overrides
	for _, override := range overrides {
		namespace, entry, field, value, err := parseOverride(override)
		if err != nil {
			return nil, fmt.Errorf("invalid override '%s': %w", override, err)
		}

		// Format: namespace:entry:field
		key := fmt.Sprintf("%s:%s:%s", namespace, entry, field)
		overrideMap[key] = value

		if logger != nil {
			logger.Debug("applying override",
				zap.String("key", key),
				zap.String("value", value))
		}
	}

	// Create new config with merged overrides
	opts := []boot.ConfigOption{
		boot.WithSection("override", overrideMap),
	}

	if cfg != nil {
		return bootconfig.Merge(cfg, boot.NewConfig(opts...)), nil
	}

	return boot.NewConfig(opts...), nil
}

func parseOverride(input string) (namespace, entry, field, value string, err error) {
	// Find equals sign to split key=value
	eqIdx := strings.Index(input, "=")
	if eqIdx == -1 {
		return "", "", "", "", fmt.Errorf("missing '=' separator (expected namespace:entry:field=value)")
	}

	keyPart := input[:eqIdx]
	value = input[eqIdx+1:]

	// Find first colon to separate namespace
	firstColonIdx := strings.Index(keyPart, ":")
	if firstColonIdx == -1 {
		return "", "", "", "", fmt.Errorf("missing first ':' separator (expected namespace:entry:field=value)")
	}

	namespace = strings.TrimSpace(keyPart[:firstColonIdx])
	remainder := keyPart[firstColonIdx+1:]

	if namespace == "" {
		return "", "", "", "", fmt.Errorf("empty namespace")
	}

	// Find second colon to separate entry from field
	secondColonIdx := strings.Index(remainder, ":")
	if secondColonIdx == -1 {
		return "", "", "", "", fmt.Errorf("missing second ':' separator (expected namespace:entry:field=value)")
	}

	entry = strings.TrimSpace(remainder[:secondColonIdx])
	field = strings.TrimSpace(remainder[secondColonIdx+1:])

	if entry == "" {
		return "", "", "", "", fmt.Errorf("empty entry name")
	}

	if field == "" {
		return "", "", "", "", fmt.Errorf("empty field")
	}

	return namespace, entry, field, value, nil
}
