// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestRuntimeConfigFromPackMetadata_DottedKeys(t *testing.T) {
	cfg := runtimeConfigFromPackMetadata(wapp.Metadata{
		"runtime.lsp.enabled":           true,
		"runtime.lsp.max_message_bytes": "2048",
		"runtime.logger.level":          "debug",
	}, zap.NewNop())

	require.NotNil(t, cfg)
	require.True(t, cfg.GetBool("lsp.enabled", false))
	require.Equal(t, 2048, cfg.GetInt("lsp.max_message_bytes", 0))
	require.Equal(t, "debug", cfg.GetString("logger.level", ""))
}

func TestRuntimeConfigFromPackMetadata_NestedRuntimeMap(t *testing.T) {
	cfg := runtimeConfigFromPackMetadata(wapp.Metadata{
		"runtime": map[string]any{
			"lsp": map[string]any{
				"enabled": "true",
			},
			"logger": map[string]any{
				"encoding": "console",
			},
		},
	}, zap.NewNop())

	require.NotNil(t, cfg)
	require.True(t, cfg.GetBool("lsp.enabled", false))
	require.Equal(t, "console", cfg.GetString("logger.encoding", ""))
}

func TestLoadPackRuntimeDefaultsFromFiles_MergeOrder(t *testing.T) {
	tmpDir := t.TempDir()

	basePack := filepath.Join(tmpDir, "base.wapp")
	require.NoError(t, writeTestPack(basePack, wapp.Metadata{
		"runtime.lsp.enabled":  true,
		"runtime.logger.level": "info",
	}))

	overridePack := filepath.Join(tmpDir, "override.wapp")
	require.NoError(t, writeTestPack(overridePack, wapp.Metadata{
		"runtime.lsp.enabled": false,
	}))

	cfg, err := loadPackRuntimeDefaultsFromFiles([]string{basePack, overridePack}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.False(t, cfg.GetBool("lsp.enabled", true))
	require.Equal(t, "info", cfg.GetString("logger.level", ""))
}

func writeTestPack(path string, metadata wapp.Metadata) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}

	writer := wapp.NewWriter()
	if err := writer.PackEntries(metadata, nil, file); err != nil {
		_ = file.Close()
		return err
	}

	return file.Close()
}
