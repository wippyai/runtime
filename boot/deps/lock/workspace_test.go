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

func TestWithWorkspaceConfigOverridesUnpackModulesWithoutPersisting(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), DefaultFilename)
	lockObj, err := New(lockPath)
	require.NoError(t, err)
	lockObj.SetOptions(Options{UnpackModules: true})
	require.NoError(t, lockObj.Write())

	cfg := boot.NewConfig(boot.WithSection("options", map[string]any{"unpack_modules": false}))
	lockObj, err = New(lockPath, WithWorkspaceConfig(cfg))
	require.NoError(t, err)
	require.False(t, lockObj.ShouldUnpackModules())
	require.True(t, lockObj.GetOptions().UnpackModules)

	require.NoError(t, lockObj.Write())
	persisted, err := New(lockPath)
	require.NoError(t, err)
	require.True(t, persisted.ShouldUnpackModules())

	lockObj.SetUnpackModulesOverride(nil)
	require.True(t, lockObj.ShouldUnpackModules())
}

func TestWithWorkspaceConfigKeepsLockUnpackModulesWhenUnset(t *testing.T) {
	lockObj, err := New(filepath.Join(t.TempDir(), DefaultFilename))
	require.NoError(t, err)
	lockObj.SetOptions(Options{UnpackModules: true})

	require.NoError(t, WithWorkspaceConfig(boot.NewConfig())(lockObj))
	require.True(t, lockObj.ShouldUnpackModules())
}

func TestWithWorkspaceConfigRejectsNonBoolUnpackModules(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection("options", map[string]any{"unpack_modules": "no"}))
	_, err := New(filepath.Join(t.TempDir(), DefaultFilename), WithWorkspaceConfig(cfg))
	require.ErrorContains(t, err, "options.unpack_modules must be a boolean")
}

func TestWorkspaceUnpackModulesCanonicalPrecedence(t *testing.T) {
	for _, tc := range []struct {
		canonical    any
		previous     any
		name         string
		canonicalSet bool
		previousSet  bool
		want         bool
		override     bool
		invalid      bool
	}{
		{name: "unset"},
		{name: "canonical-true", canonical: true, canonicalSet: true, want: true, override: true},
		{name: "canonical-false", canonical: false, canonicalSet: true, override: true},
		{name: "canonical-wins", canonical: false, canonicalSet: true, previous: true, previousSet: true, override: true},
		{name: "canonical-reset", canonicalSet: true, previous: true, previousSet: true},
		{name: "previous-true", previous: true, previousSet: true, want: true, override: true},
		{name: "previous-false", previous: false, previousSet: true, override: true},
		{name: "previous-reset", previousSet: true},
		{name: "canonical-invalid", canonical: "true", canonicalSet: true, previous: true, previousSet: true, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := map[string]any{}
			previous := map[string]any{}
			if tc.canonicalSet {
				workspace["unpack_modules"] = tc.canonical
			}
			if tc.previousSet {
				previous["unpack_modules"] = tc.previous
			}
			cfg := boot.NewConfig(boot.WithSection("workspace", workspace), boot.WithSection("options", previous))
			got, err := WorkspaceUnpackModules(cfg)
			if tc.invalid {
				require.ErrorContains(t, err, "workspace.unpack_modules must be a boolean")
				return
			}
			require.NoError(t, err)
			if !tc.override {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tc.want, *got)
		})
	}
}

func TestWithWorkspaceConfigCanonicalUnpackDoesNotPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultFilename)
	locked, err := New(path)
	require.NoError(t, err)
	require.NoError(t, locked.Write())
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	cfg := boot.NewConfig(boot.WithSection("workspace", map[string]any{"unpack_modules": true}))
	locked, err = New(path, WithWorkspaceConfig(cfg))
	require.NoError(t, err)
	require.True(t, locked.ShouldUnpackModules())
	require.False(t, locked.GetOptions().UnpackModules)
	require.NoError(t, locked.Write())
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
