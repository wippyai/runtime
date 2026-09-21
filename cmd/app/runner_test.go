// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/cmd/wippy/cmd"
)

// plannedHost answers every launch with one plan and records what it saw.
type plannedHost struct {
	plan   Plan
	err    error
	before func()

	observed Launch
	calls    int
}

func (h *plannedHost) Plan(_ context.Context, l Launch) (Plan, error) {
	h.calls++
	h.observed = l
	if h.before != nil {
		h.before()
	}
	return h.plan, h.err
}

// execution records the options an operation hands the Wippy CLI.
type execution struct {
	err     error
	options cmd.ExecuteOptions
	calls   int
}

func captureExecution(t *testing.T) *execution {
	t.Helper()
	record := &execution{}
	previous := execute
	execute = func(_ context.Context, options cmd.ExecuteOptions) error {
		record.calls++
		record.options = options
		return record.err
	}
	t.Cleanup(func() { execute = previous })
	return record
}

func runnableExecutable(t *testing.T) Executable {
	t.Helper()
	return Executable{Name: "runner-test", Command: "desktop", Bundle: testBundle(t)}
}

func TestPlannedRunLeavesStateUntouched(t *testing.T) {
	state := filepath.Join(t.TempDir(), "absent")
	captureExecution(t)
	ran := 0
	host := &plannedHost{plan: Plan{Run: func(context.Context) error { ran++; return nil }}}
	executable := runnableExecutable(t)
	executable.Host = host
	executable.Data = map[string]string{"WIPPY_APP_TEST_PLANNED_RUN": "data"}

	require.NoError(t, Run(t.Context(), executable, []string{"--state", state, "run", "terminal"}))
	require.Equal(t, 1, ran)
	require.Equal(t, 1, host.calls)
	require.Equal(t, state, host.observed.State)
	require.True(t, host.observed.Explicit)
	require.Equal(t, []string{"terminal"}, host.observed.Args)
	require.NoDirExists(t, state)
	_, bound := os.LookupEnv("WIPPY_APP_TEST_PLANNED_RUN")
	require.False(t, bound, "a planned run applied a data binding")
}

func TestPlanOverridesSelectStateCommandAndArguments(t *testing.T) {
	planned := filepath.Join(t.TempDir(), "planned")
	explicit := filepath.Join(t.TempDir(), "explicit")
	record := captureExecution(t)
	executable := runnableExecutable(t)
	executable.Host = &plannedHost{plan: Plan{DefaultState: planned, Command: "console", Args: []string{"replaced"}}}

	require.NoError(t, Run(t.Context(), executable, []string{"--state", explicit, "run", "original"}))
	require.Equal(t, 1, record.calls)
	require.Equal(t, []string{"run", "--silent", "--", "console", "replaced"}, record.options.Args)
	require.Equal(t, filepath.Join(explicit, deploymentsDir, executable.Bundle.ID(), "wippy.lock"), record.options.LockFile)
	require.Equal(t, historyPath(explicit), record.options.Overrides.GetString("registry.history_path", ""))
	require.Equal(t, cachePath(explicit), record.options.Overrides.GetString("registry.dependency_vendor_dir", ""))
	require.NoDirExists(t, planned)
}

func TestPlanDefaultStateSelectsStateWhenInvocationHasNoExplicitState(t *testing.T) {
	work := t.TempDir()
	other := t.TempDir()
	original, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(work))
	t.Cleanup(func() { _ = os.Chdir(original) })

	selected := filepath.Join(work, "selected")
	record := captureExecution(t)
	executable := runnableExecutable(t)
	executable.Host = &plannedHost{
		plan:   Plan{DefaultState: "selected"},
		before: func() { require.NoError(t, os.Chdir(other)) },
	}

	require.NoError(t, Run(t.Context(), executable, nil))
	require.Equal(t, filepath.Join(selected, deploymentsDir, executable.Bundle.ID(), "wippy.lock"), record.options.LockFile)
	require.DirExists(t, selected)
	require.NoDirExists(t, filepath.Join(other, "selected"))
}

func TestEmptyPlanDefaultStateKeepsExecutableState(t *testing.T) {
	called := 0
	executable := runnableExecutable(t)
	host := &plannedHost{
		plan: Plan{Run: func(context.Context) error {
			called++
			return nil
		}},
	}
	executable.Host = host

	require.NoError(t, Run(t.Context(), executable, nil))
	config, err := os.UserConfigDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(config, executable.Name), host.observed.State)
	require.Equal(t, 1, called)
}

func TestPlanDefaultStateErrorDoesNotOpenSelectedState(t *testing.T) {
	state := filepath.Join(t.TempDir(), "absent")
	planErr := errors.New("plan unavailable")
	executable := runnableExecutable(t)
	executable.Host = &plannedHost{err: planErr}

	err := Run(t.Context(), executable, []string{"--state", state, "run"})
	require.ErrorIs(t, err, planErr)
	require.NoDirExists(t, state)
}

func TestInvalidPlanDefaultStateRefusesBeforeOpeningState(t *testing.T) {
	work := t.TempDir()
	original, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(work))
	t.Cleanup(func() { _ = os.Chdir(original) })

	executable := runnableExecutable(t)
	executable.Host = &plannedHost{plan: Plan{DefaultState: "invalid\x00state"}}

	err = Run(t.Context(), executable, nil)
	require.ErrorContains(t, err, "state directory contains NUL")
	entries, readErr := os.ReadDir(work)
	require.NoError(t, readErr)
	require.Empty(t, entries)
}

