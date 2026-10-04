// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"

	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/cmd/internal/shutdown"
	"go.uber.org/zap"
)

// runShutdown owns the lifetime of one loaded command runtime. Pack loading
// has an outer phase that can fail before runPackEntries is called, so the
// same one-shot owner is shared by both phases. A deferred cleanup must also
// be safe when the normal path has already performed it.
type runShutdown struct {
	once     sync.Once
	exitCode int
}

func (s *runShutdown) perform(ctx context.Context, loader *bootpkg.Loader, logger *zap.Logger, silent bool) int {
	s.once.Do(func() {
		s.exitCode = shutdown.Perform(ctx, loader, logger, silent)
	})
	return s.exitCode
}

func (s *runShutdown) deferCleanup(ctx context.Context, result *error, loader *bootpkg.Loader, logger *zap.Logger, silent bool) {
	exitCode := s.perform(ctx, loader, logger, silent)
	// Preserve an existing startup/launch/cancellation error. A programmatic
	// exit code is only actionable when the runtime itself otherwise completed
	// successfully, matching the old explicit-shutdown path.
	if *result == nil && exitCode != 0 {
		_ = logger.Sync()
		os.Exit(exitCode)
	}
}

type callerCancellationKey struct{}

// detachFromCaller roots a runtime in a context that the caller's cancellation
// does not reach. Infrastructure such as the control mailbox must outlive the
// supervisor stop sequence, so the runtime ends only through its own shutdown.
// The caller's cancellation stays observable through callerCancellation and
// starts that shutdown.
func detachFromCaller(caller context.Context) (context.Context, error) {
	if caller == nil {
		return nil, fmt.Errorf("caller context is required")
	}
	if err := caller.Err(); err != nil {
		return nil, err
	}
	return context.WithValue(context.WithoutCancel(caller), callerCancellationKey{}, caller.Done()), nil
}

// callerCancellation returns a channel closed when the caller of a detached
// runtime cancels its context. It is nil, and never ready, otherwise.
func callerCancellation(ctx context.Context) <-chan struct{} {
	done, _ := ctx.Value(callerCancellationKey{}).(<-chan struct{})
	return done
}

// shutdownSources carries what starts and escalates the graceful shutdown.
// OS termination signals and programmatic shutdown requests arrive on separate
// channels because only a signal from the user may force exit.
type shutdownSources struct {
	signals   chan os.Signal
	requests  chan struct{}
	forceExit func()
}

// stop releases the OS signal subscription.
func (s *shutdownSources) stop() {
	signal.Stop(s.signals)
}

type shutdownCauseKind int

const (
	causeSignal shutdownCauseKind = iota
	causeRequest
	causeCallerCancellation
)

// shutdownCause is the trigger that began the graceful shutdown.
type shutdownCause struct {
	signal os.Signal
	kind   shutdownCauseKind
}

// startShutdown begins the graceful shutdown for its first trigger and arms
// the force exit. When several triggers are ready together, a pending OS
// signal is the first trigger, so the signal that accompanies a request is
// never taken for a second one. onFirstSignal runs only for an OS signal.
func startShutdown(ctx context.Context, sources *shutdownSources, logger *zap.Logger, cause shutdownCause, onFirstSignal func()) {
	if cause.kind != causeSignal {
		select {
		case sig := <-sources.signals:
			cause = shutdownCause{kind: causeSignal, signal: sig}
		default:
		}
	}
	switch cause.kind {
	case causeSignal:
		logger.Info("received shutdown signal", zap.String("signal", cause.signal.String()))
		if onFirstSignal != nil {
			onFirstSignal()
		}
	case causeRequest:
		logger.Info("shutdown requested")
	case causeCallerCancellation:
		logger.Info("caller context canceled")
	}
	armForceExit(ctx, sources, logger)
}

// armForceExit makes the next OS signal terminate the process while the
// graceful shutdown proceeds. Shutdown requests never force exit.
func armForceExit(ctx context.Context, sources *shutdownSources, logger *zap.Logger) {
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-sources.signals:
			logger.Error("force exit")
			sources.forceExit()
		}
	}()

	if !silentLogs {
		logger.Info("shutting down (press Ctrl+C again to force exit)")
	}
}
