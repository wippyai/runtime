// SPDX-License-Identifier: MPL-2.0

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecutableStateResolvesAgainstTheWorkingDirectory(t *testing.T) {
	executable := testExecutable()
	executable.State = ".app"
	working, err := os.Getwd()
	require.NoError(t, err)

	launch, err := parseLaunch(executable, []string{"run"})
	require.NoError(t, err)
	require.False(t, launch.Explicit)
	require.Equal(t, filepath.Join(working, ".app"), launch.State)

	explicit, err := parseLaunch(executable, []string{"--state", "elsewhere", "run"})
	require.NoError(t, err)
	require.True(t, explicit.Explicit)
	require.Equal(t, filepath.Join(working, "elsewhere"), explicit.State)
}

func TestAbsoluteExecutableStateIsUsedAsDeclared(t *testing.T) {
	target := t.TempDir()
	executable := testExecutable()
	executable.State = target

	launch, err := parseLaunch(executable, nil)
	require.NoError(t, err)
	require.Equal(t, target, launch.State)
}

func TestExecutableStateWithNULIsRejectedBeforeAnyFilesystemEffect(t *testing.T) {
	executable := testExecutable()
	executable.State = "bad\x00state"
	require.Error(t, executable.validate())
}

func TestPlanDefaultStateStillReplacesExecutableState(t *testing.T) {
	record := captureExecution(t)
	planned := filepath.Join(t.TempDir(), "planned")
	executable := runnableExecutable(t)
	executable.State = filepath.Join(t.TempDir(), "declared")
	executable.Host = &plannedHost{plan: Plan{DefaultState: planned}}

	require.NoError(t, Run(t.Context(), executable, []string{"run"}))
	require.Equal(t, 1, record.calls)
	require.Equal(t, historyPath(planned), record.options.Overrides.GetString("registry.history_path", ""))
	require.NoDirExists(t, executable.State)
}

func TestOwnedCommandRunsTransientlyWhileTheStateIsOwned(t *testing.T) {
	target := t.TempDir()
	unlock, err := lockState(target)
	require.NoError(t, err)
	defer func() { require.NoError(t, unlock()) }()
	before, err := os.ReadDir(target)
	require.NoError(t, err)

	record := captureExecution(t)
	executable := runnableExecutable(t)
	executable.OwnedCommand = "client"

	require.NoError(t, Run(t.Context(), executable, []string{"--state", target, "run", "node-a"}))
	require.Equal(t, 1, record.calls)
	require.Equal(t, []string{"run", "--silent", "--", "client", "node-a"}, record.options.Args)
	require.NotEqual(t, historyPath(target), record.options.Overrides.GetString("registry.history_path", ""))
	after, err := os.ReadDir(target)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestOwnedCommandLeavesAFreeStateToTheApplicationCommand(t *testing.T) {
	target := filepath.Join(t.TempDir(), "free")
	record := captureExecution(t)
	executable := runnableExecutable(t)
	executable.OwnedCommand = "client"

	require.NoError(t, Run(t.Context(), executable, []string{"--state", target, "run"}))
	require.Equal(t, 1, record.calls)
	require.Equal(t, []string{"run", "--silent", "--", "desktop"}, record.options.Args)
	require.Equal(t, historyPath(target), record.options.Overrides.GetString("registry.history_path", ""))
}

func TestOwnedCommandDoesNotRedirectStateOperations(t *testing.T) {
	target := t.TempDir()
	unlock, err := lockState(target)
	require.NoError(t, err)
	defer func() { require.NoError(t, unlock()) }()

	record := captureExecution(t)
	executable := runnableExecutable(t)
	executable.OwnedCommand = "client"

	err = Run(t.Context(), executable, []string{"--state", target, "recover"})
	require.ErrorIs(t, err, ErrOwned)
	require.Zero(t, record.calls)
}
