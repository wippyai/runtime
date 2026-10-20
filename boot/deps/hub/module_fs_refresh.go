// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	entrypkg "github.com/wippyai/runtime/system/entry"
)

// moduleRootRefreshedEntries returns the fs.directory entries whose registered
// filesystem holds a directory inside a module tree this operation replaces.
// Such an entry often has the same configuration in both selections, so the
// entry diff alone plans no operation, and the registered filesystem keeps the
// handle of the replaced directory.
//
// An entry owned by a replaced module is selected without decoding its
// configuration. Any other entry is selected when its resolved directory lies
// inside a replaced tree, whether it is absolute or project-relative.
func moduleRootRefreshedEntries(
	ctx context.Context,
	transcoder payload.Transcoder,
	staged []stagedModuleDirectory,
	state regapi.State,
) map[string]struct{} {
	if len(staged) == 0 {
		return nil
	}

	replaced := make(map[string]struct{}, len(staged))
	roots := make([]string, 0, len(staged))
	for _, item := range staged {
		if item.module != "" {
			replaced[item.module] = struct{}{}
		}
		if item.targetDir == "" {
			continue
		}
		if root, err := filepath.Abs(item.targetDir); err == nil {
			roots = append(roots, root)
		}
	}

	keys := make(map[string]struct{})
	for _, entry := range state {
		if entry.Kind != dirapi.Kind {
			continue
		}
		if _, owned := replaced[entryModule(entry)]; owned {
			keys[idKey(entry.ID)] = struct{}{}
			continue
		}
		if transcoder == nil || len(roots) == 0 {
			continue
		}
		cfg, err := entrypkg.DecodeEntryConfig[dirapi.Config](ctx, transcoder, entry)
		if err != nil || cfg == nil {
			continue
		}
		dir := dirapi.ResolveDirectory(ctx, entry, cfg)
		if dir == "" {
			continue
		}
		dir, err = filepath.Abs(dir)
		if err != nil {
			continue
		}
		for _, root := range roots {
			if pathWithin(root, dir) {
				keys[idKey(entry.ID)] = struct{}{}
				break
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
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

// moduleRootRefreshIDs lists the selected entries that exist in the final
// state. A transition that rebuilds its changeset as a state delta drops an
// unchanged entry, so the registry re-applies these IDs after the delta.
func moduleRootRefreshIDs(entries regapi.State, keys map[string]struct{}) []regapi.ID {
	if len(keys) == 0 {
		return nil
	}
	ids := make([]regapi.ID, 0, len(keys))
	for _, entry := range entries {
		if _, refresh := keys[idKey(entry.ID)]; refresh {
			ids = append(ids, entry.ID)
		}
	}
	return ids
}
