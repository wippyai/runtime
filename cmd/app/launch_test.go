// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
)

func launchOptions(callback Launch) Options {
	return Options{Name: "launch-test", Command: "desktop", Mode: "base", Launch: callback}
}

func requireSameConfig(t *testing.T, expected, actual boot.Config) {
	t.Helper()
	require.ElementsMatch(t, expected.Keys(), actual.Keys())
	for _, key := range expected.Keys() {
		want, found := expected.Get(key)
		require.True(t, found, key)
		got, present := actual.Get(key)
		require.True(t, present, key)
		require.Equal(t, want, got, key)
	}
}

func TestClientLaunchDoesNotOpenOwnerState(t *testing.T) {
	const binding = "WIPPY_APP_TEST_CLIENT_BINDING"
	state := filepath.Join(t.TempDir(), "absent")
	calls := 0
	options := launchOptions(func(_ context.Context, request LaunchRequest, _ func(OwnerOptions) error) error {
		calls++
		require.Equal(t, state, request.StateDir)
		require.Equal(t, []string{"terminal", "two words"}, request.Arguments)
		return nil
	})
	options.DataEnv = map[string]string{binding: "data"}
	require.NoError(t, Run(t.Context(), options, []string{"--state-dir", state, "run", "terminal", "two words"}))
	require.Equal(t, 1, calls)
	require.NoDirExists(t, state)
	_, bound := os.LookupEnv(binding)
	require.False(t, bound, "client launch applied an owner data binding")

	options.DataEnv = map[string]string{"INVALID NAME": "../outside"}
	require.ErrorContains(t, Run(t.Context(), options, []string{"--state-dir", state, "run"}),
		"invalid application data environment binding")
	require.Equal(t, 1, calls, "invalid data environment reached the launch callback")
	require.NoDirExists(t, state)
}

func TestExecutableSelectsDefaultStateBeforeLaunch(t *testing.T) {
	state := filepath.Join(t.TempDir(), "selected")
	options := launchOptions(func(_ context.Context, request LaunchRequest, _ func(OwnerOptions) error) error {
		require.Equal(t, state, request.StateDir)
		return nil
	})
	options.DefaultStateDir = func() (string, error) { return state, nil }
	require.NoError(t, Run(t.Context(), options, nil))
	require.NoDirExists(t, state)
}

func TestExplicitStateBypassesExecutableDefault(t *testing.T) {
	state := filepath.Join(t.TempDir(), "explicit")
	called := false
	options := launchOptions(func(_ context.Context, request LaunchRequest, _ func(OwnerOptions) error) error {
		require.Equal(t, state, request.StateDir)
		return nil
	})
	options.DefaultStateDir = func() (string, error) {
		called = true
		return "", errors.New("must not run")
	}
	require.NoError(t, Run(t.Context(), options, []string{"--state-dir", state}))
	require.False(t, called)
}

func TestInvalidExecutableDefaultRefusesBeforeLaunch(t *testing.T) {
	unavailable := errors.New("unavailable")
	for _, test := range []struct {
		resolve func() (string, error)
		cause   error
		name    string
		kind    apierror.Kind
	}{
		{name: "empty", resolve: func() (string, error) { return "", nil }, kind: apierror.Invalid},
		{name: "failure", resolve: func() (string, error) { return "", unavailable }, kind: apierror.Internal, cause: unavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := launchOptions(func(context.Context, LaunchRequest, func(OwnerOptions) error) error {
				t.Fatal("launch called with invalid default state")
				return nil
			})
			options.DefaultStateDir = test.resolve
			err := Run(t.Context(), options, nil)
			var rich apierror.Error
			require.ErrorAs(t, err, &rich)
			require.Equal(t, test.kind, rich.Kind())
			if test.cause != nil {
				require.ErrorIs(t, err, test.cause)
			}
		})
	}
}

func TestBusyOwnerCanSelectClientWithoutOpeningStores(t *testing.T) {
	state := t.TempDir()
	unlock, err := lockApplication(state)
	require.NoError(t, err)
	defer unlock()
	require.NoError(t, os.WriteFile(filepath.Join(state, "active.json"), []byte("invalid"), 0o600))
	called, prepared := false, false
	options := launchOptions(func(_ context.Context, _ LaunchRequest, run func(OwnerOptions) error) error {
		err := run(OwnerOptions{Prepare: func(context.Context) (OwnerResources, error) {
			prepared = true
			return OwnerResources{}, nil
		}})
		require.ErrorIs(t, err, ErrBusy)
		called = true
		return nil
	})
	options.DataEnv = map[string]string{"INVALID NAME": "../outside"}
	require.ErrorContains(t, Run(t.Context(), options, []string{"--state-dir", state}),
		"invalid application data environment binding")
	require.False(t, called, "invalid data environment reached the launch callback")

	options.DataEnv = nil
	require.NoError(t, Run(t.Context(), options, []string{"--state-dir", state}))
	require.True(t, called)
	require.False(t, prepared)
	require.NoDirExists(t, filepath.Join(state, "deployment"))
}

