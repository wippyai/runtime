// SPDX-License-Identifier: MPL-2.0

package app

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/wapp"
)

func testBundleVersion(t *testing.T, version string) Bundle {
	t.Helper()
	var data bytes.Buffer
	err := wapp.NewWriter().PackEntries(wapp.Metadata{"namespace": "acme.app", "name": "app", "version": version}, nil, &data)
	require.NoError(t, err)
	digest := sha256.Sum256(data.Bytes())
	return Bundle{Root: "acme/app", Packs: []Pack{{Module: "acme/app", Version: version, Digest: fmt.Sprintf("sha256:%x", digest), Data: data.Bytes()}}}
}

func TestSelectLaunchDeploymentKeepsEmbeddedBaselineHistoryInState(t *testing.T) {
	state := t.TempDir()
	bundle := testBundleVersion(t, "1.0.0")
	ordinary, history, err := selectLaunchDeployment(state, bundle, false, false, "base")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(state, "deployment"), ordinary)
	require.Equal(t, filepath.Join(state, "registry.db"), history)

	embedded, history, err := selectLaunchDeployment(state, bundle, true, false, "base")
	require.NoError(t, err)
	require.Equal(t, embeddedDeployment(state, bundle), embedded)
	require.Equal(t, filepath.Join(state, "registry.db"), history)

	recovery, history, err := selectLaunchDeployment(state, bundle, false, true, "base")
	require.NoError(t, err)
	require.Equal(t, embedded, recovery)
	require.Equal(t, filepath.Join(recovery, "registry.db"), history)
}

func TestEmbeddedBaselineVersionChangeUsesNewDigestAndSharedHistory(t *testing.T) {
	state := t.TempDir()
	first := testBundleVersion(t, "1.0.0")
	second := testBundleVersion(t, "2.0.0")
	oldDeployment := filepath.Join(state, "deployment")
	_, err := first.Seed(oldDeployment)
	require.NoError(t, err)
	before, err := os.ReadFile(filepath.Join(oldDeployment, "wippy.lock"))
	require.NoError(t, err)

	deployment, history, err := selectLaunchDeployment(state, second, true, false, "base")
	require.NoError(t, err)
	require.NotEqual(t, oldDeployment, deployment)
	require.Equal(t, filepath.Join(state, "registry.db"), history)
	_, err = second.Seed(deployment)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(deployment, "wippy.lock"))
	require.FileExists(t, filepath.Join(oldDeployment, "wippy.lock"))
	after, err := os.ReadFile(filepath.Join(oldDeployment, "wippy.lock"))
	require.NoError(t, err)
	require.Equal(t, before, after)
	selected, err := selectedDeployment(state)
	require.NoError(t, err)
	require.Equal(t, oldDeployment, selected)
}

func TestEmbeddedBaselineFailureLeavesPriorDeploymentRecoverable(t *testing.T) {
	state := t.TempDir()
	first := testBundleVersion(t, "1.0.0")
	oldDeployment := filepath.Join(state, "deployment")
	oldLock, err := first.Seed(oldDeployment)
	require.NoError(t, err)
	before, err := os.ReadFile(oldLock)
	require.NoError(t, err)

	second := testBundleVersion(t, "2.0.0")
	second.Packs[0].Digest = "sha256:invalid"
	deployment, history, err := selectLaunchDeployment(state, second, true, false, "base")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(state, "registry.db"), history)
	_, err = second.Seed(deployment)
	require.Error(t, err)
	after, err := os.ReadFile(oldLock)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.NoDirExists(t, deployment)
	selected, err := selectedDeployment(state)
	require.NoError(t, err)
	require.Equal(t, oldDeployment, selected)
}

func TestEmbeddedBaselineSelectionIgnoresStaleActivation(t *testing.T) {
	state := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(state, "active.json"), []byte("invalid"), 0o600))
	deployment, history, err := selectLaunchDeployment(state, testBundleVersion(t, "1.0.0"), true, false, "base")
	require.NoError(t, err)
	require.NotEmpty(t, deployment)
	require.Equal(t, filepath.Join(state, "registry.db"), history)
}

func TestBaseSelectionRejectsBootstrapMode(t *testing.T) {
	_, _, err := selectLaunchDeployment(t.TempDir(), testBundleVersion(t, "1.0.0"), false, true, "bootstrap")
	require.ErrorContains(t, err, "bootstrap applications do not expose a base deployment")
}
