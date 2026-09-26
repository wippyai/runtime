// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfinementPatchOmittedAndEmptyLists(t *testing.T) {
	var patch ConfinementPatch
	require.NoError(t, json.Unmarshal([]byte(`{"fs":{"read":[],"write":["/tmp"]}}`), &patch))
	require.NotNil(t, patch.FS)
	require.NotNil(t, patch.FS.Read, "present empty list must deny all")
	require.Empty(t, *patch.FS.Read)
	require.Nil(t, patch.FS.Exec, "omitted list must inherit")

	clone, err := (ProcessOptions{Confine: &patch}).Clone()
	require.NoError(t, err)
	*patch.FS.Write = append(*patch.FS.Write, "/etc")
	require.Equal(t, []string{"/tmp"}, *clone.Confine.FS.Write)
	require.NotNil(t, clone.Confine.FS.Read)
	require.Empty(t, *clone.Confine.FS.Read)
	require.Nil(t, clone.Confine.FS.Exec)

	data, err := json.Marshal(clone.Confine)
	require.NoError(t, err)
	require.Contains(t, string(data), `"read":[]`)
	require.NotContains(t, string(data), `"exec"`)
}

func TestConfinementEntryShape(t *testing.T) {
	var cfg NativeExecutorConfig
	require.NoError(t, json.Unmarshal([]byte(`{
		"confine": {
			"work_dir_roots": ["/srv/ws/demo"],
			"fs": {"read": ["/srv/ws/demo"], "write": ["{tmp}", "{home}"]},
			"home": "private",
			"env": {"allow": ["LANG"], "set": {"PATH": "/usr/bin"}},
			"network": "none",
			"limits": {"mem_mb": 4096, "pids": 256, "wall_s": 60},
			"tree": {"kill_on_owner_exit": true}
		}
	}`), &cfg))
	require.NotNil(t, cfg.Confine)
	require.Equal(t, []string{"/srv/ws/demo"}, cfg.Confine.WorkDirRoots)
	require.Equal(t, "none", cfg.Confine.Network)
	require.EqualValues(t, 4096, cfg.Confine.Limits.MemoryMiB)
	require.True(t, cfg.Confine.Tree.KillOnOwnerExit)
	cfg.Confine.WorkDirRoots[0] = confineTestPath("/srv/ws/demo")
	cfg.Confine.FS.Read[0] = confineTestPath("/srv/ws/demo")
	require.NoError(t, cfg.Validate())
}

func TestConfinementRejectsMalformedEntry(t *testing.T) {
	tests := []struct {
		name    string
		confine Confinement
	}{
		{"missing roots", Confinement{Network: "none"}},
		{"caller-selected grant", Confinement{WorkDirRoots: []string{confineTestPath("/srv/ws")}, FS: &ConfinementFS{Read: []string{"{workdir}"}}}},
		{"relative root", Confinement{WorkDirRoots: []string{"relative"}, Network: "none"}},
		{"unknown network", Confinement{WorkDirRoots: []string{confineTestPath("/srv/ws")}, Network: "loopback"}},
		{"negative limit", Confinement{WorkDirRoots: []string{confineTestPath("/srv/ws")}, Limits: &ConfinementLimits{PIDs: -1}}},
		{"no-op", Confinement{WorkDirRoots: []string{confineTestPath("/srv/ws")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := NativeExecutorConfig{Confine: &test.confine}
			require.ErrorIs(t, cfg.Validate(), ErrInvalidConfinement)
		})
	}
}

func TestConfinementPatchRejectsMalformedValues(t *testing.T) {
	zero := int64(0)
	unknown := "loopback"
	for _, patch := range []*ConfinementPatch{
		{Limits: &ConfinementLimitsPatch{WallSec: &zero}},
		{Network: &unknown},
		{FS: &ConfinementFSPatch{Exec: pointerToStrings("relative")}},
	} {
		_, err := (ProcessOptions{Confine: patch}).Clone()
		require.ErrorIs(t, err, ErrInvalidConfinement)
	}
}

func TestConfinementRejectsDisallowedDefaultEnvironment(t *testing.T) {
	baseline := &Confinement{
		WorkDirRoots: []string{confineTestPath("/srv/ws")},
		Env: &ConfinementEnvironment{
			Allow: []string{"LANG"},
			Set:   map[string]string{"PATH": "/usr/bin"},
		},
	}
	for _, env := range []map[string]string{
		{"TOKEN": "secret"},
		{"PATH": "/attacker"},
	} {
		cfg := NativeExecutorConfig{Confine: baseline, DefaultEnv: env}
		require.ErrorIs(t, cfg.Validate(), ErrInvalidConfinement)
	}
	cfg := NativeExecutorConfig{Confine: baseline, DefaultEnv: map[string]string{"LANG": "C"}}
	require.NoError(t, cfg.Validate())
}

func pointerToStrings(values ...string) *[]string { return &values }

func confineTestPath(value string) string {
	if runtime.GOOS == "windows" {
		return `C:` + strings.ReplaceAll(value, "/", `\`)
	}
	return value
}