func TestLockIOErrorIsNotBusy(t *testing.T) {
	state := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(state, ".application.lock"), 0o700))
	options := launchOptions(func(_ context.Context, _ LaunchRequest, run func(OwnerOptions) error) error {
		err := run(OwnerOptions{})
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrBusy)
		return err
	})
	require.Error(t, Run(t.Context(), options, []string{"--state-dir", state}))
}

func TestOwnerPreparationClosesUnderLockOnFailure(t *testing.T) {
	for _, failPrepare := range []bool{false, true} {
		t.Run(map[bool]string{false: "seed_failure", true: "preparation_failure"}[failPrepare], func(t *testing.T) {
			state := t.TempDir()
			failure, cleanup := errors.New("prepare failed"), errors.New("cleanup failed")
			closes := 0
			options := launchOptions(func(_ context.Context, _ LaunchRequest, run func(OwnerOptions) error) error {
				return run(OwnerOptions{Prepare: func(context.Context) (OwnerResources, error) {
					resources := OwnerResources{Close: func() error {
						closes++
						_, err := lockApplication(state)
						require.ErrorIs(t, err, errLockBusy)
						return cleanup
					}}
					if failPrepare {
						return resources, failure
					}
					return resources, nil
				}})
			})
			err := Run(t.Context(), options, []string{"--state-dir", state})
			require.ErrorIs(t, err, cleanup)
			if failPrepare {
				require.ErrorIs(t, err, failure)
			}
			require.Equal(t, 1, closes)
			unlock, err := lockApplication(state)
			require.NoError(t, err)
			unlock()
		})
	}
}

func TestLaunchBypassedForReservedOperations(t *testing.T) {
	for _, args := range [][]string{{"runtime", "lint"}, {"update"}, {"--base"}} {
		options := launchOptions(func(context.Context, LaunchRequest, func(OwnerOptions) error) error {
			t.Fatal("reserved operation invoked application launch")
			return nil
		})
		err := Run(t.Context(), options, append([]string{"--state-dir", t.TempDir()}, args...))
		require.Error(t, err, "empty bundle must reject the standard owner path")
	}
}

func TestLaunchOwnerRunnerSingleUseAndScoped(t *testing.T) {
	var escaped func(OwnerOptions) error
	var calls int
	err := launch(t.Context(), func(_ context.Context, _ LaunchRequest, run func(OwnerOptions) error) error {
		escaped = run
		require.NoError(t, run(OwnerOptions{}))
		require.ErrorContains(t, run(OwnerOptions{}), "single-use")
		return nil
	}, LaunchRequest{}, func(context.Context, OwnerOptions) error { calls++; return nil })
	require.NoError(t, err)
	require.ErrorContains(t, escaped(OwnerOptions{}), "single-use")
	require.Equal(t, 1, calls)
}

func TestLaunchReturnCancelsAndJoinsOwner(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	var goroutine sync.WaitGroup
	err := launch(t.Context(), func(_ context.Context, _ LaunchRequest, run func(OwnerOptions) error) error {
		goroutine.Go(func() { _ = run(OwnerOptions{}) })
		<-started
		return nil
	}, LaunchRequest{}, func(ctx context.Context, _ OwnerOptions) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})
	require.ErrorContains(t, err, "before owner runner completed")
	select {
	case <-stopped:
	default:
		t.Fatal("launch returned before owner cleanup")
	}
	goroutine.Wait()
}

func TestCanceledPreparationDoesNotOpenDeployment(t *testing.T) {
	state := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closes := 0
	options := launchOptions(func(_ context.Context, _ LaunchRequest, run func(OwnerOptions) error) error {
		return run(OwnerOptions{Prepare: func(context.Context) (OwnerResources, error) {
			cancel()
			return OwnerResources{Close: func() error { closes++; return nil }}, nil
		}})
	})
	require.ErrorIs(t, Run(ctx, options, []string{"--state-dir", state}), context.Canceled)
	require.Equal(t, 1, closes)
	require.NoDirExists(t, filepath.Join(state, "deployment"))
}

func TestOwnerConfigCannotRedirectHistory(t *testing.T) {
	config := launchOverrides(boot.NewConfig(boot.WithSection("registry", map[string]any{
		"enable_history": false, "history_type": "other", "history_path": "elsewhere",
	}), boot.WithSection("cluster", map[string]any{"enabled": true})), "selected/registry.db")
	require.Equal(t, "selected/registry.db", config.GetString("registry.history_path", ""))
	require.Equal(t, "sqlite", config.GetString("registry.history_type", ""))
	require.True(t, config.GetBool("registry.enable_history", false))
	require.True(t, config.GetBool("cluster.enabled", false))
}

func TestOwnerConfigMergesThroughBootConfig(t *testing.T) {
	history := boot.NewConfig(boot.WithSection("registry", map[string]any{
		"enable_history": true, "history_type": "sqlite", "history_path": "selected/registry.db",
	}))
	owner := boot.NewConfig(boot.WithSection("registry", map[string]any{
		"enable_history": false, "history_type": "other", "history_path": "elsewhere", "cache_size": 32,
	}), boot.WithSection("cluster", map[string]any{"enabled": true, "peers.count": 3}))
	requireSameConfig(t, bootconfig.Merge(owner, history), launchOverrides(owner, "selected/registry.db"))
	requireSameConfig(t, history, launchOverrides(nil, "selected/registry.db"))
}
