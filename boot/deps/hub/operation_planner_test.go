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
	"go.uber.org/zap"
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
				entry.Meta.Set(fixtureModuleDigestKey, "sha256:new")
				return entry
			},
		},
		{
			// Resident identity is not author content: a module that ships the
			// same bytes at a new artifact identity produces no operation, even
			// while it is being updated.
			name:    "mutable digest change alone",
			mutable: true,
			mutate: func(entry regapi.Entry) regapi.Entry {
				entry.Meta.Set(fixtureModuleDigestKey, "sha256:new")
				return entry
			},
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
			name: "version change alone",
			mutate: func(entry regapi.Entry) regapi.Entry {
				entry.Meta.Set(fixtureModuleVersionKey, "v2.0.0")
				return entry
			},
		},
		{
			name: "owner conflict remains effective",
			mutate: func(entry regapi.Entry) regapi.Entry {
				entry.Meta.Set(fixtureModuleKey, "other/service")
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
				mutable[fixtureProvenance(desired).Module] = struct{}{}
			}

			ops, err := (operationPlanner{}).plan(fixtureState(regapi.State{current}), fixtureState([]regapi.Entry{desired}), operationPlanOptions{
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

			currentState := fixtureState(regapi.State{current})
			ops, err := (operationPlanner{}).plan(currentState, fixtureState([]regapi.Entry{desired}), operationPlanOptions{
				controlledModules: map[string]struct{}{"acme/deployment": {}},
				mutableModules:    map[string]struct{}{},
			})
			require.NoError(t, err)
			require.Len(t, ops, 1, "a root transition produces exactly one operation")
			assert.Equal(t, regapi.EntryUpdate, ops[0].Kind)
			require.NotNil(t, ops[0].Provenance)
			assert.Equal(t, test.to, ops[0].Provenance.Root)
			assert.True(t, entriesEqual(currentState.Entries[0], ops[0].Entry), "a root transition changes no author content")
		})
	}
}

func TestOperationPlanner_NilMutableSetUsesNormalDiff(t *testing.T) {
	current := plannerTestEntry("service", "acme/service", "v1.0.0", "sha256:old", "same")
	changed := clonePlannerTestEntry(current)
	changed.Data = payload.New(map[string]any{"value": "changed"})

	ops, err := (operationPlanner{}).plan(fixtureState(regapi.State{current}), fixtureState([]regapi.Entry{changed}), operationPlanOptions{})
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, regapi.EntryUpdate, ops[0].Kind)

	rebuilt := clonePlannerTestEntry(current)
	rebuilt.Meta.Set(fixtureModuleDigestKey, "sha256:new")
	ops, err = (operationPlanner{}).plan(fixtureState(regapi.State{current}), fixtureState([]regapi.Entry{rebuilt}), operationPlanOptions{})
	require.NoError(t, err)
	assert.Empty(t, ops, "a resident identity change alone is not an author content change")
}

