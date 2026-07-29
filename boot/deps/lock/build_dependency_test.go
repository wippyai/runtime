// SPDX-License-Identifier: MPL-2.0

package lock

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiffReportsBuildRoleChanges(t *testing.T) {
	oldLock, err := New(filepath.Join(t.TempDir(), "old.lock"))
	require.NoError(t, err)
	oldLock.SetModule(Module{Name: "acme/shared", Version: "1.0.0", Hash: "sha256:shared", BuildOnly: true, BuildDependency: true})
	newLock, err := New(filepath.Join(t.TempDir(), "new.lock"))
	require.NoError(t, err)
	newLock.SetModule(Module{Name: "acme/shared", Version: "1.0.0", Hash: "sha256:shared"})

	changes := Diff(oldLock, newLock)
	require.Equal(t, []ModuleChange{
		{
			Name:               "acme/shared",
			OldVersion:         "1.0.0",
			NewVersion:         "1.0.0",
			OldHash:            "sha256:shared",
			NewHash:            "sha256:shared",
			OldBuildOnly:       true,
			OldBuildDependency: true,
		},
	}, changes.Updated)
}

func TestValidateRejectsRemoteBuildOnlyModuleWithoutDigest(t *testing.T) {
	lockObj, err := New(filepath.Join(t.TempDir(), DefaultFilename))
	require.NoError(t, err)
	lockObj.SetModule(Module{Name: "acme/frontend", Version: "1.0.0", BuildOnly: true})
	require.EqualError(t, Validate(lockObj), "build-only module acme/frontend requires an artifact digest")
}

func TestValidateRejectsMalformedBuildDigests(t *testing.T) {
	for _, digest := range []string{
		" ",
		"md5:" + strings.Repeat("a", 32),
		"sha256:abc",
		"sha256:" + strings.Repeat("z", 64),
	} {
		t.Run(digest, func(t *testing.T) {
			lockObj, err := New(filepath.Join(t.TempDir(), DefaultFilename))
			require.NoError(t, err)
			lockObj.SetModule(Module{Name: "acme/frontend", Version: "1.0.0", Hash: digest, BuildOnly: true})
			require.EqualError(t, Validate(lockObj), "build module acme/frontend has invalid artifact digest "+strconv.Quote(digest))
		})
	}
}

func TestValidateAllowsBuildOnlyReplacementWithoutDigest(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "local", "frontend"), 0o755))
	lockObj, err := New(filepath.Join(root, DefaultFilename))
	require.NoError(t, err)
	lockObj.SetModule(Module{Name: "acme/frontend", Version: "1.0.0", BuildOnly: true})
	lockObj.SetReplacement(Replacement{From: "acme/frontend", To: "local/frontend"})
	require.NoError(t, Validate(lockObj))
}

func TestValidateRejectsBuildOnlyDeploymentRoot(t *testing.T) {
	lockObj, err := New(filepath.Join(t.TempDir(), DefaultFilename))
	require.NoError(t, err)
	lockObj.SetModule(Module{Name: "acme/app", Version: "1.0.0", Root: true, BuildOnly: true})
	require.EqualError(t, Validate(lockObj), "deployment root acme/app cannot be build-only")
}

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
