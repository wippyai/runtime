// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
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
