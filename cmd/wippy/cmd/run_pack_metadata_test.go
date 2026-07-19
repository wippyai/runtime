// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestLoadLockRootRuntimeDefaults(t *testing.T) {
	projectDir := t.TempDir()
	lockPath := filepath.Join(projectDir, defaultLockFile)
	lockObj, err := lock.New(lockPath)
	require.NoError(t, err)
	lockObj.SetModule(lock.Module{Name: "acme/app", Version: "1.2.3", Root: true})
	require.NoError(t, lockObj.Write())

	packPath := filepath.Join(projectDir, ".wippy", "vendor", "acme", "app-1.2.3.wapp")
	require.NoError(t, os.MkdirAll(filepath.Dir(packPath), 0o755))
	require.NoError(t, writeTestPack(packPath, wapp.Metadata{
		"runtime.profiles.postgres.registry.history_type": "postgres",
		"runtime.profiles.postgres.registry.enabled":      true,
	}))

	defaults, err := loadLockRootRuntimeDefaults(lockPath, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, defaults)
	resolved, err := configForProfile(defaults, "postgres")
	require.NoError(t, err)
	require.Equal(t, "postgres", resolved.GetString("registry.history_type", ""))
	require.True(t, resolved.GetBool("registry.enabled", false))
}

func TestLoadLockRootRuntimeDefaultsRejectsMissingSelectedPack(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), defaultLockFile)
	lockObj, err := lock.New(lockPath)
	require.NoError(t, err)
	lockObj.SetModule(lock.Module{Name: "acme/app", Version: "1.2.3", Root: true})
	require.NoError(t, lockObj.Write())

	_, err = loadLockRootRuntimeDefaults(lockPath, zap.NewNop())
	require.ErrorContains(t, err, "selected deployment root acme/app is not installed")
}

func TestMaterializeHubRunPackInstallsExactLockArtifact(t *testing.T) {
	projectDir := t.TempDir()
	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(projectDir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previousDir)) })

	source := filepath.Join(t.TempDir(), "app.wapp")
	require.NoError(t, writeTestPack(source, wapp.Metadata{"name": "app"}))
	content, err := os.ReadFile(source)
	require.NoError(t, err)
	sum := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	destination, err := materializeHubRunPack(source, "acme/app", "1.2.3", digest, uint64(len(content)))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(projectDir, ".wippy", "vendor", "acme", "app-1.2.3.wapp"), destination)
	installed, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, content, installed)

	second, err := materializeHubRunPack(source, "acme/app", "1.2.3", digest, uint64(len(content)))
	require.NoError(t, err)
	require.Equal(t, destination, second)
}

func configForProfile(defaults boot.Config, profile string) (boot.Config, error) {
	return bootconfig.ApplyProfiles(defaults, []string{profile})
}

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

