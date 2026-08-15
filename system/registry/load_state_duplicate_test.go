// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

// A baseline carrying two entries under one canonical ID has no single
// authoritative payload; boot must fail naming the entry instead of letting
// map construction silently keep whichever entry came last.
func TestRegistry_LoadState_RejectsDuplicateBaselineIDs(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	hist := history.NewMemory()

	mockRunner := NewMockRunner()
	mockRunner.RunFunc = func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		return state, nil
	}

	resolver := topology.NewResolver()
	reg := NewRegistry(
		hist,
		mockRunner,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
	)

	baseline := registry.State{
		{ID: registry.NewID("app.deps", "crm"), Kind: "ns.dependency", Data: payload.NewString(`{"component":"acme/crm","version":"1.0.0"}`)},
		{ID: registry.NewID("test", "entry1"), Kind: "service"},
		{ID: registry.NewID("app.deps", "crm"), Kind: "ns.dependency", Data: payload.NewString(`{"component":"acme/crm","version":"2.0.0"}`)},
	}

	head, err := reg.Current()
	require.NoError(t, err)

	err = reg.LoadState(ctx, baseline, head)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate entry id")
	require.Contains(t, err.Error(), "crm")
}

// The hydrated ID spelling {NS: "", Name: "ns:name"} names the same entry as
// the split form; the baseline guard must catch that collision too.
func TestRegistry_LoadState_RejectsHydratedDuplicateBaselineIDs(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	hist := history.NewMemory()

	mockRunner := NewMockRunner()
	mockRunner.RunFunc = func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		return state, nil
	}

	resolver := topology.NewResolver()
	reg := NewRegistry(
		hist,
		mockRunner,
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
	)

	baseline := registry.State{
		{ID: registry.NewID("app.deps", "crm"), Kind: "ns.dependency"},
		{ID: registry.ID{Name: "app.deps:crm"}, Kind: "ns.dependency"},
	}

	head, err := reg.Current()
	require.NoError(t, err)

	err = reg.LoadState(ctx, baseline, head)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate entry id")
}

func TestRegistry_LoadStateCanonicalizesBaselineWithoutMutatingCaller(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	hist := history.NewMemory()
	resolver := topology.NewResolver()
	reg := NewRegistry(
		hist,
		NewTestRunner(),
		topology.NewStateBuilder(logger, resolver),
		resolver,
		logger,
	)

	rawID := registry.ID{NS: "app", Name: "service"}
	canonicalID := registry.NewID("app", "service")
	baseline := registry.State{{ID: rawID, Kind: registry.EntryKind}}
	head, err := reg.Current()
	require.NoError(t, err)
	require.NoError(t, reg.LoadState(ctx, baseline, head))

	stored, err := reg.GetEntry(canonicalID)
	require.NoError(t, err)
	require.Equal(t, canonicalID, stored.ID)
	require.Equal(t, rawID, baseline[0].ID)
}
