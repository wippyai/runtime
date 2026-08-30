// SPDX-License-Identifier: MPL-2.0

// Package migration contains one-time registry storage migrations.
package migration

import (
	"context"
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/history/postgres"
	"github.com/wippyai/runtime/system/registry/history/sqlite"
)

// Apply normalizes durable operation rows before normal registry replay. The
// migration records its registry-history child transition (1.0 to 1.1) in the
// existing schema ledger; in-memory histories contain no persisted rows.
func Apply(ctx context.Context, history registry.History, baseline registry.State) error {
	switch history.(type) {
	case *sqlite.History, *postgres.History:
	default:
		return nil
	}

	baseline = append(registry.State(nil), baseline...)
	if resolutions, ok := history.(registry.ResolutionHistory); ok {
		head, err := history.Head()
		if err != nil {
			return fmt.Errorf("read registry head for metadata migration: %w", err)
		}
		resolution, err := resolutions.GetDependencyResolution(head)
		switch {
		case err == nil:
			baseline = markResolutionRoots(baseline, resolution)
		case errors.Is(err, registry.ErrDependencyResolutionNotFound):
		default:
			return fmt.Errorf("read dependency resolution for metadata migration: %w", err)
		}
	}
	switch history := history.(type) {
	case *sqlite.History:
		return sqlite.MigrateEntryMetadata(ctx, history, baseline)
	case *postgres.History:
		return postgres.MigrateEntryMetadata(ctx, history, baseline)
	}
	return nil
}

func markResolutionRoots(baseline registry.State, resolution *registry.DependencyResolution) registry.State {
	if resolution == nil {
		return baseline
	}
	indexes := make(map[registry.ID]int, len(baseline))
	for i := range baseline {
		indexes[baseline[i].ID.Canonical()] = i
	}
	for _, root := range resolution.Roots {
		id := registry.ParseID(root.ID).Canonical()
		if index, ok := indexes[id]; ok {
			baseline[index].Registry.Root = true
			continue
		}
		indexes[id] = len(baseline)
		baseline = append(baseline, registry.Entry{ID: id, Registry: registry.EntryMetadata{Root: true}})
	}
	return baseline
}
