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

func TestRuntimeConfigFromPackMetadata_NestedRuntimeMapPreservesScalarTypes(t *testing.T) {
	cfg := runtimeConfigFromPackMetadata(wapp.Metadata{
		"runtime": map[string]any{
			"lsp": map[string]any{
				"enabled": "true",
			},
			"logger": map[string]any{
				"encoding": "console",
				"label":    "  padded  ",
			},
			"vars": map[string]any{
				"port":    "5432",
				"enabled": "false",
			},
		},
	}, zap.NewNop())

	require.NotNil(t, cfg)
	require.Equal(t, "true", cfg.GetString("lsp.enabled", ""))
	require.Equal(t, "console", cfg.GetString("logger.encoding", ""))
	require.Equal(t, "  padded  ", cfg.GetString("logger.label", ""))
	require.Equal(t, "5432", cfg.GetString("vars.port", ""))
	require.Equal(t, "false", cfg.GetString("vars.enabled", ""))
}

func TestRuntimeConfigFromPackMetadata_PreservesProfiles(t *testing.T) {
	cfg := runtimeConfigFromPackMetadata(wapp.Metadata{
		"runtime": map[string]any{
			"profiles": map[string]any{
				"pg": map[string]any{
					"override": map[string]any{
						"app:db:kind": "db.sql.postgres",
					},
					"disable": map[string]any{
						"namespaces": map[string]any{
							"add": []any{"kickside.research.**"},
						},
					},
				},
			},
		},
	}, zap.NewNop())

	require.NotNil(t, cfg)
	require.Equal(t, "db.sql.postgres", cfg.GetString("profiles.pg.override.app:db:kind", ""))

	namespaces, ok := cfg.Get("profiles.pg.disable.namespaces.add")
	require.True(t, ok)
	require.Equal(t, []any{"kickside.research.**"}, namespaces)
}

func TestLoadPackRuntimeDefaultsFromFiles_UsesOnlyMainPack(t *testing.T) {
	tmpDir := t.TempDir()

	dependencyPack := filepath.Join(tmpDir, "dependency.wapp")
	require.NoError(t, writeTestPack(dependencyPack, wapp.Metadata{
		"runtime.security.strict_mode": false,
		"runtime.logger.level":         "dependency",
	}))

	mainPack := filepath.Join(tmpDir, "main.wapp")
	require.NoError(t, writeTestPack(mainPack, wapp.Metadata{
		"runtime.security.strict_mode":  true,
		"runtime.registry.history_type": "memory",
	}))

	cfg, err := loadPackRuntimeDefaultsFromFiles([]string{dependencyPack, mainPack}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.True(t, cfg.GetBool("security.strict_mode", false))
	require.Equal(t, "memory", cfg.GetString("registry.history_type", ""))
	require.Equal(t, "", cfg.GetString("logger.level", ""))
}

func TestLoadPackRuntimeDefaultsFromFiles_DependencyCannotContributeConfig(t *testing.T) {
	tmpDir := t.TempDir()

	depPack := filepath.Join(tmpDir, "dep.wapp")
	require.NoError(t, writeTestPack(depPack, wapp.Metadata{
		"runtime.logger.level":                      "info",
		"runtime.vars.dep_only":                     "leaked",
		"runtime.profiles.dep.override.app:db:kind": "db.sql.sqlite",
	}))

	mainPack := filepath.Join(tmpDir, "main.wapp")
	require.NoError(t, writeTestPack(mainPack, wapp.Metadata{
		"runtime.vars.main_only":                     "kept",
		"runtime.profiles.main.override.app:db:kind": "db.sql.postgres",
	}))

	cfg, err := loadPackRuntimeDefaultsFromFiles([]string{depPack, mainPack}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "", cfg.GetString("logger.level", ""))
	require.Equal(t, "", cfg.GetString("vars.dep_only", ""))
	require.Equal(t, "kept", cfg.GetString("vars.main_only", ""))
	require.Equal(t, "", cfg.GetString("profiles.dep.override.app:db:kind", ""))
	require.Equal(t, "db.sql.postgres", cfg.GetString("profiles.main.override.app:db:kind", ""))
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
