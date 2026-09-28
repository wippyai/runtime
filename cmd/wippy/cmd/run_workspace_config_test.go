// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func resetRuntimeFlagGlobals(t *testing.T) {
	t.Helper()
	prevProfiler, prevVerbose, prevVeryVerbose, prevConsole, prevEventStreams := profiler, verbose, veryVerbose, console, eventStreams
	profiler, verbose, veryVerbose, console, eventStreams = false, false, false, false, false
	t.Cleanup(func() {
		profiler, verbose, veryVerbose, console, eventStreams = prevProfiler, prevVerbose, prevVeryVerbose, prevConsole, prevEventStreams
	})
}

func runtimeConfigCommand(t *testing.T, profiles []string, sets []string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().StringArray("profile", nil, "")
	cmd.Flags().StringArray("set", nil, "")
	cmd.Flags().StringSlice("override", nil, "")
	for _, profile := range profiles {
		require.NoError(t, cmd.Flags().Set("profile", profile))
	}
	for _, set := range sets {
		require.NoError(t, cmd.Flags().Set("set", set))
	}
	return cmd
}

func packDefaultsWithProfile(profile string) boot.Config {
	return boot.NewConfig(
		boot.WithSection("vars", map[string]any{"port": 8085}),
		boot.WithSection("profiles", map[string]any{
			profile + ".registry.history_type": "postgres",
		}),
	)
}

