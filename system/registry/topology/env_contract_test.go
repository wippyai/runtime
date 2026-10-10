// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

func environmentContractResolver(t *testing.T) *Resolver {
	t.Helper()
	r := NewResolver()
	for _, path := range []string{"data.*_env", "meta.*_env", "data.env", "data.imports"} {
		require.NoError(t, r.RegisterPattern(registry.DependencyPattern{Path: path, AllowWildcard: true}))
	}
	return r
}

func environmentContractVariable(id, name string) registry.Entry {
	return registry.Entry{ID: registry.ParseID(id), Kind: "env.variable", Data: payload.New(map[string]any{"variable": name})}
}

func requireEnvironmentContractEdges(t *testing.T, state registry.State, resolver registry.DependencyResolver, consumer registry.ID, want []registry.ID) *DepIndex {
	t.Helper()
	byID := NewStateMap(state)
	require.ElementsMatch(t, want, ResolveDependencies(byID, resolver)[consumer])
	index := BuildDepIndex(state, resolver)
	var indexed []registry.ID
	require.NoError(t, index.VisitDependencies(byID, nil, resolver, func(source, target registry.ID) error {
		if source == consumer {
			indexed = append(indexed, target)
		}
		return nil
	}))
	require.ElementsMatch(t, want, indexed, "full and indexed views must agree")
	for _, entry := range state {
		if entry.ID.Equal(consumer) {
			continue
		}
		dependents := make(map[registry.ID]struct{})
		index.Dependents(entry, dependents)
		expected := false
		for _, id := range want {
			expected = expected || id.Equal(entry.ID)
		}
		_, present := dependents[consumer]
		require.Equal(t, expected, present, "reverse dependencies for %s must match forward edges", entry.ID.String())
	}
	return index
}

func TestEnvironmentContractMetadataAndTypedDeclarations(t *testing.T) {
	for _, provider := range []string{"map", "value", "pointer"} {
		for _, reference := range []string{"data", "meta", "data.env"} {
			t.Run(provider+"/"+reference, func(t *testing.T) {
				variable := environmentContractVariable("settings:key", "KEY")
				value := envapi.Variable{ID: variable.ID, Name: "KEY", StorageID: registry.ParseID("settings:os")}
				switch provider {
				case "value":
					variable.Data = payload.New(value)
				case "pointer":
					variable.Data = payload.New(&value)
				}
				consumer := registry.Entry{ID: registry.ParseID("app:consumer")}
				switch reference {
				case "data":
					consumer.Data = payload.New(map[string]any{"key_env": "KEY"})
				case "meta":
					consumer.Meta = attrs.Bag{"key_env": "KEY"}
				case "data.env":
					consumer.Data = payload.New(map[string]any{"env": "KEY"})
				}
				requireEnvironmentContractEdges(t, registry.State{consumer, variable}, environmentContractResolver(t), consumer.ID, []registry.ID{variable.ID})
			})
		}
	}
}

func TestEnvironmentContractNamePriorityKeepsExplicitRegistryEdges(t *testing.T) {
	for _, reference := range []string{"companion", "placeholder", "shorthand", "metadata"} {
		for _, explicit := range []string{"none", "depends_on", "imports"} {
			t.Run(reference+"/"+explicit, func(t *testing.T) {
				alias := environmentContractVariable("settings:key", "KEY")
				local := environmentContractVariable("app:KEY", "LOCAL")
				consumer := registry.Entry{ID: registry.ParseID("app:consumer"), Meta: attrs.Bag{}}
				switch reference {
				case "companion":
					consumer.Data = payload.New(map[string]any{"key_env": "KEY"})
				case "placeholder":
					consumer.Data = payload.New(map[string]any{"key": "${env:KEY}"})
				case "shorthand":
					consumer.Data = payload.New(map[string]any{"key": "${KEY|default}"})
				case "metadata":
					consumer.Meta["key_env"] = "KEY"
				}
				want := []registry.ID{alias.ID}
				switch explicit {
				case "depends_on":
					consumer.Meta[registry.TagDependsOn] = []string{"KEY"}
					want = append(want, local.ID)
				case "imports":
					data := map[string]any{}
					if consumer.Data != nil {
						data = consumer.Data.Data().(map[string]any)
					}
					data["imports"] = []any{"KEY"}
					consumer.Data = payload.New(data)
					want = append(want, local.ID)
				}
				requireEnvironmentContractEdges(t, registry.State{consumer, local, alias}, environmentContractResolver(t), consumer.ID, want)
			})
		}
	}
}

