// SPDX-License-Identifier: MPL-2.0

package entries

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	regapi "github.com/wippyai/runtime/api/registry"
	historyv1 "github.com/wippyai/runtime/api/registry/history/v1"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/internal/version"
	sysreg "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/history/composite"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/history/remote"
	"github.com/wippyai/runtime/system/registry/history/remote/remotetest"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

type stateRunner struct{}

func (stateRunner) Transition(_ context.Context, state regapi.State, changes regapi.ChangeSet, _ func(context.Context)) (regapi.State, error) {
	result := topology.NewStateMap(state)
	for _, operation := range changes {
		if operation.Kind == regapi.EntryDelete {
			delete(result, operation.Entry.ID)
		} else {
			result[operation.Entry.ID] = operation.Entry
		}
	}
	return topology.StateMapToSlice(result), nil
}

func loadWithoutSources(t *testing.T, history regapi.History) *sysreg.Reg {
	t.Helper()
	ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), boot.NewConfig())
	require.NoError(t, err)
	resolver := topology.NewResolver()
	reg := sysreg.NewRegistry(history, stateRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop())
	ctx = regapi.WithRegistry(ctx, reg)
	ctx = regapi.WithResolver(ctx, resolver)
	require.NoError(t, LoadFromLockFile(ctx, zap.NewNop()))
	return reg
}

func TestMissingSourcesRestoresHistoryBaseline(t *testing.T) {
	t.Chdir(t.TempDir())
	_, connection, stop := remotetest.Start()
	t.Cleanup(stop)
	history, err := remote.New(connection, remote.Config{Key: &historyv1.RegistryKey{TenantId: "tenant", EnvironmentId: "stage", RegistryId: "app"}, Timeout: 5 * time.Second})
	require.NoError(t, err)

	base := regapi.Entry{ID: regapi.NewID("app", "base"), Kind: regapi.EntryKind}
	added := regapi.Entry{ID: regapi.NewID("app", "added"), Kind: regapi.EntryKind}
	source := historymem.New()
	require.NoError(t, source.Save(version.FromParent(version.New(0), 1), regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: added}}, true))
	require.NoError(t, history.Transfer(context.Background(), "transfer", source, regapi.State{base}))

	reg := loadWithoutSources(t, composite.New(history))
	require.ElementsMatch(t, []regapi.ID{base.ID, added.ID}, []regapi.ID{reg.Snapshot().Entries[0].ID, reg.Snapshot().Entries[1].ID})
	require.Equal(t, uint(1), reg.Snapshot().Version.ID())
}

func TestMissingSourcesWithoutBaselineStartsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	history := composite.New(historymem.New())
	reg := loadWithoutSources(t, history)
	require.Empty(t, reg.Snapshot().Entries)
	_, err := history.Baseline()
	require.ErrorIs(t, err, regapi.ErrBaselineNotFound)
}

func TestLoadStoresHistoryBaseline(t *testing.T) {
	t.Chdir(t.TempDir())
	ctx, err := bootpkg.NewBootstrapContext(zap.NewNop(), boot.NewConfig())
	require.NoError(t, err)
	history := composite.New(historymem.New())
	resolver := topology.NewResolver()
	reg := sysreg.NewRegistry(history, stateRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop())
	ctx = regapi.WithRegistry(ctx, reg)
	ctx = regapi.WithResolver(ctx, resolver)
	base := regapi.Entry{ID: regapi.NewID("app", "base"), Kind: regapi.EntryKind}
	require.NoError(t, LoadEntriesToRegistry(ctx, []regapi.Entry{base}, zap.NewNop()))
	stored, err := history.Baseline()
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, base.ID, stored[0].ID)
}
