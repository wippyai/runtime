package cmd

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	appbuild "github.com/ponyruntime/pony/cmd/runner/app"
	requirementresolver2 "github.com/ponyruntime/pony/deps/requirementresolver"
	"github.com/ponyruntime/pony/internal/runtimeconfig"
	transcoder "github.com/ponyruntime/pony/system/payload"
	json2 "github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
	regtop "github.com/ponyruntime/pony/system/registry/topology"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// SHARED ENCRYPTION KEY - MUST MATCH CONFIG SERVER!
const remoteEncryptionKey = "change-this-32byte-secret-key!!!"

type RemoteToken struct {
	ConfigURL string `json:"config_url"`
	StateURL  string `json:"state_url"`
}

type RemoteConfig struct {
	Name          string            `json:"name"`
	StateURL      string            `json:"state_url"`
	EnvVariables  map[string]string `json:"env_variables"`
	RuntimeConfig []string          `json:"runtime_config"`
}

func decryptToken(encrypted string) (*RemoteToken, error) {
	key := []byte(remoteEncryptionKey)
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
	}

	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	var token RemoteToken
	if err := json.Unmarshal(plaintext, &token); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}

	return &token, nil
}

func fetchRemoteConfig(url string) (*RemoteConfig, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var config RemoteConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	return &config, nil
}

func fetchRemoteState(url string) ([]registry.Entry, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var serializable SerializableState
	if err := json.NewDecoder(resp.Body).Decode(&serializable); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}

	dtt := transcoder.GlobalTranscoder()
	json2.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)

	entries := make([]registry.Entry, len(serializable.Entries))
	for i, se := range serializable.Entries {
		entry, err := convertFromSerializableEntry(se, dtt, nil)
		if err != nil {
			return nil, fmt.Errorf("convert entry %s: %w", se.ID, err)
		}
		entries[i] = entry
	}

	return entries, nil
}

var remoteRunCmd = &cobra.Command{
	Use:   "remote-run",
	Short: "Run application from remote config server",
	Long:  "Decrypt token, fetch config and state from remote server, and run application",
	RunE: func(cmd *cobra.Command, _ []string) error {
		logger, err := createLogger()
		if err != nil {
			return fmt.Errorf("failed to create logger: %w", err)
		}

		encryptedToken, _ := cmd.Flags().GetString("token")
		if encryptedToken == "" {
			return fmt.Errorf("token is required")
		}

		logger.Info("Decrypting deployment token")
		token, err := decryptToken(encryptedToken)
		if err != nil {
			return fmt.Errorf("failed to decrypt token: %w", err)
		}

		logger.Info("Token decrypted successfully",
			zap.String("config_url", token.ConfigURL),
			zap.String("state_url", token.StateURL))

		logger.Info("Fetching remote configuration")
		config, err := fetchRemoteConfig(token.ConfigURL)
		if err != nil {
			return fmt.Errorf("failed to fetch config: %w", err)
		}

		logger.Info("Configuration fetched",
			zap.String("deployment", config.Name),
			zap.Int("env_vars", len(config.EnvVariables)),
			zap.Int("runtime_config", len(config.RuntimeConfig)))

		logger.Info("Fetching remote state")
		entries, err := fetchRemoteState(config.StateURL)
		if err != nil {
			return fmt.Errorf("failed to fetch state: %w", err)
		}

		logger.Info("State fetched", zap.Int("entries", len(entries)))

		logger.Info("=== TODO: INJECT ENVIRONMENT VARIABLES HERE ===")
		logger.Info("Environment variables from config:", zap.Int("count", len(config.EnvVariables)))
		// Environment variables will be injected via CreateServiceHandlersWithStaticEnv below

		// Parse runtime config from remote config
		runtimeCfg := runtimeconfig.New()
		for _, configEntry := range config.RuntimeConfig {
			if err := runtimeCfg.SetFromString(configEntry); err != nil {
				logger.Warn("failed to parse runtime config entry",
					zap.String("entry", configEntry),
					zap.Error(err))
			}
		}

		logger.Info("Resolving module definitions and dependencies")
		resolver := requirementresolver2.NewResolver(logger.Named("requirement-resolver"))
		entries, err = resolver.ResolveModuleDefinitions(entries)
		if err != nil {
			return fmt.Errorf("failed to resolve module definitions: %w", err)
		}

		if runtimeCfg != nil && len(runtimeCfg.GetAllNamespaces()) > 0 {
			entries, err = applyRuntimeConfigOverrides(entries, runtimeCfg, logger)
			if err != nil {
				return fmt.Errorf("failed to apply runtime config: %w", err)
			}
		}

		boot, err := regtop.NewStateBuilder(logger).BuildDelta(registry.State{}, entries)
		if err != nil {
			return fmt.Errorf("failed to build state delta: %w", err)
		}

		// Get flags for app configuration
		enableProfiling, _ := cmd.Flags().GetBool("profiling")
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
			appbuild.WithPaths(".", "", "", ".", false),
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

		// CRITICAL: Use CreateServiceHandlersWithStaticEnv to inject environment variables
		app.SetServices(appbuild.CreateServiceHandlersWithStaticEnv(app, config.EnvVariables))
		runtime.GC()

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()

		if err := startAppWithState(ctx, app, boot, logger); err != nil {
			logger.Error("failed to start application", zap.Error(err))
			os.Exit(1)
		}

		app.StartProfiler()

		logger.Info("Application started successfully from remote configuration",
			zap.String("deployment", config.Name))

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
	rootCmd.AddCommand(remoteRunCmd)

	remoteRunCmd.Flags().StringP("token", "t", "", "encrypted deployment token")
	remoteRunCmd.Flags().BoolP("profiling", "p", false, "enable performance profiling")

	remoteRunCmd.Flags().BoolP("cluster", "C", false, "enable cluster membership")
	remoteRunCmd.Flags().StringP("cluster-name", "n", "", "cluster node name (defaults to hostname)")
	remoteRunCmd.Flags().String("cluster-bind", "0.0.0.0", "cluster bind address")
	remoteRunCmd.Flags().Int("cluster-port", 7946, "cluster bind port")
	remoteRunCmd.Flags().StringP("cluster-join", "j", "", "comma-separated addresses to join")
	remoteRunCmd.Flags().String("cluster-secret", "", "cluster secret key")
	remoteRunCmd.Flags().String("cluster-secret-file", "", "path to file containing cluster secret key")
	remoteRunCmd.Flags().String("cluster-advertise", "", "cluster advertise IP address")

	_ = remoteRunCmd.MarkFlagRequired("token")
}
