// SPDX-License-Identifier: MPL-2.0

package app

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/boot/deps/lock"
)

// retainRow adds a module row to a retained deployment lock without touching the
// artifacts it points at, reproducing a lock a previous runtime left behind.
func retainRow(t *testing.T, deployment string, module lock.Module) {
	t.Helper()
	locked, err := lock.New(filepath.Join(deployment, lock.DefaultFilename))
	require.NoError(t, err)
	locked.SetModule(module)
	require.NoError(t, locked.Write())
}

func seededDeployment(t *testing.T) (string, string, Bundle) {
	t.Helper()
	state := t.TempDir()
	bundle := bundleWithMarker(t, "stable")
	deployment := embeddedDeployment(state, bundle)
	_, err := bundle.Seed(deployment)
	require.NoError(t, err)
	return state, deployment, bundle
}

func TestSeedDependencyCacheReportsUnparsableRetainedModuleName(t *testing.T) {
	state, deployment, bundle := seededDeployment(t)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("retained")))
	retainRow(t, deployment, lock.Module{Name: "wippy/agent/extra", Version: "0.1.0-dev", Hash: digest})

	err := seedDependencyCache(state, deployment, bundle)
	require.Error(t, err)
	require.ErrorContains(t, err, "wippy/agent/extra")
}

func TestSeedDependencyCacheReportsMalformedRetainedModuleDigest(t *testing.T) {
	state, deployment, bundle := seededDeployment(t)
	retainRow(t, deployment, lock.Module{Name: "wippy/agent", Version: "0.1.0-dev", Hash: "sha256:not-a-digest"})

	err := seedDependencyCache(state, deployment, bundle)
	require.Error(t, err)
	require.ErrorContains(t, err, "wippy/agent")
}

// A lock row without a hash carries no immutable identity, so there is nothing
// to address it by in a content-addressed cache. It is not corruption.
func TestSeedDependencyCacheAcceptsRetainedModuleWithoutDigest(t *testing.T) {
	state, deployment, bundle := seededDeployment(t)
	retainRow(t, deployment, lock.Module{Name: "wippy/agent", Version: "0.1.0-dev"})

	require.NoError(t, seedDependencyCache(state, deployment, bundle))
}

func TestSeedDependencyCacheReportsCorruptActivation(t *testing.T) {
	state, deployment, bundle := seededDeployment(t)
	require.NoError(t, os.WriteFile(filepath.Join(state, "active.json"), []byte("{"), 0o600))

	err := seedDependencyCache(state, deployment, bundle)
	require.Error(t, err)
	require.ErrorContains(t, err, "activation")
}

// immutableRelative names a cache entry through the same contract production
// code publishes it under.
func immutableRelative(t *testing.T, module, version, digest string) string {
	t.Helper()
	_, relative, err := immutableArtifactPath(module, version, digest)
	require.NoError(t, err)
	return relative
}
