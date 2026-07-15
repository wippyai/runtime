// SPDX-License-Identifier: MPL-2.0

package lock

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
)

func TestWorkspaceReplacementsUsesConfigDirectory(t *testing.T) {
	configDir := t.TempDir()
	cfg := boot.NewConfig(
		boot.WithSection("boot", map[string]any{"config_dir": configDir}),
		boot.WithSection("workspace", map[string]any{
			"replacements.acme/http":     "../http",
			"replacements.acme/disabled": nil,
		}),
	)

	replacements, err := WorkspaceReplacements(cfg)
	require.NoError(t, err)
	require.Equal(t, []Replacement{{
		From: "acme/http",
		To:   filepath.Join(configDir, "../http"),
	}}, replacements)
}

func TestWorkspaceReplacementsRejectsNonStringPath(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection("workspace", map[string]any{
		"replacements.acme/http": true,
	}))

	_, err := WorkspaceReplacements(cfg)
	require.ErrorContains(t, err, "path must be a string or null")
}

func TestWorkspaceReplacementWinsOverTracked(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, DefaultFilename)
	require.NoError(t, os.WriteFile(lockPath, []byte(`directories:
  modules: .wippy
  src: ./src
replacements:
  - from: acme/http
    to: ./tracked
`), 0o600))

	lockObj, err := New(lockPath, WithWorkspaceReplacements([]Replacement{
		{From: "acme/http", To: "./workspace"},
	}))
	require.NoError(t, err)
	replacement, ok := lockObj.GetReplacement("acme/http")
	require.True(t, ok)
	require.Equal(t, "./workspace", replacement.To)
	require.Len(t, lockObj.GetReplacements(), 1)
}
