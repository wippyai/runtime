// SPDX-License-Identifier: MPL-2.0

package security

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	sec "github.com/wippyai/runtime/api/security"
	cfg "github.com/wippyai/runtime/api/service/security/policy"
	impl "github.com/wippyai/runtime/service/security/policy"
)

var shapedHotActions = []string{"funcs.call", "registry.get", "registry.find", "contract.get", "contract.open", "contract.call", "funcs.security", "process.spawn", "registry.list", "events.publish"}

func shapeHistogram(hist map[int]int) []int {
	var values []int
	for value, count := range hist {
		for i := 0; i < count; i++ {
			values = append(values, value)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(values)))
	return values
}

// Synthetic names preserve action/resource distributions without application identifiers.
func shapedPolicies(tb testing.TB, n int) ([]sec.Policy, []sec.Policy) {
	tb.Helper()
	actions := shapeHistogram(map[int]int{2: 66, 9: 81, 1: 233, 3: 14, 7: 22, 4: 3, 11: 1, 8: 2, 6: 4, 12: 1, 10: 1, 5: 2})
	exact := shapeHistogram(map[int]int{0: 232, 1: 105, 4: 5, 2: 31, 12: 2, 47: 1, 8: 4, 6: 4, 10: 2, 3: 23, 5: 7, 9: 1, 11: 2, 7: 2, 14: 1, 21: 1, 29: 1, 16: 2, 13: 2, 15: 1, 30: 1})
	prefixes := shapeHistogram(map[int]int{2: 41, 1: 26, 0: 274, 11: 5, 8: 10, 6: 15, 3: 10, 15: 3, 9: 6, 10: 4, 4: 9, 7: 15, 13: 5, 18: 1, 16: 2, 5: 1, 20: 2, 14: 1})
	list, oracle := make([]sec.Policy, n), make([]sec.Policy, n)
	for i := range list {
		k := i * 430 / n
		names := make([]string, actions[k])
		for j := range names {
			if k < 150 {
				names[j] = shapedHotActions[(i+j)%len(shapedHotActions)]
			} else {
				names[j] = fmt.Sprintf("action.%02d", (i+j)%86)
			}
		}
		if k >= 300 && k < 311 {
			names[0] = "*"
		}
		var resources []string
		for j := 0; j < exact[(k+279)%430]; j++ {
			resource := fmt.Sprintf("entry:%04d:resource:%04d:exact-valuexx", i, j)
			if k == 151 && j == 0 {
				resource = ""
			}
			if k == 151 && j == 1 {
				resource += strings.Repeat("z", 60-len(resource))
			}
			resources = append(resources, resource)
		}
		for j := 0; j < prefixes[k]; j++ {
			prefix := fmt.Sprintf("entry:%04d:prefix:%04d:x", i, j)
			if i == 0 && j == 0 {
				prefix = "short"
			}
			if i == 0 && j == 1 {
				prefix += strings.Repeat("z", 46-len(prefix))
			}
			resources = append(resources, prefix+"*")
		}
		if k >= 349 {
			resources = append(resources, "*")
		}
		effect, decision := cfg.Allow, sec.Allow
		if k < 4 {
			effect, decision = cfg.Deny, sec.Deny
		}
		var conditions []cfg.Condition
		if k >= 408 && k < 419 {
			conditions = []cfg.Condition{{Field: "meta.enabled", Operator: "eq", Value: true}}
		}
		definition := cfg.Definition{Actions: shapeAny(names), Resources: shapeAny(resources), Effect: effect, Conditions: conditions}
		id := registry.NewID("shape", fmt.Sprint(i))
		p, err := impl.NewPolicy(id, &cfg.Config{Policy: definition})
		if err != nil {
			tb.Fatal(err)
		}
		list[i] = p
		evaluator, err := impl.NewConditionEvaluator(conditions)
		if err != nil {
			tb.Fatal(err)
		}
		oracle[i] = &referencePolicy{id: id, actions: names, resources: resources, effect: decision, conditions: conditions, evaluator: evaluator}
	}
	return list, oracle
}

