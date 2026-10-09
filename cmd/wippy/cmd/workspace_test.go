// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestWorkspaceReplacementsComposeThroughProfiles(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".wippy.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`version: "1.0"
workspace:
  replacements:
    acme/base: ../base
    acme/http: ../default-http
profiles:
  local:
    workspace:
      replacements:
        acme/http: ../local-http
        acme/extra: ../extra
  clean:
    workspace:
      replacements:
        acme/base: null
`), 0o600))

	cfg, err := bootconfig.Load(configPath)
	require.NoError(t, err)
	cfg, err = bootconfig.ApplyProfiles(cfg, []string{"local", "clean"})
	require.NoError(t, err)

	replacements, err := lock.WorkspaceReplacements(cfg)
	require.NoError(t, err)
	require.Equal(t, []lock.Replacement{
		{From: "acme/extra", To: "../extra"},
		{From: "acme/http", To: "../local-http"},
	}, replacements)
}

func TestConfiguredLockWarnsForTrackedReplacements(t *testing.T) {
	oldSilentLogs := silentLogs
	silentLogs = false
	t.Cleanup(func() { silentLogs = oldSilentLogs })
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, lock.DefaultFilename)
	require.NoError(t, os.WriteFile(lockPath, []byte(`directories:
  modules: .wippy
  src: ./src
replacements:
  - from: acme/http
    to: ./http
`), 0o600))

	core, observed := observer.New(zap.WarnLevel)
	_, err := newConfiguredLock(lockPath, nil, zap.New(core))
	require.NoError(t, err)
	require.Len(t, observed.All(), 1)
	require.Contains(t, observed.All()[0].Message, "DEPRECATED")
	require.Contains(t, observed.All()[0].Message, "workspace.replacements")
}

func TestCanonicalWorkspaceControlsComposeThroughFilesProfilesAndSet(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yaml")
	overlay := filepath.Join(dir, "overlay.yaml")
	require.NoError(t, os.WriteFile(base, []byte(`version: "1.0"
options:
  unpack_modules: false
workspace:
  unpack_modules: false
  include_source_dependencies: false
  replacements:
    acme/app: ./app
profiles:
  isolated:
    workspace:
      unpack_modules: false
      include_source_dependencies: false
  reset:
    workspace:
      unpack_modules: null
      include_source_dependencies: null
`), 0o600))
	require.NoError(t, os.WriteFile(overlay, []byte(`version: "1.0"
workspace:
  unpack_modules: true
  include_source_dependencies: true
  replacements:
    acme/app: ./overlay-app
`), 0o600))
	setTestConfigFiles(t, base, overlay)
	resetRuntimeFlagGlobals(t)
	path := filepath.Join(dir, defaultLockFile)
	locked, err := lock.New(path)
	require.NoError(t, err)
	locked.SetOptions(lock.Options{UnpackModules: true})
	require.NoError(t, locked.Write())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, tc := range []struct {
		name       string
		profiles   []string
		sets       []string
		wantUnpack bool
		wantSource bool
	}{
		{name: "later-file", wantUnpack: true, wantSource: true},
		{name: "profile", profiles: []string{"isolated"}},
		{name: "set", profiles: []string{"isolated"}, sets: []string{"workspace.unpack_modules=true", "workspace.include_source_dependencies=true"}, wantUnpack: true, wantSource: true},
		{name: "canonical-beats-old-key", sets: []string{"options.unpack_modules=false"}, wantUnpack: true, wantSource: true},
		{name: "reset-to-lock", profiles: []string{"isolated", "reset"}, wantUnpack: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := runtimeConfigCommand(t, tc.profiles, tc.sets)
			early, err := loadWorkspaceConfig(cmd, zap.NewNop())
			require.NoError(t, err)
			full, err := loadRuntimeConfigWithPinnedWorkspace(cmd, zap.NewNop(), nil, early)
			require.NoError(t, err)
			for _, cfg := range []boot.Config{early, full} {
				configured, err := newConfiguredLock(path, cfg, zap.NewNop())
				require.NoError(t, err)
				require.Equal(t, tc.wantUnpack, configured.ShouldUnpackModules())
				enabled, err := includeSourceDependencies(cfg)
				require.NoError(t, err)
				require.Equal(t, tc.wantSource, enabled)
				replacement, ok := configured.GetReplacement("acme/app")
				require.True(t, ok)
				require.Equal(t, filepath.Join(dir, "overlay-app"), replacement.To)
			}
		})
	}
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "runtime composition must not persist workspace controls")
}
