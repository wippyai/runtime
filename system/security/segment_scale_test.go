// SPDX-License-Identifier: MPL-2.0

package security

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	sec "github.com/wippyai/runtime/api/security"
	cfg "github.com/wippyai/runtime/api/service/security/policy"
	impl "github.com/wippyai/runtime/service/security/policy"
)

func boundaryFixture(tb testing.TB, base sec.Scope, policies []sec.Policy) (context.Context, *sec.Config) {
	tb.Helper()
	reg := NewPolicyRegistry(nil, nil)
	ids := make([]registry.ID, len(policies))
	for i, p := range policies {
		ids[i] = p.ID()
		reg.policies.Store(ids[i], &sec.PolicyEntry{Policy: p})
	}
	ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	tb.Cleanup(func() { ctxapi.ReleaseFrameContext(frame) })
	ctx = sec.WithRegistry(ctx, reg)
	require.NoError(tb, sec.SetScope(ctx, base))
	return ctx, &sec.Config{Policies: ids}
}

func TestResidentListHitAllocatesNothing(t *testing.T) {
	policies, _ := countPolicies(600, false)
	for _, duplicate := range []bool{false, true} {
		list := append([]sec.Policy(nil), policies...)
		if duplicate {
			list = append(list, policies...)
			list = append(list, policies[0])
		}
		resident := NewScope(list)
		require.Zero(t, testing.AllocsPerRun(100, func() { benchmarkScope = NewScope(list) }))
		require.True(t, resident == benchmarkScope)
	}
}

func TestAdditionsDoNotReadExistingPolicies(t *testing.T) {
	for _, boundary := range []bool{false, true} {
		t.Run(fmt.Sprint(boundary), func(t *testing.T) {
			policies, counts := countPolicies(600, false)
			base := NewScope(policies)
			added := nativePolicy(t, "added", "read", "*", cfg.Allow)
			ctx, config := boundaryFixture(t, base, []sec.Policy{added})
			for _, p := range counts {
				p.ids.Store(0)
				p.matches.Store(0)
			}
			for i := 0; i < 100; i++ {
				if boundary {
					_, err := ResolveConfigPairs(ctx, config)
					require.NoError(t, err)
				} else {
					base.With(added)
				}
			}
			for _, p := range counts {
				require.Zero(t, p.ids.Load())
				require.Zero(t, p.matches.Load())
			}
			require.True(t, base == base.With(policies[42]), "identical addition returns the receiver")
		})
	}
}

func TestSharedPlansSurviveDistinctScopes(t *testing.T) {
	policies, counts := countPolicies(500, false)
	base := NewScope(policies)
	require.Equal(t, sec.Allow, base.Evaluate(sec.Actor{}, "0", "entry", nil))
	for _, p := range counts {
		p.matches.Store(0)
		p.calls.Store(0)
	}
	for i := 0; i < 3000; i++ {
		added := newMockPolicy(fmt.Sprintf("restriction-%d", i), sec.Undefined)
		next := base.With(added)
		require.Equal(t, sec.Allow, next.Evaluate(sec.Actor{ID: fmt.Sprint(i)}, "0", "entry", nil))
	}
	for _, p := range counts {
		require.Zero(t, p.matches.Load())
		require.Zero(t, p.calls.Load())
	}
}

