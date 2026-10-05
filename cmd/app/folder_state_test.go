// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	apierror "github.com/wippyai/runtime/api/error"
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
	for _, field := range []string{"state", "owned command"} {
		t.Run(field, func(t *testing.T) {
			executable := testExecutable()
			if field == "state" {
				executable.State = "bad\x00state"
			} else {
				executable.OwnedCommand = "bad\x00command"
			}
			target := filepath.Join(t.TempDir(), "absent")
			err := Run(t.Context(), executable, []string{"--state", target, "run"})
			var reported apierror.Error
			require.ErrorAs(t, err, &reported)
			require.Equal(t, apierror.Invalid, reported.Kind())
			require.Equal(t, apierror.False, reported.Retryable())
			require.NoDirExists(t, target)
		})
	}
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

	for _, args := range [][]string{{"update"}, {"recover"}, {"wippy", "lint"}} {
		err = Run(t.Context(), executable, append([]string{"--state", target}, args...))
		require.ErrorIs(t, err, ErrOwned)
		require.Zero(t, record.calls)
	}
}

func TestOwnedCommandUsesActualLockResultAfterAFreeProbe(t *testing.T) {
	target := t.TempDir()
	owned, err := Owned(target)
	require.NoError(t, err)
	require.False(t, owned)

	// Another invocation wins ownership after a free probe. Exercise the
	// actual acquisition path, not another probe of an already owned state.
	unlock, err := lockState(target)
	require.NoError(t, err)
	defer func() { require.NoError(t, unlock()) }()
	record := captureExecution(t)
	executable := runnableExecutable(t)
	executable.OwnedCommand = "client"

	err = operate(t.Context(), executable, Launch{
		State: target, Command: executable.Command, Op: OpRun, Args: []string{"node-a"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, record.calls)
	require.Equal(t, []string{"run", "--silent", "--", "client", "node-a"}, record.options.Args)
	require.NotEqual(t, historyPath(target), record.options.Overrides.GetString("registry.history_path", ""))
}

func TestOwnedCommandDoesNotRedirectErrorsAfterTakingOwnership(t *testing.T) {
	for _, stage := range []string{"prepare", "execute"} {
		t.Run(stage, func(t *testing.T) {
			target := t.TempDir()
			record := captureExecution(t)
			executable := runnableExecutable(t)
			executable.OwnedCommand = "client"
			prepared := 0
			executable.Host = &plannedHost{plan: Plan{
				Prepare: func(_ context.Context) (boot.Config, func() error, error) {
					prepared++
					if stage == "prepare" {
						return nil, nil, ErrOwned
					}
					return nil, nil, nil
				},
			}}
			if stage == "execute" {
				record.err = ErrOwned
			}

			err := Run(t.Context(), executable, []string{"--state", target, "run"})
			require.ErrorIs(t, err, ErrOwned)
			require.Equal(t, 1, prepared)
			if stage == "prepare" {
				require.Zero(t, record.calls)
			} else {
				require.Equal(t, 1, record.calls)
				require.Equal(t, []string{"run", "--silent", "--", "desktop"}, record.options.Args)
			}
		})
	}
}

func TestOwnedCommandDoesNotRedirectFilesystemErrors(t *testing.T) {
	target := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(target, lockFilename), 0o700))
	record := captureExecution(t)
	executable := runnableExecutable(t)
	executable.OwnedCommand = "client"

	err := Run(t.Context(), executable, []string{"--state", target, "run"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrOwned)
	require.Zero(t, record.calls)
}

func TestOwnedCommandKeepsConfigurationAndDataPrivate(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			target := t.TempDir()
			config := []byte("version: '1.0'\nvars:\n  owner_only: private\n")
			configPath := filepath.Join(target, configFilename)
			require.NoError(t, os.WriteFile(configPath, config, 0o600))
			unlock, err := lockState(target)
			require.NoError(t, err)
			defer func() { require.NoError(t, unlock()) }()

			const binding = "WIPPY_APP_OWNED_CLIENT_DATA"
			unsetEnvironment(t, binding)
			record := captureExecution(t)
			executable := runnableExecutable(t)
			executable.OwnedCommand = "client"
			executable.Data = map[string]string{binding: "data"}
			prepared := 0
			transient := ""
			executable.Host = &plannedHost{plan: Plan{
				Prepare: func(_ context.Context) (boot.Config, func() error, error) {
					prepared++
					transient = filepath.Dir(os.Getenv(binding))
					require.NotEqual(t, target, transient)
					return boot.NewConfig(boot.WithSection("vars", map[string]any{"client": true})), nil, nil
				},
			}}
			failure := errors.New("client failed")
			if fail {
				record.err = failure
			}

			err = Run(t.Context(), executable, []string{"--state", target, "run"})
			if fail {
				require.ErrorIs(t, err, failure)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, prepared)
			require.Equal(t, 1, record.calls)
			require.Empty(t, record.options.ConfigFiles, "owner configuration must not enter the client state")
			require.True(t, record.options.Overrides.GetBool("vars.client", false))
			require.Equal(t, historyPath(transient), record.options.Overrides.GetString("registry.history_path", ""))
			require.NoDirExists(t, transient)
			_, present := os.LookupEnv(binding)
			require.False(t, present)
			after, err := os.ReadFile(configPath)
			require.NoError(t, err)
			require.Equal(t, config, after)
		})
	}
}