func TestOperationPlanner_ControlledDeletionBoundary(t *testing.T) {
	controlled := plannerTestEntry("service", "acme/controlled", "v1.0.0", "sha256:a", "controlled")
	unrelated := plannerTestEntry("service", "acme/unrelated", "v1.0.0", "sha256:b", "unrelated")
	unrelated.ID = regapi.NewID("other", "service")
	host := regapi.Entry{ID: regapi.NewID("app", "host"), Kind: "service", Data: payload.New("host")}

	ops, err := (operationPlanner{}).plan(fixtureState(regapi.State{controlled, unrelated, host}), regapi.ProvenancedState{}, operationPlanOptions{
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
		fixtureState(regapi.State{target, dependent}),
		fixtureState([]regapi.Entry{dependent}),
		operationPlanOptions{controlledModules: map[string]struct{}{"acme/library": {}}},
	)
	require.NoError(t, err)
	assert.Empty(t, ops)
}

func TestOperationPlanner_KindReplacementRecreatesLiveDependentClosure(t *testing.T) {
	provider := plannerTestEntry("test.provider", "acme/provider", "v1.0.0", "sha256:a", "provider")
	provider.ID = regapi.NewID("test", "provider")
	dependent := plannerTestEntry("test.dependent", "acme/dependent", "v1.0.0", "sha256:b", "dependent")
	dependent.ID = regapi.NewID("test", "dependent")
	dependent.Meta.Set(regapi.TagDependsOn, []string{provider.ID.Name})
	outer := plannerTestEntry("test.outer", "acme/outer", "v1.0.0", "sha256:c", "outer")
	outer.ID = regapi.NewID("test", "outer")
	outer.Meta.Set(regapi.TagDependsOn, []string{dependent.ID.Name})
	unrelated := plannerTestEntry("test.unrelated", "acme/unrelated", "v1.0.0", "sha256:d", "unrelated")
	unrelated.ID = regapi.NewID("test", "unrelated")

	desiredProvider := clonePlannerTestEntry(provider)
	desiredProvider.Kind = "test.provider.v2"
	currentState := fixtureState(regapi.State{provider, dependent, outer, unrelated})
	desiredState := fixtureState([]regapi.Entry{desiredProvider, dependent, outer, unrelated})
	resolver := regtop.NewResolver()
	ops, err := (operationPlanner{resolver: resolver}).plan(currentState, desiredState, operationPlanOptions{
		controlledModules: map[string]struct{}{
			"acme/provider": {}, "acme/dependent": {}, "acme/outer": {}, "acme/unrelated": {},
		},
	})
	require.NoError(t, err)
	require.Len(t, ops, 6)

	counts := make(map[string]map[string]int)
	for _, op := range ops {
		key := idKey(op.Entry.ID)
		if counts[key] == nil {
			counts[key] = make(map[string]int)
		}
		counts[key][op.Kind]++
	}
	for _, id := range []regapi.ID{provider.ID, dependent.ID, outer.ID} {
		assert.Equal(t, 1, counts[idKey(id)][regapi.EntryDelete])
		assert.Equal(t, 1, counts[idKey(id)][regapi.EntryCreate])
	}
	assert.Nil(t, counts[idKey(unrelated.ID)])

	builder := regtop.NewStateBuilder(zap.NewNop(), resolver)
	sorted, err := builder.SortChangeSet(currentState.Entries, ops)
	require.NoError(t, err)
	state := regtop.NewStateMap(currentState.Entries)
	for _, op := range sorted {
		state, err = builder.ApplyOperation(state, op)
		require.NoError(t, err)
	}
	assertPlannerStatesEqual(t, desiredState, applyPlannerOperations(currentState, ops))
	require.Len(t, regtop.StateMapToSlice(state), len(desiredState.Entries))
}

func TestOperationPlanner_KindReplacementDoesNotRecreateRewiredDependent(t *testing.T) {
	provider := plannerTestEntry("test.provider", "acme/provider", "v1.0.0", "sha256:a", "provider")
	provider.ID = regapi.NewID("test", "provider")
	dependent := plannerTestEntry("test.dependent", "acme/dependent", "v1.0.0", "sha256:b", "dependent")
	dependent.ID = regapi.NewID("test", "dependent")
	dependent.Meta.Set(regapi.TagDependsOn, []string{provider.ID.Name})

	desiredProvider := clonePlannerTestEntry(provider)
	desiredProvider.Kind = "test.provider.v2"
	desiredDependent := clonePlannerTestEntry(dependent)
	desiredDependent.Meta.Set(regapi.TagDependsOn, []string{})
	ops, err := (operationPlanner{resolver: regtop.NewResolver()}).plan(
		fixtureState(regapi.State{provider, dependent}),
		fixtureState([]regapi.Entry{desiredProvider, desiredDependent}),
		operationPlanOptions{controlledModules: map[string]struct{}{"acme/provider": {}, "acme/dependent": {}}},
	)
	require.NoError(t, err)
	require.Len(t, ops, 3)
	assert.Equal(t, 1, countPlannerOperation(ops, dependent.ID, regapi.EntryUpdate))
	assert.Equal(t, 0, countPlannerOperation(ops, dependent.ID, regapi.EntryDelete))
	assert.Equal(t, 0, countPlannerOperation(ops, dependent.ID, regapi.EntryCreate))
}

func TestOperationPlanner_KindReplacementReportsUnplannedDependent(t *testing.T) {
	provider := plannerTestEntry("test.provider", "acme/provider", "v1.0.0", "sha256:a", "provider")
	provider.ID = regapi.NewID("test", "provider")
	dependent := plannerTestEntry("test.dependent", "acme/dependent", "v1.0.0", "sha256:b", "dependent")
	dependent.ID = regapi.NewID("test", "dependent")
	dependent.Meta.Set(regapi.TagDependsOn, []string{provider.ID.Name})

	desiredProvider := clonePlannerTestEntry(provider)
	desiredProvider.Kind = "test.provider.v2"
	planner := operationPlanner{resolver: regtop.NewResolver()}

	_, err := planner.plan(
		fixtureState(regapi.State{provider, dependent}),
		fixtureState([]regapi.Entry{desiredProvider}),
		operationPlanOptions{controlledModules: map[string]struct{}{"acme/provider": {}}},
	)
	require.ErrorContains(t, err, "live dependent test:dependent absent from desired state")

	_, err = planner.plan(
		fixtureState(regapi.State{provider, dependent}),
		fixtureState([]regapi.Entry{desiredProvider, dependent}),
		operationPlanOptions{
			controlledModules: map[string]struct{}{"acme/provider": {}, "acme/dependent": {}},
			originalKey:       idKey(dependent.ID),
		},
	)
	require.ErrorContains(t, err, "original operation target test:dependent")
}

func countPlannerOperation(ops []regapi.Operation, id regapi.ID, kind string) int {
	count := 0
	for _, op := range ops {
		if op.Entry.ID == id && op.Kind == kind {
			count++
		}
	}
	return count
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
				currentState := fixtureState(current)
				desiredState := fixtureState(desired)
				ops, err := planner.plan(currentState, desiredState, opts)
				require.NoError(t, err)
				got := applyPlannerOperations(currentState, ops)
				assertPlannerStatesEqual(t, desiredState, got)

				next, err := planner.plan(got, desiredState, opts)
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
			fixtureModuleKey:        module,
			fixtureModuleVersionKey: version,
			fixtureModuleDigestKey:  digest,
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

// applyPlannerOperations applies a plan the way the registry does: the entry
// and the record the operation carries move together.
func applyPlannerOperations(current regapi.ProvenancedState, ops []regapi.Operation) regapi.ProvenancedState {
	entries := make(map[string]regapi.Entry, len(current.Entries)+len(ops))
	records := make(map[string]regapi.EntryProvenance, len(current.Entries)+len(ops))
	for _, entry := range current.Entries {
		entries[idKey(entry.ID)] = entry
		records[idKey(entry.ID)] = current.Provenance[entry.ID]
	}
	for _, op := range ops {
		key := idKey(op.Entry.ID)
		switch op.Kind {
		case regapi.EntryCreate, regapi.EntryUpdate:
			entries[key] = op.Entry
			if op.Provenance != nil {
				records[key] = *op.Provenance
			}
		case regapi.EntryDelete:
			delete(entries, key)
			delete(records, key)
		}
	}
	result := regapi.ProvenancedState{
		Entries:    make(regapi.State, 0, len(entries)),
		Provenance: make(regapi.ProvenanceMap, len(entries)),
	}
	for key, entry := range entries {
		result.Entries = append(result.Entries, entry)
		result.Provenance[entry.ID] = records[key]
	}
	return result
}

func assertPlannerStatesEqual(t *testing.T, want, got regapi.ProvenancedState) {
	t.Helper()
	require.Len(t, got.Entries, len(want.Entries))
	gotByID := entriesByID(got.Entries)
	for _, entry := range want.Entries {
		actual, ok := gotByID[idKey(entry.ID)]
		require.True(t, ok, "missing entry %s", entry.ID)
		assert.Equal(t, entry, actual, "entry %s differs", entry.ID)
		assert.Equal(t, want.Provenance[entry.ID], got.Provenance[entry.ID], "provenance of %s differs", entry.ID)
	}
}
