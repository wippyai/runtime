// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

func TestResolveDependenciesUsesCanonicalTopologySemantics(t *testing.T) {
	direct := registry.Entry{ID: registry.NewID("app", "direct")}
	grouped := registry.Entry{
		ID:   registry.NewID("other", "grouped"),
		Meta: attrs.NewBagFrom(map[string]any{registry.TagGroups: []string{"workers"}}),
	}
	namespaced := registry.Entry{ID: registry.NewID("services", "api")}
	root := registry.Entry{ID: registry.NewID("", "root")}
	consumer := registry.Entry{
		ID: registry.NewID("app", "consumer"),
		Meta: attrs.NewBagFrom(map[string]any{registry.TagDependsOn: []string{
			"direct", "group:workers", "ns:services", "ns:", "missing",
		}}),
	}
	state := registry.StateMap{
		direct.ID: direct, grouped.ID: grouped, namespaced.ID: namespaced,
		root.ID: root, consumer.ID: consumer,
	}

	resolved := ResolveDependencies(state, nil)
	require.Len(t, resolved[consumer.ID], 3)
	assert.Equal(t, []registry.ID{direct.ID, grouped.ID, namespaced.ID}, resolved[consumer.ID])
}

func TestIndexedCandidateDependenciesUseFreshMembershipAndPreserveCommittedIndex(t *testing.T) {
	consumer := registry.Entry{ID: registry.NewID("app", "consumer"), Meta: attrs.NewBagFrom(map[string]any{
		registry.TagDependsOn: []string{"group:workers", "ns:services"},
	})}
	member := registry.Entry{ID: registry.NewID("services", "member")}
	baseline := registry.State{consumer, member}
	index := BuildDepIndex(baseline, nil)
	member.Meta = attrs.NewBagFrom(map[string]any{registry.TagGroups: []string{"workers"}})
	added := registry.Entry{ID: registry.NewID("other", "added"), Meta: attrs.NewBagFrom(map[string]any{
		registry.TagGroups: []string{"workers"}, registry.TagDependsOn: []string{"app:consumer"},
	})}
	changes := registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: member}, {Kind: registry.EntryCreate, Entry: added}}
	candidate := NewStateMap(registry.State{consumer, member, added})
	indexed := make(map[registry.ID]map[registry.ID]bool)
	require.NoError(t, index.VisitDependencies(candidate, changes, nil, func(source, target registry.ID) error {
		if indexed[source] == nil {
			indexed[source] = map[registry.ID]bool{}
		}
		indexed[source][target] = true
		return nil
	}))
	expected := make(map[registry.ID]map[registry.ID]bool)
	require.NoError(t, VisitDependencies(candidate, nil, func(source, target registry.ID) error {
		if expected[source] == nil {
			expected[source] = map[registry.ID]bool{}
		}
		expected[source][target] = true
		return nil
	}))
	require.Equal(t, expected, indexed)
	dependents := map[registry.ID]struct{}{}
	index.Dependents(consumer, dependents)
	require.Empty(t, dependents, "candidate validation must not commit newly declared dependencies")
}

func BenchmarkResolveDependenciesGroups(b *testing.B) {
	const entries = 2000
	state := make(registry.StateMap, entries)
	for i := 0; i < entries; i++ {
		id := registry.NewID("bench", strconv.Itoa(i))
		group := "partition-" + strconv.Itoa(i/20)
		meta := attrs.NewBagFrom(map[string]any{registry.TagGroups: []string{group}})
		if i%10 == 0 {
			meta[registry.TagDependsOn] = []string{"group:" + group}
		}
		state[id] = registry.Entry{ID: id, Meta: meta}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ResolveDependencies(state, nil)
	}
}
