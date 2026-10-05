// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// newTestShutdownSources returns sources whose force exit does nothing.
func newTestShutdownSources() *shutdownSources {
	return &shutdownSources{
		signals:   make(chan os.Signal, 1),
		requests:  make(chan struct{}, 1),
		forceExit: func() {},
	}
}

// newObservedShutdownSources returns sources and a channel that receives each
// force exit instead of the process ending.
func newObservedShutdownSources() (*shutdownSources, <-chan struct{}) {
	forced := make(chan struct{}, 1)
	sources := newTestShutdownSources()
	sources.forceExit = func() { forced <- struct{}{} }
	return sources, forced
}

func waitForShutdownReturns(ctx context.Context, t *testing.T, sources *shutdownSources) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		waitForShutdown(ctx, sources, zap.NewNop(), nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown waiter did not start the shutdown")
	}
}

func requireNoForceExit(t *testing.T, forced <-chan struct{}) {
	t.Helper()
	require.Never(t, func() bool {
		select {
		case <-forced:
			return true
		default:
			return false
		}
	}, 200*time.Millisecond, 5*time.Millisecond, "force exit fired")
}

func requireForceExit(t *testing.T, forced <-chan struct{}) {
	t.Helper()
	select {
	case <-forced:
	case <-time.After(2 * time.Second):
		t.Fatal("force exit did not fire")
	}
}

func TestShutdownRequestStartsGracefulShutdown(t *testing.T) {
	sources, forced := newObservedShutdownSources()
	sources.requests <- struct{}{}

	waitForShutdownReturns(t.Context(), t, sources)
	requireNoForceExit(t, forced)
}

func TestShutdownRequestAfterFirstSignalDoesNotForceExit(t *testing.T) {
	sources, forced := newObservedShutdownSources()
	sources.signals <- syscall.SIGINT
	waitForShutdownReturns(t.Context(), t, sources)

	sources.requests <- struct{}{}
	requireNoForceExit(t, forced)
}

func TestSecondSignalAfterFirstSignalForcesExit(t *testing.T) {
	sources, forced := newObservedShutdownSources()
	sources.signals <- syscall.SIGINT
	waitForShutdownReturns(t.Context(), t, sources)

	sources.signals <- syscall.SIGINT
	requireForceExit(t, forced)
}

func TestSignalAfterShutdownRequestForcesExit(t *testing.T) {
	sources, forced := newObservedShutdownSources()
	sources.requests <- struct{}{}
	waitForShutdownReturns(t.Context(), t, sources)

	sources.signals <- syscall.SIGINT
	requireForceExit(t, forced)
}

func TestSignalAfterCallerCancellationForcesExit(t *testing.T) {
	caller, cancel := context.WithCancel(context.Background())
	runtime, err := detachFromCaller(caller)
	require.NoError(t, err)
	runtime, stop := context.WithCancel(runtime)
	defer stop()
	sources, forced := newObservedShutdownSources()
	cancel()
	waitForShutdownReturns(runtime, t, sources)

	sources.requests <- struct{}{}
	requireNoForceExit(t, forced)
	sources.signals <- syscall.SIGINT
	requireForceExit(t, forced)
}

// A signal that is ready together with a request is the first trigger, not a
// second signal: one Ctrl+C that ends a command whose host then requests
// shutdown stops gracefully.
func TestPendingSignalWithShutdownRequestIsTheFirstTrigger(t *testing.T) {
	sources, forced := newObservedShutdownSources()
	called := make(chan struct{}, 1)
	sources.signals <- syscall.SIGINT
	startShutdown(t.Context(), sources, zap.NewNop(), shutdownCause{kind: causeRequest}, func() { called <- struct{}{} })

	select {
	case <-called:
	default:
		t.Fatal("the pending signal was not the first trigger")
	}
	requireNoForceExit(t, forced)
}

func TestShutdownRequestDoesNotInvokeOnFirstSignal(t *testing.T) {
	sources, _ := newObservedShutdownSources()
	startShutdown(t.Context(), sources, zap.NewNop(), shutdownCause{kind: causeRequest},
		func() { t.Error("a shutdown request reported an OS signal") })
}
