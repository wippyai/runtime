// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"fmt"
	"os"
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
