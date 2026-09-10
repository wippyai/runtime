// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFailedUpdateNeverChangesActiveDeployment(t *testing.T) {
	for _, failing := range []string{"update", "lint"} {
		t.Run(failing, func(t *testing.T) {
			state := t.TempDir()
			bundle := testBundle(t)
			current := filepath.Join(state, "deployment")
			path, err := bundle.Seed(current)
			require.NoError(t, err)
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			run := func(_ context.Context, candidate string, args ...string) error {
				require.NotEqual(t, state, candidate)
				if args[0] == failing {
					require.NoError(t, os.WriteFile(filepath.Join(candidate, "deployment", "wippy.lock"), []byte("broken lock"), 0600))
					return errors.New("injected failure")
				}
				return nil
			}
			err = updateDeployment(context.Background(), Options{Bundle: bundle}, state, current, nil, run)
			require.Error(t, err)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after)
			selected, err := selectedDeployment(state)
			require.NoError(t, err)
			require.Equal(t, current, selected)
		})
	}
}

func TestSuccessfulUpdateSelectsVerifiedCandidate(t *testing.T) {
	state := t.TempDir()
	bundle := testBundle(t)
	current := filepath.Join(state, "deployment")
	_, err := bundle.Seed(current)
	require.NoError(t, err)
	var commands []string
	run := func(_ context.Context, _ string, args ...string) error {
		commands = append(commands, args[0])
		return nil
	}
	require.NoError(t, updateDeployment(context.Background(), Options{Bundle: bundle}, state, current, nil, run))
	require.Equal(t, []string{"update", "lint"}, commands)
	selected, err := selectedDeployment(state)
	require.NoError(t, err)
	require.NotEqual(t, current, selected)
	_, err = bundle.existing(filepath.Join(selected, "wippy.lock"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(current, "wippy.lock"))
	require.NoError(t, err)
}

func TestApplicationLockIsReleasedAndRejectsConcurrentRun(t *testing.T) {
	state := t.TempDir()
	unlock, err := lockApplication(state)
	require.NoError(t, err)
	_, err = lockApplication(state)
	require.Error(t, err)
	unlock()
	unlock, err = lockApplication(state)
	require.NoError(t, err)
	unlock()
}

func TestActivationCannotEscapeStateDirectory(t *testing.T) {
	state := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(state, "active.json"), []byte(`{"directory":"../outside"}`), 0600))
	_, err := selectedDeployment(state)
	require.Error(t, err)
}
