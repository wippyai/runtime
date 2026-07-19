// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	regtop "github.com/wippyai/runtime/system/registry/topology"
)

func TestOperationPlanner_MutableArtifactBoundary(t *testing.T) {
	base := plannerTestEntry("service", "acme/service", "v1.0.0", "sha256:old", "resident")

	tests := []struct {
		name      string
		mutate    func(regapi.Entry) regapi.Entry
		wantKinds []string
		mutable   bool
		wantError bool
	}{
		{
			name: "identical",
			mutate: func(entry regapi.Entry) regapi.Entry {
				return entry
			},
		},
		{
			name: "immutable digest metadata drift",
			mutate: func(entry regapi.Entry) regapi.Entry {
				entry.Meta.Set(metaModuleDigestKey, "sha256:new")
				return entry
			},
		},
		{
			name:    "mutable digest metadata drift",
			mutable: true,
			mutate: func(entry regapi.Entry) regapi.Entry {
				entry.Meta.Set(metaModuleDigestKey, "sha256:new")
				return entry
			},
			wantKinds: []string{regapi.EntryUpdate},
		},
		{
			name: "immutable data normalization",
			mutate: func(entry regapi.Entry) regapi.Entry {
				entry.Data = payload.New(map[string]any{"value": "normalized"})
				return entry
			},
		},
		{
			name:    "mutable host data drift",
			mutable: true,
			mutate: func(entry regapi.Entry) regapi.Entry {
				entry.Data = payload.New(map[string]any{"value": "changed"})
				return entry
			},
			wantKinds: []string{regapi.EntryUpdate},
		},
		{
			name: "version change remains effective",
			mutate: func(entry regapi.Entry) regapi.Entry {
				entry.Meta.Set(metaModuleVersionKey, "v2.0.0")
				return entry
			},
			wantKinds: []string{regapi.EntryUpdate},
		},
		{
			name: "owner conflict remains effective",
			mutate: func(entry regapi.Entry) regapi.Entry {
				entry.Meta.Set(metaModuleKey, "other/service")
				return entry
			},
			wantError: true,
		},
		{
			name: "kind change remains effective",
			mutate: func(entry regapi.Entry) regapi.Entry {
				entry.Kind = "other.kind"
				return entry
			},
			wantKinds: []string{regapi.EntryDelete, regapi.EntryCreate},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := clonePlannerTestEntry(base)
			desired := tt.mutate(clonePlannerTestEntry(current))
			mutable := map[string]struct{}{}
			if tt.mutable {
				mutable[entryModule(desired)] = struct{}{}
			}

			ops, err := (operationPlanner{}).plan(regapi.State{current}, []regapi.Entry{desired}, operationPlanOptions{
				controlledModules: map[string]struct{}{"acme/service": {}},
				mutableModules:    mutable,
			})
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, ops, len(tt.wantKinds))
			for i, kind := range tt.wantKinds {
				assert.Equal(t, kind, ops[i].Kind)
			}
		})
	}
}

func TestOperationPlanner_DependencyRootTransitionsRemainEffective(t *testing.T) {
	for _, test := range []struct {
		name string
		from bool
		to   bool
	}{
		{name: "promotion", from: false, to: true},
		{name: "demotion", from: true, to: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := plannerTestEntry(regapi.NamespaceDependency, "acme/deployment", "v1.0.0", "sha256:a", "root")
			current.DependencyRoot = test.from
			desired := clonePlannerTestEntry(current)
			desired.DependencyRoot = test.to

			ops, err := (operationPlanner{}).plan(regapi.State{current}, []regapi.Entry{desired}, operationPlanOptions{
				controlledModules: map[string]struct{}{"acme/deployment": {}},
				mutableModules:    map[string]struct{}{},
			})
			require.NoError(t, err)
			require.Len(t, ops, 1)
			assert.Equal(t, regapi.EntryUpdate, ops[0].Kind)
			assert.Equal(t, test.to, ops[0].Entry.DependencyRoot)
		})
	}
}

func TestOperationPlanner_ActiveApplicationAdoptsOnlyDeploymentRootDeclarations(t *testing.T) {
	const application = "acme/application"
	current := regapi.Entry{
		ID:             regapi.NewID("app.deps", "service"),
		Kind:           regapi.NamespaceDependency,
		DependencyRoot: true,
		Data:           payload.New(map[string]any{"component": "acme/service", "version": "*"}),
	}
	desired := clonePlannerTestEntry(current)
	desired.DependencyRoot = false
	desired.Meta = attrs.NewBagFrom(map[string]any{
		metaModuleKey:        application,
		metaModuleVersionKey: "v2.0.0",
		metaModuleDigestKey:  "sha256:new",
	})
	desired.Data = payload.New(map[string]any{"component": "acme/service", "version": ">=v2.0.0"})

	ops, err := (operationPlanner{}).plan(regapi.State{current}, []regapi.Entry{desired}, operationPlanOptions{
		controlledModules:     map[string]struct{}{application: {}},
		mutableModules:        map[string]struct{}{application: {}},
		deploymentRootModules: map[string]struct{}{application: {}},
	})
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, regapi.EntryUpdate, ops[0].Kind)
	assert.Equal(t, desired, ops[0].Entry)
}

