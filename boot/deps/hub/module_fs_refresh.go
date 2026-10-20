// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	entrypkg "github.com/wippyai/runtime/system/entry"
)

// moduleRootRefreshedEntries returns the entries whose resolved directory
// lives inside a module tree this operation replaces. A module-relative
// fs.directory entry has the same configuration in both selections, so the
// entry diff alone plans no operation, and the registered filesystem keeps
// the handle of the replaced directory.
func moduleRootRefreshedEntries(
	ctx context.Context,
	transcoder payload.Transcoder,
	staged []stagedModuleDirectory,
	state regapi.State,
) map[string]struct{} {
	if len(staged) == 0 || transcoder == nil {
		return nil
	}

	replaced := make(map[string]struct{}, len(staged))
	for _, item := range staged {
		if item.module != "" {
			replaced[item.module] = struct{}{}
		}
	}
	if len(replaced) == 0 {
		return nil
	}

	keys := make(map[string]struct{})
	for _, entry := range state {
		if entry.Kind != dirapi.Kind {
			continue
		}
		if _, owned := replaced[entryModule(entry)]; !owned {
			continue
		}
		cfg, err := entrypkg.DecodeEntryConfig[dirapi.Config](ctx, transcoder, entry)
		if err != nil || cfg == nil {
			continue
		}
		if dirapi.IsConfiguredPathAbsolute(cfg.Directory) || cfg.Base == dirapi.BaseProject {
			continue
		}
		keys[idKey(entry.ID)] = struct{}{}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

// appendModuleRootRefreshes adds the missing update operations for the entries
// selected by moduleRootRefreshedEntries. An operation the planner already
// emitted for the same entry wins, because a create, update or delete covers
// the registration.
func appendModuleRootRefreshes(
	ops []regapi.Operation,
	entries regapi.State,
	keys map[string]struct{},
) []regapi.Operation {
	if len(keys) == 0 {
		return ops
	}

	planned := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		planned[idKey(op.Entry.ID)] = struct{}{}
	}
	for _, entry := range entries {
		key := idKey(entry.ID)
		if _, refresh := keys[key]; !refresh {
			continue
		}
		if _, exists := planned[key]; exists {
			continue
		}
		ops = append(ops, regapi.Operation{Kind: regapi.EntryUpdate, Entry: entry})
	}
	return ops
}
