// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/cmd/wippy/cmd"
)

func TestSignalScopeCancelsOnSignalBeforeRelease(t *testing.T) {
	signals := make(chan os.Signal, 1)
	ctx, scope := newSignalScope(t.Context(), signals, func() {})
	defer scope.close()

	signals <- os.Interrupt
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("a signal before release did not cancel the invocation")
	}
}

func TestSignalScopeReleaseHandsSignalsOverWithoutCanceling(t *testing.T) {
	signals := make(chan os.Signal, 1)
	stopped := false
	ctx, scope := newSignalScope(t.Context(), signals, func() { stopped = true })
	defer scope.close()

	releaseSignals(ctx)
	require.True(t, stopped, "release stops signal capture")
	signals <- os.Interrupt
	require.Never(t, func() bool { return ctx.Err() != nil }, 100*time.Millisecond, time.Millisecond,
		"a released scope canceled the invocation")
}

func TestRunnerReleasesSignalsBeforeHandingOverToTheCLI(t *testing.T) {
	signals := make(chan os.Signal, 1)
	stopped := false
	ctx, scope := newSignalScope(t.Context(), signals, func() { stopped = true })
	defer scope.close()
	record := captureExecution(t)
	released := false
	record.during = func(cmd.ExecuteOptions) error {
		released = stopped
		return nil
	}

	state := filepath.Join(t.TempDir(), "state")
	require.NoError(t, Run(ctx, runnableExecutable(t), []string{"--state", state}))
	require.Equal(t, 1, record.calls)
	require.True(t, released, "the CLI ran while the runner still captured signals")
	require.NoError(t, ctx.Err())
}

func TestReleaseSignalsWithoutScopeIsHarmless(t *testing.T) {
	releaseSignals(context.Background())
}
