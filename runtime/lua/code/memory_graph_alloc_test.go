// SPDX-License-Identifier: MPL-2.0

package code

import (
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/registry"
)

// A shared dependency closure, as found in applications with many Lua entries.
func buildSharedGraph(tb testing.TB) (*MemoryGraph, registry.ID) {
	tb.Helper()
	g := NewMemoryGraph()
	root := createTestNode("root")
	if err := g.AddNode(root); err != nil {
		tb.Fatal(err)
	}
	for i := range 128 {
		if err := g.AddNode(createTestNode(fmt.Sprintf("leaf%03d", i))); err != nil {
			tb.Fatal(err)
		}
	}
	for i := range 8 {
		parent := createTestNode(fmt.Sprintf("parent%d", i))
		if err := g.AddNode(parent); err != nil {
			tb.Fatal(err)
		}
		if err := g.AddDependency(root.ID, parent.ID, parent.ID.Name); err != nil {
			tb.Fatal(err)
		}
		for j := range 128 {
			leaf := registry.ID{Name: fmt.Sprintf("leaf%03d", j)}
			if err := g.AddDependency(parent.ID, leaf, leaf.Name); err != nil {
				tb.Fatal(err)
			}
		}
	}
	return g, root.ID
}

func BenchmarkMemoryGraphBuildSharedClosure(b *testing.B) {
	g, root := buildSharedGraph(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := g.Build(root); err != nil {
			b.Fatal(err)
		}
	}
}

func TestMemoryGraphBuildAllocationBudget(t *testing.T) {
	g, root := buildSharedGraph(t)
	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			if _, err := g.Build(root); err != nil {
				b.Fatal(err)
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > 450000 {
		t.Fatalf("shared closure allocated %d bytes; budget is 450000", got)
	}
}
