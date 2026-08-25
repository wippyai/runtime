// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// A deployment installed its roots before registry-owned provenance: the hot
// install's history rows carry no provenance and state root-ness only through
// the wire DependencyRoot flag. Booting that history on the current runtime
// must report those roots — a rootless replay changes the deployment baseline
// digest and forces a re-resolution the environment cannot answer offline.
func TestLoadStateReplaysLegacyDependencyRoots(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	hist := history.NewMemory()

	mockRunner := NewMockRunner()
	mockRunner.RunFunc = func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		stateMap := make(map[registry.ID]registry.Entry)
		for _, e := range state {
			stateMap[e.ID] = e
		}
		for _, op := range changes {
			switch op.Kind {
			case registry.EntryCreate, registry.EntryUpdate:
				stateMap[op.Entry.ID] = op.Entry
			case registry.EntryDelete:
				delete(stateMap, op.Entry.ID)
			}
		}
		result := make(registry.State, 0, len(stateMap))
		for _, e := range stateMap {
			result = append(result, e)
		}
		return result, nil
	}

	resolver := topology.NewResolver()
	reg := NewRegistry(
		hist,
		mockRunner,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
	)

	root := version.New(registry.RootVersion)
	require.NoError(t, hist.Save(root, registry.ChangeSet{}, true))

	rootID := registry.NewID("app.deps", "graph")
	legacyInstall := registry.ChangeSet{{
		Kind: registry.EntryCreate,
		Entry: registry.Entry{
			ID:             rootID,
			Kind:           "ns.dependency",
			Data:           payload.NewString(`{"component":"chestor/graph","version":">=0.1.0"}`),
			DependencyRoot: true,
		},
	}}
	installed := version.FromParent(root, 1)
	require.NoError(t, hist.Save(installed, legacyInstall, true))

	baseline := registry.State{
		{ID: registry.NewID("test", "service"), Kind: "service"},
	}
	require.NoError(t, reg.LoadState(ctx, hostProvenanced(baseline), installed))

	roots := reg.DependencyRoots()
	require.Len(t, roots, 1, "the legacy hot install replays as the root it was")
	require.Equal(t, rootID, roots[0])

	prov, ok := reg.EntryProvenance(rootID)
	require.True(t, ok)
	require.True(t, prov.Root)
}
