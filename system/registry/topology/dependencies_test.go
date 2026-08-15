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
