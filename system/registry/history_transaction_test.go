// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

type transactionHistory struct {
	*historymem.Storage
	outside int
	begins  int
	active  int
}

func (h *transactionHistory) BeginTransaction() {
	h.begins++
	h.active++
}

func (h *transactionHistory) EndTransaction() { h.active-- }

func (h *transactionHistory) Head() (registry.Version, error) {
	h.check()
	return h.Storage.Head()
}

func (h *transactionHistory) SaveWithDependencyResolution(v registry.Version, changes registry.ChangeSet, resolution *registry.DependencyResolution, head bool) error {
	h.check()
	return h.Storage.SaveWithDependencyResolution(v, changes, resolution, head)
}

func (h *transactionHistory) Save(v registry.Version, changes registry.ChangeSet, head bool) error {
	h.check()
	return h.Storage.Save(v, changes, head)
}

func (h *transactionHistory) check() {
	if h.active != 1 {
		h.outside++
	}
}

func TestRegistryOperationsRunInOneHistoryTransaction(t *testing.T) {
	ctx := context.Background()
	history := &transactionHistory{Storage: historymem.New()}
	runner := NewMockRunner()
	runner.RunFunc = func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
		result := topology.NewStateMap(state)
		for _, op := range changes {
			if op.Kind == registry.EntryDelete {
				delete(result, op.Entry.ID)
			} else {
				result[op.Entry.ID] = op.Entry
			}
		}
		return topology.StateMapToSlice(result), nil
	}
	resolver := topology.NewResolver()
	reg := NewRegistry(history, runner, topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop())
	current, err := reg.Current()
	require.NoError(t, err)
	require.NoError(t, reg.LoadState(ctx, nil, current))

	first, err := reg.Apply(ctx, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("app", "one"), Kind: "service"}}})
	require.NoError(t, err)
	_, err = reg.Apply(ctx, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: registry.Entry{ID: registry.NewID("app", "two"), Kind: "service"}}})
	require.NoError(t, err)
	require.NoError(t, reg.ApplyVersion(ctx, first))

	require.Equal(t, 4, history.begins)
	require.Zero(t, history.active)
	require.Zero(t, history.outside)
}
