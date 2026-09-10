// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
)

func launchOptions(callback Launch) Options {
	return Options{Name: "launch-test", Command: "desktop", Mode: "base", Launch: callback}
}

func TestClientLaunchDoesNotOpenOwnerState(t *testing.T) {
	state := filepath.Join(t.TempDir(), "absent")
	options := launchOptions(func(_ context.Context, request LaunchRequest, _ func(OwnerOptions) error) error {
		require.Equal(t, state, request.StateDir)
		require.Equal(t, []string{"terminal", "two words"}, request.Arguments)
		require.NotEmpty(t, request.Directory)
		return nil
	})
	options.DataEnv = map[string]string{"INVALID NAME": "../outside"}
	require.NoError(t, Run(t.Context(), options, []string{"--state-dir", state, "run", "terminal", "two words"}))
	require.NoDirExists(t, state)
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

func TestExpiredOwnerDoesNotOpenDeployment(t *testing.T) {
	state := t.TempDir()
	closes := 0
	options := launchOptions(func(_ context.Context, _ LaunchRequest, run func(OwnerOptions) error) error {
		return run(OwnerOptions{Prepare: func(context.Context) (OwnerResources, error) {
			return OwnerResources{Deadline: time.Now().Add(-time.Second), Close: func() error { closes++; return nil }}, nil
		}})
	})
	require.ErrorIs(t, Run(t.Context(), options, []string{"--state-dir", state}), context.DeadlineExceeded)
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