func TestOperationPlanner_ActiveApplicationAdoptsPreviouslyUpdatedDependencyDeclaration(t *testing.T) {
	const application = "acme/application"
	current := regapi.Entry{
		ID:   regapi.NewID("app.deps", "service"),
		Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{"component": "acme/service", "version": ">=v1.0.0"}),
	}
	desired := clonePlannerTestEntry(current)
	desired.Meta = attrs.NewBagFrom(map[string]any{metaModuleKey: application})
	desired.Data = payload.New(map[string]any{"component": "acme/service", "version": ">=v2.0.0"})

	ops, err := (operationPlanner{}).plan(regapi.State{current}, []regapi.Entry{desired}, operationPlanOptions{
		deploymentRootModules: map[string]struct{}{application: {}},
	})
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, regapi.EntryUpdate, ops[0].Kind)
	assert.Equal(t, application, entryModule(ops[0].Entry))
}

func TestOperationPlanner_ApplicationAdoptionBoundaryRejectsOtherClaims(t *testing.T) {
	const application = "acme/application"
	baseCurrent := regapi.Entry{
		ID:             regapi.NewID("app.deps", "service"),
		Kind:           regapi.NamespaceDependency,
		DependencyRoot: true,
		Data:           payload.New(map[string]any{"component": "acme/service", "version": "*"}),
	}
	baseDesired := clonePlannerTestEntry(baseCurrent)
	baseDesired.DependencyRoot = false
	baseDesired.Meta = attrs.NewBagFrom(map[string]any{metaModuleKey: application})

	tests := []struct {
		mutate func(*regapi.Entry, *regapi.Entry)
		roots  map[string]struct{}
		name   string
	}{
		{
			name:  "package is not the active deployment root",
			roots: map[string]struct{}{"acme/other": {}},
		},
		{
			name: "ordinary unowned host entry",
			mutate: func(current, desired *regapi.Entry) {
				current.Kind = "http.service"
				desired.Kind = "http.service"
			},
			roots: map[string]struct{}{application: {}},
		},
		{
			name: "declaration remains a deployment root",
			mutate: func(_, desired *regapi.Entry) {
				desired.DependencyRoot = true
			},
			roots: map[string]struct{}{application: {}},
		},
		{
			name: "declaration belongs to another module",
			mutate: func(current, _ *regapi.Entry) {
				current.Meta = attrs.NewBagFrom(map[string]any{metaModuleKey: "acme/owner"})
			},
			roots: map[string]struct{}{application: {}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := clonePlannerTestEntry(baseCurrent)
			desired := clonePlannerTestEntry(baseDesired)
			if test.mutate != nil {
				test.mutate(&current, &desired)
			}
			_, err := (operationPlanner{}).plan(regapi.State{current}, []regapi.Entry{desired}, operationPlanOptions{
				deploymentRootModules: test.roots,
			})
			require.Error(t, err)
		})
	}
}

func TestOperationPlanner_NilMutableSetUsesNormalDiff(t *testing.T) {
	current := plannerTestEntry("service", "acme/service", "v1.0.0", "sha256:old", "same")
	desired := clonePlannerTestEntry(current)
	desired.Meta.Set(metaModuleDigestKey, "sha256:new")

	ops, err := (operationPlanner{}).plan(regapi.State{current}, []regapi.Entry{desired}, operationPlanOptions{})
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, regapi.EntryUpdate, ops[0].Kind)
}

func TestOperationPlanner_ControlledDeletionBoundary(t *testing.T) {
	controlled := plannerTestEntry("service", "acme/controlled", "v1.0.0", "sha256:a", "controlled")
	unrelated := plannerTestEntry("service", "acme/unrelated", "v1.0.0", "sha256:b", "unrelated")
	unrelated.ID = regapi.NewID("other", "service")
	host := regapi.Entry{ID: regapi.NewID("app", "host"), Kind: "service", Data: payload.New("host")}

	ops, err := (operationPlanner{}).plan(regapi.State{controlled, unrelated, host}, nil, operationPlanOptions{
		controlledModules: map[string]struct{}{"acme/controlled": {}},
	})
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, regapi.EntryDelete, ops[0].Kind)
	assert.Equal(t, controlled.ID, ops[0].Entry.ID)
}