func TestLoadWorkspaceConfigDefersProfilesUndefinedLocally(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".wippy.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`version: "1.0"
profiles:
  local:
    workspace:
      replacements:
        acme/app: ./app
`), 0o600))
	setTestConfigFiles(t, cfgPath)
	resetRuntimeFlagGlobals(t)

	cmd := runtimeConfigCommand(t, []string{"local", "pg"}, nil)

	workspaceCfg, err := loadWorkspaceConfig(cmd, zap.NewNop())
	require.NoError(t, err)
	replacements, err := lock.WorkspaceReplacements(workspaceCfg)
	require.NoError(t, err)
	require.Equal(t, []lock.Replacement{{From: "acme/app", To: filepath.Join(dir, "app")}}, replacements)

	cfg, err := loadRuntimeConfigWithDefaults(cmd, zap.NewNop(), packDefaultsWithProfile("pg"))
	require.NoError(t, err)
	require.Equal(t, "postgres", cfg.GetString("registry.history_type", ""))

	_, err = loadRuntimeConfigWithDefaults(cmd, zap.NewNop(), nil)
	require.ErrorContains(t, err, `profile "pg" not found`)
}

func TestLoadWorkspaceConfigSetOverridesProfileReplacement(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".wippy.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`version: "1.0"
profiles:
  local:
    workspace:
      replacements:
        acme/app: ./app
`), 0o600))
	setTestConfigFiles(t, cfgPath)
	resetRuntimeFlagGlobals(t)

	cmd := runtimeConfigCommand(t, []string{"local"}, []string{"workspace.replacements.acme/app=./other"})

	workspaceCfg, err := loadWorkspaceConfig(cmd, zap.NewNop())
	require.NoError(t, err)
	replacements, err := lock.WorkspaceReplacements(workspaceCfg)
	require.NoError(t, err)
	require.Equal(t, []lock.Replacement{{From: "acme/app", To: filepath.Join(dir, "other")}}, replacements)
}

func TestLoadWorkspaceConfigMatchesRuntimeConfigWorkspace(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.yaml")
	overlayPath := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(basePath, []byte(`version: "1.0"
vars:
  modules: ../modules
workspace:
  replacements:
    acme/app: ${modules}/app
    acme/lib: ./lib
    acme/gone: ./gone
override:
  "app:gateway:addr": ":${port}"
profiles:
  local:
    workspace:
      replacements:
        acme/tool: ${modules}/tool
`), 0o600))
	require.NoError(t, os.WriteFile(overlayPath, []byte(`version: "1.0"
workspace:
  replacements:
    acme/lib: ./lib-overlay
    acme/gone: null
`), 0o600))
	setTestConfigFiles(t, basePath, overlayPath)
	resetRuntimeFlagGlobals(t)

	cmd := runtimeConfigCommand(t, []string{"local", "pg"}, []string{"workspace.replacements.acme/cli=./cli"})

	workspaceCfg, err := loadWorkspaceConfig(cmd, zap.NewNop())
	require.NoError(t, err)
	fromWorkspace, err := lock.WorkspaceReplacements(workspaceCfg)
	require.NoError(t, err)

	cfg, err := loadRuntimeConfigWithDefaults(cmd, zap.NewNop(), packDefaultsWithProfile("pg"))
	require.NoError(t, err)
	fromRuntime, err := lock.WorkspaceReplacements(cfg)
	require.NoError(t, err)

	require.Equal(t, fromRuntime, fromWorkspace)
	require.Equal(t, []lock.Replacement{
		{From: "acme/app", To: filepath.Join(dir, "../modules/app")},
		{From: "acme/cli", To: filepath.Join(dir, "cli")},
		{From: "acme/lib", To: filepath.Join(dir, "lib-overlay")},
		{From: "acme/tool", To: filepath.Join(dir, "../modules/tool")},
	}, fromWorkspace)
	require.Equal(t, ":8085", cfg.GetString("override.app:gateway:addr", ""))
}

func TestPackedProfileCannotRedirectLocalWorkspaceReplacement(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".wippy.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`version: "1.0"
vars:
  sourceRoot: ./local
workspace:
  replacements:
    acme/dep: ${sourceRoot}/dep
`), 0o600))
	lockPath := filepath.Join(dir, defaultLockFile)
	locked, err := lock.New(lockPath)
	require.NoError(t, err)
	locked.SetModule(lock.Module{Name: "acme/app", Version: "1.0.0", Root: true})
	require.NoError(t, locked.Write())
	packPath := filepath.Join(dir, ".wippy", "vendor", "acme", "app-1.0.0.wapp")
	require.NoError(t, os.MkdirAll(filepath.Dir(packPath), 0o755))
	require.NoError(t, writeTestPack(packPath, wapp.Metadata{
		"runtime.profiles.prod.vars.sourceRoot":       "./packed",
		"runtime.profiles.prod.registry.history_type": "postgres",
	}))
	setTestConfigFiles(t, cfgPath)
	resetRuntimeFlagGlobals(t)

	cmd := runtimeConfigCommand(t, []string{"prod"}, nil)
	workspaceCfg, err := loadWorkspaceConfig(cmd, zap.NewNop())
	require.NoError(t, err)
	defaults, err := loadLockRootRuntimeDefaults(lockPath, workspaceCfg, zap.NewNop())
	require.NoError(t, err)
	fullCfg, err := loadRuntimeConfigWithPinnedWorkspace(cmd, zap.NewNop(), defaults, workspaceCfg)
	require.NoError(t, err)

	selected, err := lock.WorkspaceReplacements(fullCfg)
	require.NoError(t, err)
	require.Equal(t, []lock.Replacement{{From: "acme/dep", To: filepath.Join(dir, "local", "dep")}}, selected)
	require.Equal(t, "./packed", fullCfg.GetString("vars.sourceRoot", ""))
	require.Equal(t, "postgres", fullCfg.GetString("registry.history_type", ""))
}

func TestLoadWorkspaceConfigLeavesPackVariablesOutsideWorkspaceToFullResolution(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".wippy.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`version: "1.0"
override:
  "app:gateway:addr": ":${port}"
`), 0o600))
	setTestConfigFiles(t, cfgPath)
	resetRuntimeFlagGlobals(t)

	cmd := runtimeConfigCommand(t, nil, nil)

	_, err := loadWorkspaceConfig(cmd, zap.NewNop())
	require.NoError(t, err)

	cfg, err := loadRuntimeConfigWithDefaults(cmd, zap.NewNop(), packDefaultsWithProfile("pg"))
	require.NoError(t, err)
	require.Equal(t, ":8085", cfg.GetString("override.app:gateway:addr", ""))
}

func TestLoadWorkspaceConfigReportsUnresolvedWorkspaceVariable(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".wippy.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`version: "1.0"
workspace:
  replacements:
    acme/app: ${missing}/app
`), 0o600))
	setTestConfigFiles(t, cfgPath)
	resetRuntimeFlagGlobals(t)

	_, err := loadWorkspaceConfig(runtimeConfigCommand(t, nil, nil), zap.NewNop())
	require.ErrorContains(t, err, `variable "missing" not found`)
}

// TestRunWithUseCaseWorkspaceReplacedRootBoots boots a test-harness layout: the
// lock selects the harness as the deployment root, the harness directory
// supplies it through a workspace replacement, and the module under test is
// the lock's src directory.
func TestRunWithUseCaseWorkspaceReplacedRootBoots(t *testing.T) {
	projectDir := t.TempDir()
	moduleSrc := filepath.Join(projectDir, "module", "src")
	harnessDir := filepath.Join(projectDir, "module", "test")
	require.NoError(t, os.MkdirAll(moduleSrc, 0o755))
	require.NoError(t, os.MkdirAll(harnessDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(moduleSrc, "_index.yaml"), []byte(`version: "1.0"
namespace: acme.app
entries:
  - name: answer
    kind: library.lua
    source: |
      return { value = 42 }
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(harnessDir, "wippy.yaml"), []byte(`organization: acme
module: app-harness
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(harnessDir, "_index.yaml"), []byte(`version: "1.0"
namespace: app
entries:
  - name: terminal
    kind: terminal.host
    lifecycle:
      auto_start: true
  - name: probe
    kind: process.lua
    method: main
    imports:
      answer: acme.app:answer
    source: |
      local answer = require("answer")
      return {main = function()
        assert(answer.value == 42, "module under test did not load")
      end}
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(harnessDir, ".wippy.yaml"), []byte(`version: "1.0"
workspace:
  replacements:
    acme/app-harness: .
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(harnessDir, defaultLockFile), []byte(`directories:
  modules: .wippy
  src: ../src
modules:
  - name: acme/app-harness
    version: 0.1.0
    root: true
`), 0o600))

	t.Chdir(harnessDir)
	setTestConfigFiles(t)
	oldSilent := silentLogs
	t.Cleanup(func() { silentLogs = oldSilent })

	cmd := &cobra.Command{}
	cmd.Flags().String("exec", "", "")
	cmd.Flags().String("host", "", "")
	cmd.Flags().String("registry", "", "")
	require.NoError(t, cmd.ParseFlags([]string{"--exec", "app:probe"}))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd.SetContext(ctx)
	require.NoError(t, runWithUseCase(cmd, cmd.Flags().Args(), defaultUseCase))
}
