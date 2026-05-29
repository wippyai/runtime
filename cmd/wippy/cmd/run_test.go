// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	"go.uber.org/zap"
)

func TestLoadBootConfigSetsConfigDir(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "wippy.yaml")
	cfgBody := []byte("version: \"1.0\"\nlua:\n  proto_cache_size: 1\n")
	require.NoError(t, os.WriteFile(cfgPath, cfgBody, 0o644))

	prevConfigFile := configFile
	prevProfiler := profiler
	configFile = cfgPath
	profiler = false
	t.Cleanup(func() {
		configFile = prevConfigFile
		profiler = prevProfiler
	})

	cfg, err := loadBootConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	expectedPath, err := filepath.Abs(cfgPath)
	require.NoError(t, err)
	expectedDir := filepath.Dir(expectedPath)

	require.Equal(t, expectedPath, cfg.GetString("boot.config_path", ""))
	require.Equal(t, expectedDir, cfg.GetString("boot.config_dir", ""))
}

func TestLoadRuntimeConfigAppliesOverridesAndCLISettings(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "wippy.yaml")
	cfgBody := []byte("version: \"1.0\"\n")
	require.NoError(t, os.WriteFile(cfgPath, cfgBody, 0o644))

	prevConfigFile := configFile
	prevProfiler := profiler
	prevVerbose := verbose
	prevVeryVerbose := veryVerbose
	prevConsole := console
	prevEventStreams := eventStreams

	configFile = cfgPath
	profiler = false
	verbose = true
	veryVerbose = false
	console = false
	eventStreams = true
	t.Cleanup(func() {
		configFile = prevConfigFile
		profiler = prevProfiler
		verbose = prevVerbose
		veryVerbose = prevVeryVerbose
		console = prevConsole
		eventStreams = prevEventStreams
	})

	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("override", nil, "")
	require.NoError(t, cmd.Flags().Set("override", "app:test:enabled=true"))
	require.NoError(t, cmd.Flags().Set("override", "app:db:port=5432"))
	require.NoError(t, cmd.Flags().Set("override", "app:gateway:addr=:9090"))

	cfg, err := loadRuntimeConfig(cmd, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, cfg)

	require.Equal(t, "development", cfg.GetString("logger.mode", ""))
	require.Equal(t, "debug", cfg.GetString("logger.level", ""))
	require.True(t, cfg.GetBool("logmanager.stream_to_events", false))

	overrideCfg := cfg.Sub("override")
	require.NotNil(t, overrideCfg)
	enabled, ok := overrideCfg.Get("app:test:enabled")
	require.True(t, ok)
	require.Equal(t, true, enabled)

	port, ok := overrideCfg.Get("app:db:port")
	require.True(t, ok)
	require.Equal(t, 5432, port)

	require.Equal(t, ":9090", overrideCfg.GetString("app:gateway:addr", ""))
}

func TestLoadRuntimeConfigWithDefaultsAppliesPackDefaultsWhenFileMissingKey(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "wippy.yaml")
	cfgBody := []byte("version: \"1.0\"\n")
	require.NoError(t, os.WriteFile(cfgPath, cfgBody, 0o644))

	prevConfigFile := configFile
	prevProfiler := profiler
	configFile = cfgPath
	profiler = false
	t.Cleanup(func() {
		configFile = prevConfigFile
		profiler = prevProfiler
	})

	runtimeDefaults := boot.NewConfig(boot.WithSection("lsp", map[string]any{
		"enabled": true,
	}))

	cfg, err := loadRuntimeConfigWithDefaults(nil, zap.NewNop(), runtimeDefaults)
	require.NoError(t, err)
	require.True(t, cfg.GetBool("lsp.enabled", false))
}

func TestLoadRuntimeConfigWithDefaultsFileOverridesPackDefaults(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "wippy.yaml")
	cfgBody := []byte("version: \"1.0\"\nlsp:\n  enabled: false\n")
	require.NoError(t, os.WriteFile(cfgPath, cfgBody, 0o644))

	prevConfigFile := configFile
	prevProfiler := profiler
	configFile = cfgPath
	profiler = false
	t.Cleanup(func() {
		configFile = prevConfigFile
		profiler = prevProfiler
	})

	runtimeDefaults := boot.NewConfig(boot.WithSection("lsp", map[string]any{
		"enabled": true,
	}))

	cfg, err := loadRuntimeConfigWithDefaults(nil, zap.NewNop(), runtimeDefaults)
	require.NoError(t, err)
	require.False(t, cfg.GetBool("lsp.enabled", true))
}