func TestPartitionJoinAndReplacement(t *testing.T) {
	rng := rand.New(rand.NewSource(31745))
	for trial := 0; trial < 120; trial++ {
		var flat []sec.Policy
		s := NewScope(nil)
		for group := 0; group < 2+rng.Intn(12); group++ {
			var list []sec.Policy
			for i := 0; i < 1+rng.Intn(7); i++ {
				var p sec.Policy
				if len(flat) > 0 && rng.Intn(4) == 0 {
					p = flat[rng.Intn(len(flat))]
				} else {
					effect := cfg.Allow
					if rng.Intn(3) == 0 {
						effect = cfg.Deny
					}
					id := registry.NewID("test", fmt.Sprint(rng.Intn(25)))
					actions := []any{"read", fmt.Sprint(rng.Intn(4))}
					resources := []any{"entry", fmt.Sprintf("prefix%d.*", rng.Intn(3))}
					var err error
					switch rng.Intn(3) {
					case 0:
						p, err = impl.NewExprPolicy(id, &cfg.ExprConfig{Policy: cfg.ExprDefinition{Actions: actions, Resources: resources, Effect: effect, Expression: `actor.ID == meta.owner`}})
					case 1:
						p, err = impl.NewPolicy(id, &cfg.Config{Policy: cfg.Definition{Actions: actions, Resources: resources, Effect: effect, Conditions: []cfg.Condition{{Field: "meta.number", Operator: "gt", Value: 0}}}})
					default:
						p = nativePolicy(t, id.Name, actions, resources, effect)
					}
					require.NoError(t, err)
				}
				list = append(list, p)
				flat = append(flat, p)
			}
			// Every partition crosses the real function boundary, including same-object
			// overlaps and replacements under the same ID.
			ctx, config := boundaryFixture(t, s, list)
			result := WithSecurityConfig(ctx, config)
			var ok bool
			s, ok = sec.GetScope(result)
			require.True(t, ok)
			for _, action := range []string{"read", "0", "missing"} {
				for _, resource := range []string{"entry", "prefix1suffix", "missing"} {
					for _, meta := range []attrs.Bag{nil, {"owner": "a", "number": 1}, {"owner": "b", "number": "bad"}} {
						actor := sec.Actor{ID: "a"}
						require.Equal(t, referenceScope(flat, actor, action, resource, meta), s.Evaluate(actor, action, resource, meta))
					}
				}
			}
		}
		require.Len(t, s.Policies(), len(uniquePolicyIDs(flat)))
		for _, p := range flat {
			require.True(t, s.Contains(p.ID()))
		}
		removed := flat[rng.Intn(len(flat))].ID()
		without := s.Without(removed)
		require.False(t, without.Contains(removed))
		var remaining []sec.Policy
		for _, p := range flat {
			if p.ID() != removed {
				remaining = append(remaining, p)
			}
		}
		require.Equal(t, referenceScope(remaining, sec.Actor{}, "read", "entry", nil), without.Evaluate(sec.Actor{}, "read", "entry", nil))
	}
}
func uniquePolicyIDs(list []sec.Policy) map[registry.ID]bool {
	ids := map[registry.ID]bool{}
	for _, p := range list {
		ids[p.ID()] = true
	}
	return ids
}

func TestWithChainCompactionAndConcurrentMerges(t *testing.T) {
	policies, _ := countPolicies(96, false)
	s := NewScope(nil)
	for i, p := range policies {
		s = s.With(p)
		require.Equal(t, referenceScope(policies[:i+1], sec.Actor{}, "0", "entry", nil), s.Evaluate(sec.Actor{}, "0", "entry", nil))
	}
	require.Len(t, s.Policies(), 96)
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				next := s.With(policies[i%len(policies)]).Without(policies[(i+1)%len(policies)].ID())
				if len(next.Policies()) != 95 {
					t.Error("concurrent merge loses policies")
				}
			}
		}()
	}
	wg.Wait()
}

func benchmarkDeclaration(b *testing.B, name string) sec.Policy {
	b.Helper()
	p, err := impl.NewPolicy(registry.NewID("declaration", name), &cfg.Config{Policy: cfg.Definition{Actions: "function.call", Resources: "*", Effect: cfg.Allow}})
	if err != nil {
		b.Fatal(err)
	}
	return p
}
func BenchmarkSegmentParallel(b *testing.B) {
	policies := benchmarkPolicies(b, 600, "static")
	base := NewScope(policies)
	ctx, config := boundaryFixture(b, base, []sec.Policy{benchmarkDeclaration(b, "parallel")})
	base.Evaluate(sec.Actor{}, "registry.get", "entry:42", nil)
	for _, kind := range []string{"check", "list", "boundary"} {
		b.Run(kind, func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					switch kind {
					case "check":
						base.Evaluate(sec.Actor{}, "registry.get", "entry:42", nil)
					case "list":
						NewScope(policies)
					case "boundary":
						if _, err := ResolveConfigPairs(ctx, config); err != nil {
							b.Error(err)
						}
					}
				}
			})
		})
	}
}