func BenchmarkShapedPermissions(b *testing.B) {
	for _, n := range []int{430, 700, 1024, 860, 1720} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			policies, _ := shapedPolicies(b, n)
			s := NewScope(policies)
			resources := make([]string, 5400)
			for i := range resources {
				resources[i] = fmt.Sprintf("entry:%04d:prefix:%04d:xchild", i%n, i%20)
			}
			for _, action := range shapedHotActions {
				b.Run("check/"+action, func(b *testing.B) {
					s.Evaluate(sec.Actor{}, action, resources[0], nil)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						benchmarkResult = s.Evaluate(sec.Actor{}, action, resources[i%len(resources)], nil)
					}
				})
			}
			for _, size := range []int{1500, 5400} {
				b.Run(fmt.Sprintf("scan/%d", size), func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						for _, resource := range resources[:size] {
							benchmarkResult = s.Evaluate(sec.Actor{}, "registry.get", resource, nil)
						}
					}
				})
			}
			b.Run("cold", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					fresh := freshShapedScope(policies)
					benchmarkResult = fresh.Evaluate(sec.Actor{}, "registry.get", "missing", nil)
				}
			})
			b.Run("retained", func(b *testing.B) {
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				scopes := make([]sec.Scope, b.N)
				b.ResetTimer()
				for i := range scopes {
					scopes[i] = freshShapedScope(policies)
					for _, action := range append(append([]string(nil), shapedHotActions...), shapeOtherActions()...) {
						scopes[i].Evaluate(sec.Actor{}, action, "missing", nil)
					}
				}
				b.StopTimer()
				runtime.GC()
				runtime.ReadMemStats(&after)
				if after.HeapAlloc > before.HeapAlloc {
					b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/float64(b.N), "retained-B/segment")
				}
				runtime.KeepAlive(scopes)
				runtime.KeepAlive(policies)
			})
		})
	}
}

func shapeOtherActions() []string {
	names := make([]string, 86)
	for i := range names {
		names[i] = fmt.Sprintf("action.%02d", i)
	}
	return names
}

func TestShapedFixtureDistribution(t *testing.T) {
	list, oracle := shapedPolicies(t, 430)
	if len(list) != 430 {
		t.Fatal("policy count")
	}
	var exact, prefix, anyCount int
	var exactLengths, prefixLengths []int
	names := make(map[string]struct{})
	for _, p := range oracle {
		for _, name := range p.(*referencePolicy).actions.([]string) {
			if name != "*" {
				names[name] = struct{}{}
			}
		}
		for _, r := range p.(*referencePolicy).resources.([]string) {
			if strings.HasSuffix(r, "*") && r != "*" {
				prefix++
				prefixLengths = append(prefixLengths, len(r)-1)
			} else if r != "*" {
				exact++
				exactLengths = append(exactLengths, len(r))
			} else {
				anyCount++
			}
		}
	}
	sort.Ints(exactLengths)
	sort.Ints(prefixLengths)
	if len(names) != 96 || anyCount != 81 {
		t.Fatalf("action/any distributions: %d/%d", len(names), anyCount)
	}
	if exactLengths[0] != 0 || exactLengths[len(exactLengths)/2] != 38 || exactLengths[len(exactLengths)-1] != 60 || prefixLengths[0] != 5 || prefixLengths[len(prefixLengths)/2] != 24 || prefixLengths[len(prefixLengths)-1] != 46 {
		t.Fatal("resource length distributions")
	}
	if exact != 650 || prefix != 817 {
		t.Fatalf("resource distributions: exact=%d prefix=%d", exact, prefix)
	}
}

func shapeAny(values []string) []any {
	result := make([]any, len(values))
	for i, v := range values {
		result[i] = v
	}
	return result
}
