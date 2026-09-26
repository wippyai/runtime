// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"encoding/json"
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
}
