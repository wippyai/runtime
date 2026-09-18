// SPDX-License-Identifier: MPL-2.0

package app

import (
	"bytes"
	"context"
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

func activatedDeployment(state string, _ Bundle) string {
	return filepath.Join(state, "deployment")
}

// seedBarrier leaves both candidate deployments present but unreadable, so a
// run reports the deployment it selected instead of starting the runtime.
func seedBarrier(t *testing.T, state string, bundle Bundle) {
	t.Helper()
	require.NoError(t, os.MkdirAll(activatedDeployment(state, bundle), 0o700))
	require.NoError(t, os.MkdirAll(embeddedDeployment(state, bundle), 0o700))
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

func TestEmbeddedBaselineSeedLeavesActivatedDeploymentIntact(t *testing.T) {
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
	require.Equal(t, embeddedDeployment(state, second), deployment)
	require.NotEqual(t, embeddedDeployment(state, first), deployment)
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

func TestEmbeddedBaselineSeedFailureLeavesActivatedDeploymentRecoverable(t *testing.T) {
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
	require.Equal(t, embeddedDeployment(state, second), deployment)
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
	bundle := testBundleVersion(t, "1.0.0")
	require.NoError(t, os.WriteFile(filepath.Join(state, "active.json"), []byte("invalid"), 0o600))
	deployment, history, err := selectLaunchDeployment(state, bundle, true, false, "base")
	require.NoError(t, err)
	require.Equal(t, embeddedDeployment(state, bundle), deployment)
	require.Equal(t, filepath.Join(state, "registry.db"), history)
}

func TestBaseSelectionRejectsBootstrapMode(t *testing.T) {
	_, _, err := selectLaunchDeployment(t.TempDir(), testBundleVersion(t, "1.0.0"), false, true, "bootstrap")
	require.ErrorContains(t, err, "bootstrap applications do not expose a base deployment")
}

func TestRunSelectsDeploymentForBaseline(t *testing.T) {
	for _, selection := range []struct {
		name      string
		baseline  string
		selected  func(string, Bundle) string
		arguments []string
	}{
		{name: "embedded", baseline: baselineEmbedded, selected: embeddedDeployment},
		{name: "activated", baseline: baselineActivated, selected: activatedDeployment},
		{name: "default", baseline: "", selected: activatedDeployment},
		{name: "embedded_runtime_command", baseline: baselineEmbedded, arguments: []string{"runtime", "lint"}, selected: activatedDeployment},
		{name: "embedded_base_recovery", baseline: baselineEmbedded, arguments: []string{"--base"}, selected: embeddedDeployment},
	} {
		for _, client := range []bool{false, true} {
			t.Run(selection.name+map[bool]string{false: "", true: "_through_launch"}[client], func(t *testing.T) {
				state := t.TempDir()
				bundle := testBundleVersion(t, "1.0.0")
				seedBarrier(t, state, bundle)
				options := Options{Name: "baseline-test", Command: "desktop", Mode: "base",
					Bundle: bundle, Baseline: selection.baseline}
				if client {
					options.Launch = func(_ context.Context, _ LaunchRequest, run func(OwnerOptions) error) error {
						return run(OwnerOptions{})
					}
				}
				err := Run(t.Context(), options, append([]string{"--state-dir", state}, selection.arguments...))
				require.ErrorContains(t, err, filepath.Join(selection.selected(state, bundle), "wippy.lock"))
			})
		}
	}
}

func TestRunRefusesUnknownBaselineBeforeLaunch(t *testing.T) {
	state := filepath.Join(t.TempDir(), "absent")
	called := false
	options := Options{Name: "baseline-test", Command: "desktop", Mode: "base", Baseline: "recovery",
		Bundle: testBundleVersion(t, "1.0.0"),
		Launch: func(context.Context, LaunchRequest, func(OwnerOptions) error) error {
			called = true
			return nil
		}}
	err := Run(t.Context(), options, []string{"--state-dir", state})
	require.ErrorContains(t, err, "application baseline must be activated or embedded")
	require.False(t, called)
	require.NoDirExists(t, state)
}