func TestEnvironmentContractNamePriorityDoesNotCreateFalseCycle(t *testing.T) {
	alias := environmentContractVariable("settings:key", "KEY")
	local := environmentContractVariable("app:KEY", "LOCAL")
	local.Data = payload.New(map[string]any{"variable": "LOCAL", "default": "${env:OTHER}"})
	consumer := environmentContractVariable("app:consumer", "OTHER")
	consumer.Data = payload.New(map[string]any{"variable": "OTHER", "default": "${env:KEY}"})
	resolver := environmentContractResolver(t)
	state := registry.State{local, consumer, alias}
	requireEnvironmentContractEdges(t, state, resolver, consumer.ID, []registry.ID{alias.ID})
	sorted, err := SortEntriesByDependency(state, resolver)
	require.NoError(t, err)
	require.Equal(t, []registry.ID{alias.ID, consumer.ID, local.ID}, []registry.ID{sorted[0].ID, sorted[1].ID, sorted[2].ID})
	consumer.Meta = attrs.Bag{registry.TagDependsOn: []string{"KEY"}}
	_, err = SortEntriesByDependency(registry.State{local, consumer, alias}, resolver)
	require.Error(t, err, "a real explicit registry cycle must not be hidden by an environment alias")
}

func TestEnvironmentContractCandidateShadowingIsNotCommittedEarly(t *testing.T) {
	local := environmentContractVariable("app:KEY", "LOCAL")
	consumer := registry.Entry{ID: registry.ParseID("app:consumer"), Data: payload.New(map[string]any{"key_env": "KEY"})}
	resolver := environmentContractResolver(t)
	baseline := registry.State{local, consumer}
	index := requireEnvironmentContractEdges(t, baseline, resolver, consumer.ID, []registry.ID{local.ID})
	alias := environmentContractVariable("settings:key", "KEY")
	candidate := NewStateMap(append(append(registry.State(nil), baseline...), alias))
	changes := registry.ChangeSet{{Kind: registry.EntryCreate, Entry: alias}}
	var targets []registry.ID
	require.NoError(t, index.VisitDependencies(candidate, changes, resolver, func(source, target registry.ID) error {
		if source == consumer.ID {
			targets = append(targets, target)
		}
		return nil
	}))
	require.Equal(t, []registry.ID{alias.ID}, targets)
	dependents := map[registry.ID]struct{}{}
	index.Dependents(local, dependents)
	require.Contains(t, dependents, consumer.ID, "uncommitted candidate cannot alter live name selection")
	index.Patch(changes, resolver)
	dependents = map[registry.ID]struct{}{}
	index.Dependents(local, dependents)
	require.NotContains(t, dependents, consumer.ID)
	dependents = map[registry.ID]struct{}{}
	index.Dependents(alias, dependents)
	require.Contains(t, dependents, consumer.ID)
	index.Patch(registry.ChangeSet{{Kind: registry.EntryDelete, Entry: alias}}, resolver)
	dependents = map[registry.ID]struct{}{}
	index.Dependents(local, dependents)
	require.Contains(t, dependents, consumer.ID, "removing the alias restores ID fallback")
}

func TestEnvironmentContractSortUsesResidentAliasOutsideBatch(t *testing.T) {
	alias := environmentContractVariable("settings:key", "KEY")
	local := environmentContractVariable("app:KEY", "LOCAL")
	local.Data = payload.New(map[string]any{"variable": "LOCAL", "default": "${env:OTHER}"})
	consumer := environmentContractVariable("app:consumer", "OTHER")
	consumer.Data = payload.New(map[string]any{"variable": "OTHER", "default": "${env:KEY}"})
	resolver := environmentContractResolver(t)
	baseline := registry.State{alias, local, consumer}
	changes := registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: local}, {Kind: registry.EntryUpdate, Entry: consumer}}
	builder := NewStateBuilder(zap.NewNop(), resolver)
	for _, index := range []*DepIndex{nil, BuildDepIndex(baseline, resolver)} {
		sorted, err := builder.SortChangeSetWithIndex(baseline, changes, index)
		require.NoError(t, err)
		require.Equal(t, consumer.ID, sorted[0].Entry.ID)
		require.Equal(t, local.ID, sorted[1].Entry.ID)
	}
}

func TestEnvironmentContractFallbacksAndSelfReferences(t *testing.T) {
	local := environmentContractVariable("app:key", "LOCAL")
	local.Meta = attrs.Bag{registry.TagGroups: []string{"keys"}}
	self := environmentContractVariable("app:self", "SELF")
	self.Data = payload.New(map[string]any{"variable": "SELF", "default": "${env:SELF}"})
	consumer := registry.Entry{ID: registry.ParseID("app:consumer")}
	resolver := environmentContractResolver(t)
	for _, reference := range []string{"key", "app:key", "group:keys", "ns:app"} {
		t.Run(reference, func(t *testing.T) {
			consumer.Data = payload.New(map[string]any{"nested": []any{map[string]any{"key_env": reference}}})
			want := []registry.ID{local.ID}
			if reference == "ns:app" {
				want = append(want, self.ID)
			}
			index := requireEnvironmentContractEdges(t, registry.State{local, self, consumer}, resolver, consumer.ID, want)
			dependents := map[registry.ID]struct{}{}
			index.Dependents(consumer, dependents)
			require.NotContains(t, dependents, consumer.ID)
			index.Dependents(self, dependents)
			require.NotContains(t, dependents, self.ID)
		})
	}
}

