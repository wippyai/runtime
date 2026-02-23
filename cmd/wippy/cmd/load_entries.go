// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"

	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/cmd/internal/entries"
	"go.uber.org/zap"
)

func loadEntriesFromLockPaths(ctx context.Context, lockObj *lock.Lock, logger *zap.Logger) ([]regapi.Entry, error) {
	if lockObj == nil {
		return nil, nil
	}
	modulePaths := lockObj.GetModuleLoadPaths()
	return entries.LoadEntriesFromModuleLoadPaths(ctx, modulePaths, logger)
}