func TestOperationPlanner_LiveDependentRetainsControlledEntry(t *testing.T) {
	target := plannerTestEntry("library", "acme/library", "v1.0.0", "sha256:a", "target")
	dependent := regapi.Entry{
		ID:   regapi.NewID(target.ID.NS, "dependent"),
		Kind: "service",
		Meta: attrs.NewBagFrom(map[string]any{regapi.TagDependsOn: []string{target.ID.Name}}),
		Data: payload.New("dependent"),
	}

	ops, err := (operationPlanner{resolver: regtop.NewResolver()}).plan(
		regapi.State{target, dependent},
		[]regapi.Entry{dependent},
		operationPlanOptions{controlledModules: map[string]struct{}{"acme/library": {}}},
	)
	require.NoError(t, err)
	assert.Empty(t, ops)
}

func TestOperationPlanner_PlanConvergesAndIsIdempotent(t *testing.T) {
	const entryCount = 4
	for currentMask := 0; currentMask < 1<<entryCount; currentMask++ {
		for desiredMask := 0; desiredMask < 1<<entryCount; desiredMask++ {
			name := fmt.Sprintf("current_%02d_desired_%02d", currentMask, desiredMask)
			t.Run(name, func(t *testing.T) {
				current := make(regapi.State, 0, entryCount)
				desired := make([]regapi.Entry, 0, entryCount)
				controlled := make(map[string]struct{}, entryCount)
				for i := 0; i < entryCount; i++ {
					module := fmt.Sprintf("acme/module-%d", i)
					controlled[module] = struct{}{}
					if currentMask&(1<<i) != 0 {
						entry := plannerTestEntry("service", module, "v1.0.0", fmt.Sprintf("sha256:current-%d", i), fmt.Sprintf("current-%d", i))
						entry.ID = regapi.NewID("test", fmt.Sprintf("entry_%d", i))
						current = append(current, entry)
					}
					if desiredMask&(1<<i) != 0 {
						entry := plannerTestEntry("service", module, "v1.0.0", fmt.Sprintf("sha256:desired-%d", i), fmt.Sprintf("desired-%d", i))
						entry.ID = regapi.NewID("test", fmt.Sprintf("entry_%d", i))
						desired = append(desired, entry)
					}
				}

				planner := operationPlanner{}
				opts := operationPlanOptions{controlledModules: controlled, mutableModules: controlled}
				ops, err := planner.plan(current, desired, opts)
				require.NoError(t, err)
				got := applyPlannerOperations(current, ops)
				assertPlannerStatesEqual(t, desired, got)

				next, err := planner.plan(got, desired, opts)
				require.NoError(t, err)
				assert.Empty(t, next)
			})
		}
	}
}

func plannerTestEntry(kind regapi.Kind, module, version, digest, value string) regapi.Entry {
	return regapi.Entry{
		ID:   regapi.NewID("test", "entry"),
		Kind: kind,
		Meta: attrs.NewBagFrom(map[string]any{
			metaModuleKey:        module,
			metaModuleVersionKey: version,
			metaModuleDigestKey:  digest,
		}),
		Data: payload.New(map[string]any{"value": value}),
	}
}

func clonePlannerTestEntry(entry regapi.Entry) regapi.Entry {
	entry.Meta = attrs.NewBagFrom(entry.Meta)
	if entry.Data != nil {
		if data, ok := entry.Data.Data().(map[string]any); ok {
			copyData := make(map[string]any, len(data))
			for key, value := range data {
				copyData[key] = value
			}
			entry.Data = payload.New(copyData)
		}
	}
	return entry
}

func applyPlannerOperations(current regapi.State, ops []regapi.Operation) regapi.State {
	entries := make(map[string]regapi.Entry, len(current)+len(ops))
	for _, entry := range current {
		entries[idKey(entry.ID)] = entry
	}
	for _, op := range ops {
		switch op.Kind {
		case regapi.EntryCreate, regapi.EntryUpdate:
			entries[idKey(op.Entry.ID)] = op.Entry
		case regapi.EntryDelete:
			delete(entries, idKey(op.Entry.ID))
		}
	}
	result := make(regapi.State, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	return result
}

func assertPlannerStatesEqual(t *testing.T, want []regapi.Entry, got regapi.State) {
	t.Helper()
	require.Len(t, got, len(want))
	gotByID := entriesByID(got)
	for _, entry := range want {
		actual, ok := gotByID[idKey(entry.ID)]
		require.True(t, ok, "missing entry %s", entry.ID)
		assert.Equal(t, entry, actual, "entry %s differs", entry.ID)
	}
}
