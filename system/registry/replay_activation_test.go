// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

type preparedReplayHistory struct {
	*memory.Storage
	preparations, activations int
}

func (h *preparedReplayHistory) ReplayState(context.Context, registry.Version) (*registry.ReplayState, error) {
	return nil, errors.New("projection must share its read with activation preparation")
}

func (h *preparedReplayHistory) CompareAndSetHead(registry.Version, registry.Version) error {
	return errors.New("activation must consume the prepared replay")
}

func (h *preparedReplayHistory) PrepareReplayState(ctx context.Context, target registry.Version) (*registry.ReplayState, registry.ReplayActivation, error) {
	h.preparations++
	state := registry.NewReplayState()
	if err := h.ReplayChanges(ctx, target, func(cs registry.ChangeSet) error { return state.Apply(ctx, cs) }); err != nil {
		return nil, nil, err
	}
	return state, func(expected registry.Version, resolution *registry.DependencyResolution) error {
		h.activations++
		if resolution != nil {
			return h.CompareAndSetHeadWithDependencyResolution(expected, target, resolution)
		}
		return h.Storage.CompareAndSetHead(expected, target)
	}, nil
}

func TestReplayActivationLoadStateAndApplyVersion(t *testing.T) {
	history := &preparedReplayHistory{Storage: memory.New()}
	root, err := history.GetVersion(0)
	require.NoError(t, err)
	require.NoError(t, history.SetHead(root))
	first := version.FromParent(root, 1)
	entry := registry.Entry{ID: registry.NewID("app", "setting"), Kind: "config"}
	require.NoError(t, history.Save(first, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: entry}}, true))
	second := version.FromParent(first, 2)
	require.NoError(t, history.Save(second, registry.ChangeSet{{Kind: registry.EntryDelete, Entry: entry}}, true))

	builder := topology.NewStateBuilder(zap.NewNop(), nil)
	reg := NewRegistry(history, newApplyingMockRunner(builder), builder, nil, zap.NewNop())
	require.NoError(t, reg.LoadState(t.Context(), nil, second))
	require.Equal(t, 1, history.preparations)
	require.Equal(t, 1, history.activations)
	require.NoError(t, reg.ApplyVersion(t.Context(), first))
	_, err = reg.GetEntry(entry.ID)
	require.NoError(t, err)
	require.NoError(t, reg.ApplyVersion(t.Context(), second))
	_, err = reg.GetEntry(entry.ID)
	require.Error(t, err)
	require.Equal(t, 3, history.preparations)
	require.Equal(t, 3, history.activations)
}
