// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

var (
	refreshDepID    = registry.NewID("app", "dep")
	refreshAssetsID = registry.NewID("mod", "assets")
)

// refreshDirective materializes one module entry whose configuration never
// changes between versions, as a module-relative filesystem entry does.
type refreshDirective struct {
	refresh bool
	stored  bool
}

func (d refreshDirective) result(kind string, resolution *registry.DependencyResolution) registry.DirectiveResult {
	assets := registry.Entry{ID: refreshAssetsID, Kind: "fs.directory", Data: payload.NewString("assets")}
	result := registry.DirectiveResult{
		Applied:    true,
		Resolution: resolution,
		Additional: []registry.ScopedOperation{{
			Operation: registry.Operation{Kind: kind, Entry: assets},
			Scope:     registry.ScopeBaseline,
		}},
	}
	if d.refresh {
		result.Refresh = []registry.ID{refreshAssetsID}
	}
	return result
}

func (d refreshDirective) Expand(_ context.Context, op registry.Operation, snapshot registry.State) (registry.DirectiveResult, error) {
	kind := registry.EntryCreate
	for _, entry := range snapshot {
		if entry.ID == refreshAssetsID {
			kind = registry.EntryUpdate
			break
		}
	}
	if !d.stored {
		return d.result(kind, nil), nil
	}
	version, _ := op.Entry.Data.Data().(string)
	resolution := (&registry.DependencyResolution{InputDigest: "sha256:" + version}).Canonical()
	return d.result(kind, resolution), nil
}

type reconcilingRefreshDirective struct {
	refreshDirective
}

func (d reconcilingRefreshDirective) ReconcileResolutionTransition(
	_ context.Context,
	_ registry.State,
	_ registry.State,
	resolution *registry.DependencyResolution,
) (registry.DirectiveResult, error) {
	return d.result(registry.EntryUpdate, resolution), nil
}

func newRefreshRegistry(t *testing.T, directive registry.Directive) (*Reg, *TestRunner) {
	t.Helper()
	logger := zap.NewNop()
	runner := NewTestRunner()
	resolver := topology.NewResolver()
	reg := NewRegistry(historymem.New(), runner, topology.NewStateBuilder(logger, resolver), resolver, logger,
		WithKindDirective(registry.NamespaceDependency, directive),
	)
	return reg, runner
}

func applyRefreshVersions(t *testing.T, reg *Reg) registry.Version {
	t.Helper()
	ctx := context.Background()
	dep := registry.Entry{ID: refreshDepID, Kind: registry.NamespaceDependency, Data: payload.NewString("1.0.0")}
	v1, err := reg.Apply(ctx, registry.ChangeSet{{Kind: registry.EntryCreate, Entry: dep}})
	require.NoError(t, err)
	dep.Data = payload.NewString("2.0.0")
	_, err = reg.Apply(ctx, registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: dep}})
	require.NoError(t, err)
	return v1
}

func refreshOperation(cs registry.ChangeSet) *registry.Operation {
	for i := range cs {
		if cs[i].Entry.ID == refreshAssetsID {
			return &cs[i]
		}
	}
	return nil
}

// An undo through version expansion rebuilds the changeset as a state delta.
// The delta drops the unchanged entry, so the registry must re-add the update
// that the directive marked for refresh.
func TestApplyVersion_ExpansionReappliesRefreshedEntry(t *testing.T) {
	reg, runner := newRefreshRegistry(t, refreshDirective{refresh: true})
	v1 := applyRefreshVersions(t, reg)

	require.NoError(t, reg.ApplyVersion(context.Background(), v1))

	op := refreshOperation(runner.LastTransition())
	require.NotNil(t, op, "a refreshed entry must be applied again after the delta")
	assert.Equal(t, registry.EntryUpdate, op.Kind)
}

func TestApplyVersion_StoredResolutionReappliesRefreshedEntry(t *testing.T) {
	reg, runner := newRefreshRegistry(t, reconcilingRefreshDirective{refreshDirective{refresh: true, stored: true}})
	v1 := applyRefreshVersions(t, reg)

	require.NoError(t, reg.ApplyVersion(context.Background(), v1))

	op := refreshOperation(runner.LastTransition())
	require.NotNil(t, op, "a refreshed entry must be applied again after the delta")
	assert.Equal(t, registry.EntryUpdate, op.Kind)
}

func TestApplyVersion_UnmarkedUnchangedEntryStaysOutOfDelta(t *testing.T) {
	for name, directive := range map[string]registry.Directive{
		"expansion":         refreshDirective{},
		"stored_resolution": reconcilingRefreshDirective{refreshDirective{stored: true}},
	} {
		t.Run(name, func(t *testing.T) {
			reg, runner := newRefreshRegistry(t, directive)
			v1 := applyRefreshVersions(t, reg)

			require.NoError(t, reg.ApplyVersion(context.Background(), v1))

			assert.Nil(t, refreshOperation(runner.LastTransition()))
		})
	}
}

func TestAppendRefreshOperations(t *testing.T) {
	assets := registry.Entry{ID: refreshAssetsID, Kind: "fs.directory"}
	other := registry.Entry{ID: registry.NewID("mod", "other"), Kind: "fs.directory"}
	stateMap := registry.StateMap{refreshAssetsID: assets, other.ID: other}
	planned := registry.ChangeSet{{Kind: registry.EntryDelete, Entry: other}}

	ops := appendRefreshOperations(planned, stateMap, []registry.ID{
		refreshAssetsID, refreshAssetsID, other.ID, registry.NewID("mod", "missing"),
	})

	require.Len(t, ops, 2)
	assert.Equal(t, registry.EntryDelete, ops[0].Kind)
	assert.Equal(t, registry.Operation{Kind: registry.EntryUpdate, Entry: assets}, ops[1])
	assert.Equal(t, planned, appendRefreshOperations(planned, stateMap, nil))
}