func TestEnvironmentContractDuplicateNamesRemainConservative(t *testing.T) {
	first := environmentContractVariable("settings:first", "KEY")
	second := environmentContractVariable("settings:second", "KEY")
	local := environmentContractVariable("app:KEY", "LOCAL")
	consumer := registry.Entry{ID: registry.ParseID("app:consumer"), Data: payload.New(map[string]any{"key_env": "KEY"})}
	// The first registrant owns the runtime shortcut. Topology does not know
	// registration history, so it orders after every possible named provider.
	index := requireEnvironmentContractEdges(t, registry.State{local, first, second, consumer}, nil, consumer.ID, []registry.ID{first.ID, second.ID})
	index.OnDelete(first)
	dependents := map[registry.ID]struct{}{}
	index.Dependents(local, dependents)
	require.NotContains(t, dependents, consumer.ID, "one remaining declaration still shadows the ID fallback")
}

func TestEnvironmentContractReplacementKeepsNewProvider(t *testing.T) {
	old := environmentContractVariable("settings:key", "OLD")
	newProvider := environmentContractVariable("settings:key", "KEY")
	consumer := registry.Entry{ID: registry.ParseID("app:consumer"), Data: payload.New(map[string]any{"key_env": "KEY"})}
	baseline := registry.State{old}
	changes := registry.ChangeSet{
		{Kind: registry.EntryCreate, Entry: consumer},
		{Kind: registry.EntryCreate, Entry: newProvider},
		{Kind: registry.EntryDelete, Entry: old},
	}
	resolver := environmentContractResolver(t)
	builder := NewStateBuilder(zap.NewNop(), resolver)
	for _, index := range []*DepIndex{nil, BuildDepIndex(baseline, resolver)} {
		sorted, err := builder.SortChangeSetWithIndex(baseline, changes, index)
		require.NoError(t, err)
		require.Equal(t, registry.EntryDelete, sorted[0].Kind)
		require.Equal(t, newProvider.ID, sorted[1].Entry.ID)
		require.Equal(t, consumer.ID, sorted[2].Entry.ID)
	}
}

func TestEnvironmentContractAliasRemovalOrdersExplicitFallbackBeforeConsumer(t *testing.T) {
	alias := environmentContractVariable("settings:key", "KEY")
	local := environmentContractVariable("app:KEY", "LOCAL")
	consumer := registry.Entry{ID: registry.ParseID("app:aaa"), Data: payload.New(map[string]any{"key_env": "KEY"})}
	baseline := registry.State{alias, local, consumer}
	// Move the consumer explicitly before deleting its live provider. Leaving
	// KEY unchanged would still resolve to the alias during the update event.
	next := consumer
	next.Data = payload.New(map[string]any{"key_env": "app:KEY"})
	changes := registry.ChangeSet{
		{Kind: registry.EntryDelete, Entry: alias},
		{Kind: registry.EntryUpdate, Entry: local},
		{Kind: registry.EntryUpdate, Entry: next},
	}
	resolver := environmentContractResolver(t)
	builder := NewStateBuilder(zap.NewNop(), resolver)
	sorted, err := builder.SortChangeSetWithIndex(baseline, changes, BuildDepIndex(baseline, resolver))
	require.NoError(t, err)
	require.Equal(t, local.ID, sorted[0].Entry.ID)
	require.Equal(t, consumer.ID, sorted[1].Entry.ID)
	require.Equal(t, alias.ID, sorted[2].Entry.ID)
	assertAppliesWithoutIncomingDependencyDelete(t, builder, baseline, sorted)
}