func BenchmarkSegmentBoundarySize(b *testing.B) {
	added := benchmarkDeclaration(b, "boundary")
	for _, n := range []int{10, 400, 600, 1024} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			base := NewScope(benchmarkPolicies(b, n, "static"))
			ctx, config := boundaryFixture(b, base, []sec.Policy{added})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := ResolveConfigPairs(ctx, config); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSegmentPopulations(b *testing.B) {
	policies := benchmarkPolicies(b, 600, "mixed")
	groups := make([]sec.Scope, 1024)
	for mask := range groups {
		var list []sec.Policy
		for group := 0; group < 10; group++ {
			if mask&(1<<group) != 0 {
				list = append(list, policies[group*60:(group+1)*60]...)
			}
		}
		groups[mask] = NewScope(list)
		groups[mask].Evaluate(sec.Actor{}, "registry.get", "entry:42", nil)
	}
	declarations := make([]sec.Policy, 64)
	for i := range declarations {
		declarations[i] = benchmarkDeclaration(b, fmt.Sprint(i))
	}
	for _, kind := range []string{"identical-users", "combinations", "unique-restriction"} {
		b.Run(kind, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s := groups[1023]
				if kind == "identical-users" {
					s = NewScope(policies)
				}
				switch kind {
				case "combinations":
					s = groups[i%1024].With(declarations[(i/1024)%64])
				case "unique-restriction":
					s = s.With(newMockPolicy(fmt.Sprint(i), sec.Undefined))
				}
				benchmarkScope = s
				benchmarkResult = s.Evaluate(sec.Actor{}, "registry.get", "entry:42", nil)
			}
		})
	}
}

func BenchmarkSegmentRetainedPopulations(b *testing.B) {
	for _, kind := range []string{"identical-users", "combinations", "unique-restriction"} {
		b.Run(kind, func(b *testing.B) {
			isolateBenchmarkInterner(b)
			policies := benchmarkPolicies(b, 600, "static")
			var groups []sec.Scope
			declarations := make([]sec.Policy, 64)
			for i := range declarations {
				declarations[i] = benchmarkDeclaration(b, fmt.Sprint(i))
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			for mask := 0; mask < 1024; mask++ {
				if kind != "combinations" && mask != 1023 {
					groups = append(groups, nil)
					continue
				}
				var list []sec.Policy
				for group := 0; group < 10; group++ {
					if mask&(1<<group) != 0 {
						list = append(list, policies[group*60:(group+1)*60]...)
					}
				}
				groups = append(groups, NewScope(list))
			}
			n := 10000
			if kind == "combinations" {
				n = 1024 * 64
			}
			live := make([]sec.Scope, n)
			b.ResetTimer()
			for i := range live {
				s := groups[1023]
				if kind == "identical-users" {
					s = NewScope(policies)
				}
				switch kind {
				case "combinations":
					s = groups[i%1024].With(declarations[i/1024])
				case "unique-restriction":
					s = s.With(newMockPolicy(fmt.Sprint(i), sec.Undefined))
				}
				live[i] = s
				s.Evaluate(sec.Actor{}, "registry.get", "entry:42", nil)
			}
			b.StopTimer()
			runtime.GC()
			runtime.ReadMemStats(&after)
			b.ReportMetric(float64(int64(after.HeapAlloc)-int64(before.HeapAlloc)), "retained-B/population")
			b.ReportMetric(float64(n), "scopes/population")
			runtime.KeepAlive(live)
			runtime.KeepAlive(groups)
			runtime.KeepAlive(policies)
			runtime.KeepAlive(declarations)
		})
	}
}
