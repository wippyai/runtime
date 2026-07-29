// SPDX-License-Identifier: MPL-2.0

package lock

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLockSeparatesRuntimeAndArtifactModulePaths(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), DefaultFilename)
	lockObj, err := New(lockPath)
	require.NoError(t, err)
	lockObj.SetDirectories(Directories{Modules: ".wippy", Src: "app"})
	lockObj.SetModule(Module{Name: "acme/runtime", Version: "1.0.0"})
	lockObj.SetModule(Module{Name: "acme/frontend", Version: "1.0.0", BuildOnly: true})
	require.NoError(t, lockObj.Write())

	reloaded, err := New(lockPath)
	require.NoError(t, err)

	runtimePaths := reloaded.GetModuleLoadPaths()
	require.Len(t, runtimePaths, 2)
	require.Equal(t, "acme/runtime", runtimePaths[1].Module)

	artifactPaths := reloaded.GetArtifactModuleLoadPaths()
	require.Len(t, artifactPaths, 3)
	require.Equal(t, "acme/frontend", artifactPaths[1].Module)
	require.Equal(t, "acme/runtime", artifactPaths[2].Module)

	frontend, ok := reloaded.GetModule("acme/frontend")
	require.True(t, ok)
	require.True(t, frontend.BuildOnly)
}
