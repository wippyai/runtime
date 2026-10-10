// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

func BenchmarkEnvironmentIncrementalUpdate10000(b *testing.B) {
	resolver := NewResolver()
	for _, path := range []string{"data.*_env", "meta.*_env", "data.imports"} {
		if err := resolver.RegisterPattern(registry.DependencyPattern{Path: path, AllowWildcard: true}); err != nil {
			b.Fatal(err)
		}
	}
	state := make(registry.State, 0, 10000)
	for i := 0; i < 100; i++ {
		state = append(state, registry.Entry{ID: registry.NewID("settings", fmt.Sprintf("key%d", i)), Kind: "env.variable", Data: payload.New(map[string]any{"variable": fmt.Sprintf("KEY%d", i)})})
	}
	for i := 100; i < 10000; i++ {
		state = append(state, registry.Entry{ID: registry.NewID("app", fmt.Sprintf("consumer%d", i)), Data: payload.New(map[string]any{"key_env": fmt.Sprintf("KEY%d", i%100), "nested": map[string]any{"url": "prefix-${env:KEY0}"}})})
	}
	index := BuildDepIndex(state, resolver)
	builder := NewStateBuilder(zap.NewNop(), resolver)
	changes := registry.ChangeSet{{Kind: registry.EntryUpdate, Entry: state[len(state)-1]}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := builder.SortChangeSetWithIndex(state, changes, index); err != nil {
			b.Fatal(err)
		}
	}
}
