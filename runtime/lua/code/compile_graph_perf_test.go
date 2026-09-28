// SPDX-License-Identifier: MPL-2.0

package code

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/wippyai/go-lua/compiler/bytecode"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"go.uber.org/zap"
)

// This mirrors registry loading: each new entry is compiled while the graph grows.
func BenchmarkSequentialLuaCompiles(b *testing.B) {
	for _, size := range []int{100, 200, 400} {
		b.Run(fmt.Sprintf("N%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cm, err := NewCodeManager(zap.NewNop(), &testEventBus{}, Config{})
				if err != nil {
					b.Fatal(err)
				}
				base := registry.NewID("bench", "shared")
				if err := cm.AddNode(context.Background(), Node{ID: base, Kind: api.Function, Source: `return 1`, Method: "main"}, nil); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				for n := 0; n < size; n++ {
					id := registry.NewID("bench", fmt.Sprintf("entry_%04d", n))
					if err := cm.AddNode(context.Background(), Node{ID: id, Kind: api.Function, Source: `return shared`, Method: "main"}, []Import{{ID: base, Alias: "shared"}}); err != nil {
						b.Fatal(err)
					}
					if _, err := cm.Compile(id, nil); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
			}
		})
	}
}

func TestCompileMatchesFullGraphSnapshot(t *testing.T) {
	cm, err := NewCodeManager(zap.NewNop(), &testEventBus{}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	nodes := []struct {
		name, source string
		deps         []Import
	}{
		{"leaf", `return 41`, nil},
		{"other", `return 2`, nil},
		{"left", `return leaf`, []Import{{ID: registry.NewID("equiv", "leaf"), Alias: "leaf"}}},
		{"right", `return leaf`, []Import{{ID: registry.NewID("equiv", "leaf"), Alias: "alias_leaf"}}},
		{"main", `return left + right`, []Import{{ID: registry.NewID("equiv", "left"), Alias: "left"}, {ID: registry.NewID("equiv", "right"), Alias: "right"}}},
		{"bridge", `return right`, []Import{{ID: registry.NewID("equiv", "right"), Alias: "right"}}},
		{"outside", `return other`, []Import{{ID: registry.NewID("equiv", "other"), Alias: "other"}, {ID: registry.NewID("equiv", "bridge"), Alias: "bridge"}}},
	}
	for _, n := range nodes {
		if err := cm.AddNode(context.Background(), Node{ID: registry.NewID("equiv", n.name), Kind: api.Function, Source: n.source, Method: "main"}, n.deps); err != nil {
			t.Fatal(err)
		}
	}
	id := registry.NewID("equiv", "main")
	options := NewBuildOptions().WithPreloaded(Preload{Name: "extra", ModuleID: registry.NewID("equiv", "other")})
	baseline, err := cm.compiler.Compile(cm.memGraph.Snapshot(), id, options)
	if err != nil {
		t.Fatal(err)
	}
	cm.compiler.Invalidate([]registry.ID{id, registry.NewID("equiv", "leaf"), registry.NewID("equiv", "left"), registry.NewID("equiv", "right")})
	actual, err := cm.Compile(id, options)
	if err != nil {
		t.Fatal(err)
	}
	normalizeImports := func(imports map[registry.ID][]Import) map[registry.ID][]Import {
		copy := make(map[registry.ID][]Import, len(imports))
		for id, entries := range imports {
			copy[id] = append([]Import(nil), entries...)
			sort.Slice(copy[id], func(i, j int) bool {
				if copy[id][i].Alias != copy[id][j].Alias {
					return copy[id][i].Alias < copy[id][j].Alias
				}
				return copy[id][i].ID.String() < copy[id][j].ID.String()
			})
		}
		return copy
	}
	if !reflect.DeepEqual(normalizeImports(baseline.Imports), normalizeImports(actual.Imports)) {
		t.Fatalf("imports differ: baseline=%v actual=%v", baseline.Imports, actual.Imports)
	}
	if len(baseline.Dependencies) != len(actual.Dependencies) {
		t.Fatalf("dependency count differs: %d != %d", len(baseline.Dependencies), len(actual.Dependencies))
	}
	if len(actual.Preloaded) != 1 || actual.Preloaded[0].Node.ID != registry.NewID("equiv", "other") {
		t.Fatalf("preloaded nodes differ: %v", actual.Preloaded)
	}
	wantOrder := []string{"alias_leaf", "leaf", "right", "left"}
	actualOrder := make([]string, 0, len(actual.Dependencies))
	for _, dep := range actual.Dependencies {
		actualOrder = append(actualOrder, dep.Name)
	}
	if !reflect.DeepEqual(actualOrder, wantOrder) {
		t.Fatalf("dependency resolution order = %v, want %v", actualOrder, wantOrder)
	}
	checkProto := func(name string, want, got []byte) {
		t.Helper()
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s bytecode differs", name)
		}
	}
	wantMain, err := bytecode.Dump(baseline.Main)
	if err != nil {
		t.Fatal(err)
	}
	gotMain, err := bytecode.Dump(actual.Main)
	if err != nil {
		t.Fatal(err)
	}
	checkProto("main", wantMain, gotMain)
	for i := range baseline.Dependencies {
		want := baseline.Dependencies[i]
		got := actual.Dependencies[i]
		if want.Node.ID != got.Node.ID || want.Name != got.Name {
			t.Errorf("dependency %d differs: %s/%s != %s/%s", i, want.Node.ID, want.Name, got.Node.ID, got.Name)
		}
		wantBytes, err := bytecode.Dump(want.Proto)
		if err != nil {
			t.Fatal(err)
		}
		gotBytes, err := bytecode.Dump(got.Proto)
		if err != nil {
			t.Fatal(err)
		}
		checkProto(want.Name, wantBytes, gotBytes)
	}
}

func TestIncrementalLevelsMatchWholeGraphAfterMutations(t *testing.T) {
	g := NewMemoryGraph()
	ids := make(map[string]registry.ID)
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		id := registry.NewID("levels", name)
		ids[name] = id
		if err := g.AddNode(&Node{ID: id, Kind: api.Function}); err != nil {
			t.Fatal(err)
		}
	}
	check := func() {
		t.Helper()
		full, err := g.graph.DependencyLevels()
		if err != nil {
			t.Fatal(err)
		}
		for level, nodes := range full.AllLevels() {
			for _, id := range nodes {
				if g.nodeLevels[id] != level {
					t.Errorf("%s: incremental level %d, full graph level %d", id, g.nodeLevels[id], level)
				}
			}
		}
	}
	for _, edge := range [][2]string{{"a", "b"}, {"a", "c"}, {"c", "d"}, {"e", "c"}, {"f", "e"}} {
		if err := g.AddDependency(ids[edge[0]], ids[edge[1]], ""); err != nil {
			t.Fatal(err)
		}
		check()
	}
	if err := g.RemoveDependency(ids["f"], ids["e"]); err != nil {
		t.Fatal(err)
	}
	check()
	if err := g.UpdateNode(&Node{ID: ids["c"], Kind: api.Function}, nil); err != nil {
		t.Fatal(err)
	}
	check()
	if err := g.RemoveNode(ids["f"]); err != nil {
		t.Fatal(err)
	}
	check()
}
