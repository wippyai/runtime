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

func TestLoadRuntimeConfig_ProfileAndSetPrecedence(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "wippy.yaml")
	cfgBody := []byte(`version: "1.0"
vars:
  port: 8085
override:
  "app:gateway:addr": ":${port}"
  "app:db:kind": db.sql.sqlite
disable:
  namespaces:
    - legacy.**
profiles:
  pg:
    vars:
      port: 18085
    override:
      "app:db:kind": db.sql.postgres
    disable:
      namespaces:
        add:
          - kickside.research.**
  lean:
    disable:
      namespaces:
        remove:
          - legacy.**
      entries:
        add:
          - app:heavy_worker
`)
	require.NoError(t, os.WriteFile(cfgPath, cfgBody, 0o644))

	prevConfigFile := configFile
	prevProfiler := profiler
	prevVerbose := verbose
	prevVeryVerbose := veryVerbose
	prevConsole := console
	prevEventStreams := eventStreams
	configFile = cfgPath
	profiler, verbose, veryVerbose, console, eventStreams = false, false, false, false, false
	t.Cleanup(func() {
		configFile = prevConfigFile
		profiler = prevProfiler
		verbose = prevVerbose
		veryVerbose = prevVeryVerbose
		console = prevConsole
		eventStreams = prevEventStreams
	})

	cmd := &cobra.Command{}
	cmd.Flags().StringArray("profile", nil, "")
	cmd.Flags().StringArray("set", nil, "")
	cmd.Flags().StringSlice("override", nil, "")
	require.NoError(t, cmd.Flags().Set("profile", "pg"))
	require.NoError(t, cmd.Flags().Set("profile", "lean"))
	require.NoError(t, cmd.Flags().Set("set", "vars.port=19000"))

	cfg, err := loadRuntimeConfig(cmd, zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, ":19000", cfg.GetString("override.app:gateway:addr", ""))
	require.Equal(t, "db.sql.postgres", cfg.GetString("override.app:db:kind", ""))

	namespaces, ok := cfg.Get("disable.namespaces")
	require.True(t, ok)
	require.Equal(t, []string{"kickside.research.**"}, namespaces)

	entries, ok := cfg.Get("disable.entries")
	require.True(t, ok)
	require.Equal(t, []string{"app:heavy_worker"}, entries)
	require.Empty(t, cfg.Sub("profiles").Keys())
}

func TestLoadRuntimeConfig_ProfileFromPackDefaults(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "wippy.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("version: \"1.0\"\n"), 0o644))

	prevConfigFile := configFile
	prevProfiler := profiler
	configFile = cfgPath
	profiler = false
	t.Cleanup(func() {
		configFile = prevConfigFile
		profiler = prevProfiler
	})

	runtimeDefaults := boot.NewConfig(
		boot.WithSection("override", map[string]any{
			"app:db:kind": "db.sql.sqlite",
		}),
		boot.WithSection("profiles", map[string]any{
			"pg.override.app:db:kind": "db.sql.postgres",
		}),
	)

	cmd := &cobra.Command{}
	cmd.Flags().StringArray("profile", nil, "")
	require.NoError(t, cmd.Flags().Set("profile", "pg"))

	cfg, err := loadRuntimeConfigWithDefaults(cmd, zap.NewNop(), runtimeDefaults)
	require.NoError(t, err)
	require.Equal(t, "db.sql.postgres", cfg.GetString("override.app:db:kind", ""))
}
