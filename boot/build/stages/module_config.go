// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
	depconfig "github.com/wippyai/runtime/boot/deps/config"
)

// FilterModuleEntries applies a module manifest's entry and metadata excludes.
// Source-file excludes belong on depconfig.NewSourceFS before entries are loaded.
func FilterModuleEntries(
	ctx context.Context,
	cfg *depconfig.ModuleConfig,
	entries []registry.Entry,
) ([]registry.Entry, error) {
	if cfg == nil {
		return entries, nil
	}
	entryExcludes := cfg.EntryExcludes()
	if len(entryExcludes) == 0 && len(cfg.ExcludeMeta) == 0 {
		return entries, nil
	}

	filtered := append([]registry.Entry(nil), entries...)
	stage := DisableWithOptions(DisableOptions{
		Entries:     entryExcludes,
		MetaFilters: cfg.ExcludeMeta,
	})
	if err := stage.Execute(ctx, &filtered); err != nil {
		return nil, err
	}
	return filtered, nil
}