func TestPublishedApplicationRuntimeConfigSurvivesPackRoundTripWithoutLocalConfig(t *testing.T) {
	publisherDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(publisherDir, "wippy.yaml"), []byte(`
organization: kickside
module: kickside
type: application
publish:
  runtime:
    sections:
      - security
      - registry
      - override
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(publisherDir, ".wippy.yaml"), []byte(`
version: "1.0"
security:
  strict_mode: true
registry:
  enable_history: true
  history_type: sqlite
  history_path: ./.wippy/registry.db
  event_wait_timeout: 120s
vars:
  public_url: http://localhost:8085
override:
  "app.env:defaults:values.PUBLIC_API_URL": "${public_url}"
`), 0o600))

	manifest, err := config.Load(publisherDir)
	require.NoError(t, err)
	require.NoError(t, manifest.Validate())

	metadata := attrs.Bag{}
	require.NoError(t, addPublishedRuntimeMetadata(metadata, publisherDir, manifest.Publish))

	packDir := t.TempDir()
	dependencyPack := filepath.Join(packDir, "dependency.wapp")
	require.NoError(t, writeTestPack(dependencyPack, wapp.Metadata{
		"runtime": map[string]any{
			"security": map[string]any{"strict_mode": false},
			"registry": map[string]any{"history_type": "memory"},
			"logger":   map[string]any{"level": "dependency-must-not-leak"},
		},
	}))
	applicationPack := filepath.Join(packDir, "application.wapp")
	require.NoError(t, writeTestPack(applicationPack, wapp.Metadata(metadata)))

	packDefaults, err := loadPackRuntimeDefaultsFromFiles(
		[]string{dependencyPack, applicationPack},
		zap.NewNop(),
	)
	require.NoError(t, err)

	destinationDir := t.TempDir()
	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(destinationDir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previousDir)) })
	setTestConfigFiles(t)

	previousProfiler := profiler
	profiler = false
	t.Cleanup(func() { profiler = previousProfiler })

	effective, err := loadRuntimeConfigWithDefaults(nil, zap.NewNop(), packDefaults)
	require.NoError(t, err)
	require.True(t, effective.GetBool("security.strict_mode", false))
	require.True(t, effective.GetBool("registry.enable_history", false))
	require.Equal(t, "sqlite", effective.GetString("registry.history_type", ""))
	require.Equal(t, "./.wippy/registry.db", effective.GetString("registry.history_path", ""))
	require.Equal(t, "120s", effective.GetString("registry.event_wait_timeout", ""))
	require.Equal(t, "http://localhost:8085", effective.GetString("vars.public_url", ""))
	require.Equal(
		t, "http://localhost:8085",
		effective.GetString("override.app.env:defaults:values.PUBLIC_API_URL", ""),
	)
	require.Equal(t, "", effective.GetString("logger.level", ""))
}

func TestRawPackRuntimeProfilesSurvivePackRoundTrip(t *testing.T) {
	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "wippy.yaml"), []byte(`
organization: acme
module: app
type: application
publish:
  runtime:
    sections:
      - registry
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, ".wippy.yaml"), []byte(`
version: "1.0"
registry:
  enable_history: true
  history_type: sqlite
vars:
  postgres_host: localhost
  publisher_secret: "${env:PUBLISHER_SECRET}"
profiles:
  postgres:
    vars:
      postgres_host: db.internal
    registry:
      history_type: postgres
      postgres_host: "${postgres_host}"
workspace:
  replacements:
    acme/database: ../database
`), 0o600))

	metadata := attrs.Bag{}
	require.NoError(t, addPackRuntimeMetadata(metadata, projectDir))

	packPath := filepath.Join(t.TempDir(), "application.wapp")
	require.NoError(t, writeTestPack(packPath, wapp.Metadata(metadata)))
	packDefaults, err := loadPackRuntimeDefaults(packPath, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, packDefaults)

	destinationDir := t.TempDir()
	previousDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(destinationDir))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previousDir)) })
	setTestConfigFiles(t)

	previousProfiler := profiler
	profiler = false
	t.Cleanup(func() { profiler = previousProfiler })

	cmd := &cobra.Command{}
	cmd.Flags().StringArray("profile", nil, "")
	require.NoError(t, cmd.Flags().Set("profile", "postgres"))

	effective, err := loadRuntimeConfigWithDefaults(cmd, zap.NewNop(), packDefaults)
	require.NoError(t, err)
	require.True(t, effective.GetBool("registry.enable_history", false))
	require.Equal(t, "postgres", effective.GetString("registry.history_type", ""))
	require.Equal(t, "db.internal", effective.GetString("registry.postgres_host", ""))
	require.Equal(t, "", effective.GetString("vars.publisher_secret", ""))
	require.Empty(t, effective.Sub("workspace").Keys())
}

func TestAddPackRuntimeMetadataWithoutManifestIsNoop(t *testing.T) {
	metadata := attrs.Bag{"description": "snapshot"}
	require.NoError(t, addPackRuntimeMetadata(metadata, t.TempDir()))
	require.Equal(t, attrs.Bag{"description": "snapshot"}, metadata)
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
