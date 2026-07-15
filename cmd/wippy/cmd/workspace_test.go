// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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