func TestEnvironmentContractPublicExtractionAndEscaping(t *testing.T) {
	resolver := NewResolver()
	consumer := registry.Entry{ID: registry.ParseID("app:consumer"),
		Data: payload.New(map[string]any{"key_env": "KEY", "literal": "$${env:IGNORED}", "values": []string{"${KEY|fallback}", "${env:KEY}"}}),
		Meta: attrs.Bag{"description": "${env:IGNORED}"}}
	require.Equal(t, []string{"KEY"}, resolver.Extract(consumer), "public extraction retains placeholder semantics")
	consumer.Data = payload.New(map[string]any{"key_env": "KEY"})
	require.Empty(t, resolver.Extract(consumer), "unregistered companion fields remain topology-only")
	alias := environmentContractVariable("settings:key", "KEY")
	ignored := environmentContractVariable("settings:ignored", "IGNORED")
	requireEnvironmentContractEdges(t, registry.State{consumer, alias, ignored}, resolver, consumer.ID, []registry.ID{alias.ID})
	consumer.Data = payload.New(map[string]any{"literal": "$${env:IGNORED}", "key_env": ""})
	requireEnvironmentContractEdges(t, registry.State{consumer, alias, ignored}, resolver, consumer.ID, nil)
}

type environmentContractCustomResolver struct{}

func (environmentContractCustomResolver) Extract(registry.Entry) []string { return []string{"KEY"} }
func (environmentContractCustomResolver) RegisterPattern(registry.DependencyPattern) error {
	return nil
}

func TestEnvironmentContractCustomResolverRetainsExplicitEdges(t *testing.T) {
	alias := environmentContractVariable("settings:key", "KEY")
	local := environmentContractVariable("app:KEY", "LOCAL")
	consumer := registry.Entry{ID: registry.ParseID("app:consumer"), Data: payload.New(map[string]any{"key_env": "KEY"})}
	index := requireEnvironmentContractEdges(t, registry.State{local, alias, consumer}, environmentContractCustomResolver{}, consumer.ID, []registry.ID{alias.ID, local.ID})
	dependents := map[registry.ID]struct{}{alias.ID: {}}
	index.Dependents(alias, dependents)
	require.Contains(t, dependents, alias.ID, "lookup must preserve results accumulated for another target")
}

func TestEnvironmentContractExtractionDoesNotMutateDependsOn(t *testing.T) {
	backing := []string{"app:database", "sentinel", "sentinel"}
	entry := registry.Entry{ID: registry.ParseID("app:consumer"),
		Meta: attrs.Bag{registry.TagDependsOn: backing[:1]},
		Data: payload.New(map[string]any{"imports": []string{"app:library"}})}
	index := BuildDepIndex(registry.State{entry}, environmentContractResolver(t))
	require.NotNil(t, index)
	require.Equal(t, []string{"app:database", "sentinel", "sentinel"}, backing)
}

func TestEnvironmentContractIncrementalMutationParity(t *testing.T) {
	resolver := environmentContractResolver(t)
	state := NewStateMap(nil)
	index := BuildDepIndex(nil, resolver)
	rng := rand.New(rand.NewSource(950))
	for step := 0; step < 150; step++ {
		id := registry.NewID("app", fmt.Sprintf("entry%d", rng.Intn(16)))
		entry := registry.Entry{ID: id, Kind: "test.entry", Meta: attrs.Bag{registry.TagGroups: []string{"keys"}}}
		if rng.Intn(3) == 0 {
			entry.Kind = "env.variable"
			entry.Data = payload.New(map[string]any{"variable": fmt.Sprintf("KEY%d", rng.Intn(4))})
		} else {
			entry.Data = payload.New(map[string]any{"key_env": fmt.Sprintf("KEY%d", rng.Intn(4)), "nested": map[string]any{"other_env": "group:keys"}})
			entry.Meta[registry.TagDependsOn] = []string{"entry0"}
		}
		previous, exists := state[id]
		op := registry.Operation{Kind: registry.EntryCreate, Entry: entry}
		if exists {
			op.Kind, op.OriginalEntry = registry.EntryUpdate, &previous
			if rng.Intn(4) == 0 {
				op.Kind = registry.EntryDelete
			}
		}
		candidate := NewStateMap(StateMapToSlice(state))
		if op.Kind == registry.EntryDelete {
			delete(candidate, id)
		} else {
			candidate[id] = entry
		}
		changes := registry.ChangeSet{op}
		want := ResolveDependencies(candidate, resolver)
		got := make(map[registry.ID][]registry.ID)
		require.NoError(t, index.VisitDependencies(candidate, changes, resolver, func(source, target registry.ID) error {
			got[source] = append(got[source], target)
			return nil
		}))
		for source := range candidate {
			require.ElementsMatch(t, want[source], got[source], "step %d candidate %s", step, source.String())
		}
		index.Patch(changes, resolver)
		state = candidate
		for target, entry := range state {
			dependents := map[registry.ID]struct{}{}
			index.Dependents(entry, dependents)
			for source := range state {
				expected := false
				for _, dependency := range want[source] {
					expected = expected || dependency == target
				}
				_, actual := dependents[source]
				require.Equal(t, expected, actual, "step %d reverse %s -> %s", step, source.String(), target.String())
			}
		}
	}
}
