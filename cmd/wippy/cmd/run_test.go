package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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