func TestPreparedConfigCannotRedirectHistoryOrCache(t *testing.T) {
	state := t.TempDir()
	record := captureExecution(t)
	closes := 0
	executable := runnableExecutable(t)
	executable.Host = &plannedHost{plan: Plan{
		Prepare: func(context.Context) (boot.Config, func() error, error) {
			// Preparation owns the state, so the lock is already held.
			_, err := lockState(state)
			require.ErrorIs(t, err, ErrOwned)
			config := boot.NewConfig(
				boot.WithSection("registry", map[string]any{
					"enable_history":        false,
					"history_type":          "other",
					"history_path":          "elsewhere",
					"dependency_vendor_dir": "elsewhere",
				}),
				boot.WithSection("cluster", map[string]any{"enabled": true}),
			)
			return config, func() error { closes++; return nil }, nil
		},
	}}

	require.NoError(t, Run(t.Context(), executable, []string{"--state", state, "run"}))
	require.Equal(t, 1, closes)
	overrides := record.options.Overrides
	require.Equal(t, historyPath(state), overrides.GetString("registry.history_path", ""))
	require.Equal(t, "sqlite", overrides.GetString("registry.history_type", ""))
	require.True(t, overrides.GetBool("registry.enable_history", false))
	require.Equal(t, cachePath(state), overrides.GetString("registry.dependency_vendor_dir", ""))
	require.True(t, overrides.GetBool("cluster.enabled", false))
}

func TestPreparedResourcesCloseUnderTheLock(t *testing.T) {
	for _, failing := range []string{"preparation", "startup"} {
		t.Run(failing, func(t *testing.T) {
			state := t.TempDir()
			record := captureExecution(t)
			failure, cleanup := errors.New("stage failed"), errors.New("cleanup failed")
			closes := 0
			executable := runnableExecutable(t)
			executable.Host = &plannedHost{plan: Plan{
				Prepare: func(context.Context) (boot.Config, func() error, error) {
					release := func() error {
						closes++
						_, err := lockState(state)
						require.ErrorIs(t, err, ErrOwned)
						return cleanup
					}
					if failing == "preparation" {
						return nil, release, failure
					}
					return nil, release, nil
				},
			}}
			if failing == "startup" {
				record.err = failure
			}

			err := Run(t.Context(), executable, []string{"--state", state, "run"})
			require.ErrorIs(t, err, failure)
			require.ErrorIs(t, err, cleanup)
			require.Equal(t, 1, closes)
			unlock, err := lockState(state)
			require.NoError(t, err)
			require.NoError(t, unlock())
		})
	}
}

func TestOwnedStateRejectsTheLockedOperations(t *testing.T) {
	for _, args := range [][]string{{"run"}, {"update"}, {"recover"}, {"wippy", "install"}} {
		t.Run(args[0]+"/"+args[len(args)-1], func(t *testing.T) {
			state := t.TempDir()
			captureExecution(t)
			unlock, err := lockState(state)
			require.NoError(t, err)
			defer func() { _ = unlock() }()

			err = Run(t.Context(), runnableExecutable(t), append([]string{"--state", state}, args...))
			require.ErrorIs(t, err, ErrOwned)
		})
	}
}

func TestReadOnlyWippyCommandRunsWhileTheStateIsOwned(t *testing.T) {
	state := t.TempDir()
	record := captureExecution(t)
	executable := runnableExecutable(t)
	_, err := executable.Bundle.Seed(filepath.Join(deploymentsPath(state), executable.Bundle.ID()))
	require.NoError(t, err)
	unlock, err := lockState(state)
	require.NoError(t, err)
	defer func() { _ = unlock() }()

	require.NoError(t, Run(t.Context(), executable, []string{"--state", state, "wippy", "lint", "--json"}))
	require.Equal(t, []string{"lint", "--json"}, record.options.Args)
	require.Equal(t, filepath.Join(deploymentsPath(state), executable.Bundle.ID(), "wippy.lock"), record.options.LockFile)
}

func TestDataEnvironmentBindsInsideStateAndKeepsUserValues(t *testing.T) {
	const bound = "WIPPY_APP_TEST_BOUND"
	const overridden = "WIPPY_APP_TEST_OVERRIDDEN"
	state := t.TempDir()
	captureExecution(t)
	t.Setenv(overridden, "")
	require.NoError(t, os.Unsetenv(bound))
	t.Cleanup(func() { _ = os.Unsetenv(bound) })
	executable := runnableExecutable(t)
	executable.Data = map[string]string{bound: filepath.Join("data", "app.db"), overridden: "data"}

	require.NoError(t, Run(t.Context(), executable, []string{"--state", state, "run"}))
	require.Equal(t, filepath.Join(state, "data", "app.db"), os.Getenv(bound))
	value, found := os.LookupEnv(overridden)
	require.True(t, found)
	require.Equal(t, "", value, "an explicit user value stays as the user set it")
}

func TestCanceledInvocationDoesNotCreateState(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	state := filepath.Join(t.TempDir(), "absent")
	captureExecution(t)

	err := Run(ctx, runnableExecutable(t), []string{"--state", state, "run"})
	require.ErrorIs(t, err, context.Canceled)
	require.NoDirExists(t, state)
}

func TestConfigurationFileOfTheStateReachesTheRuntime(t *testing.T) {
	state := t.TempDir()
	record := captureExecution(t)
	path := filepath.Join(state, configFilename)
	require.NoError(t, os.WriteFile(path, []byte("version: \"1.0\"\n"), 0o600))

	require.NoError(t, Run(t.Context(), runnableExecutable(t), []string{"--state", state, "run"}))
	require.Equal(t, []string{path}, record.options.ConfigFiles)
}
