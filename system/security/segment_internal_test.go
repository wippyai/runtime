// SPDX-License-Identifier: MPL-2.0

package security

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	sec "github.com/wippyai/runtime/api/security"
	cfg "github.com/wippyai/runtime/api/service/security/policy"
	impl "github.com/wippyai/runtime/service/security/policy"
)

func TestSegmentsRetainSharedPlansAndIndexes(t *testing.T) {
	policies, _ := countPolicies(500, false)
	base := NewScope(policies).(*scope)
	base.Evaluate(sec.Actor{}, "0", "entry", nil)
	part := base.segments[0]
	plan := part.plan("0")
	for i := 0; i < 3000; i++ {
		declaration := makeScope([]sec.Policy{nativePolicy(t, fmt.Sprint(i), "other", "*", cfg.Allow)})
		joined := base.merge(declaration)
		require.Same(t, part, joined.segments[0])
		require.Same(t, declaration.segments[0], joined.segments[1])
		require.Same(t, plan, joined.segments[0].plan("0"))
	}
	tail1 := nativePolicy(t, "tail-one", "other", "*", cfg.Allow)
	tail2 := nativePolicy(t, "tail-two", "other", "*", cfg.Allow)
	layered := base.With(tail1).With(tail2).(*scope)
	require.Len(t, layered.segments, 3)
	withoutBaseEntry := layered.Without(policies[42].ID()).(*scope)
	require.Len(t, withoutBaseEntry.segments, 3)
	require.False(t, withoutBaseEntry.Contains(policies[42].ID()))
	require.NotSame(t, part, withoutBaseEntry.segments[0])
	require.Same(t, layered.segments[1], withoutBaseEntry.segments[1])
	require.Same(t, layered.segments[2], withoutBaseEntry.segments[2])
	withoutTail := layered.Without(tail1.ID()).(*scope)
	require.Len(t, withoutTail.segments, 2)
	require.Same(t, part, withoutTail.segments[0])
	require.Same(t, layered.segments[2], withoutTail.segments[1])
	overlap := makeScope(append(append([]sec.Policy(nil), policies[10:20]...), tail1))
	merged := base.merge(overlap)
	require.Len(t, merged.segments, 2)
	require.Same(t, part, merged.segments[0])
	require.Len(t, merged.segments[1].ordered, 1)
	require.True(t, merged.Contains(tail1.ID()))
	require.Same(t, base, base.merge(makeScope(policies[10:20])))

	s := base
	for i := 0; i < maxScopeSegments*4; i++ {
		s = s.With(nativePolicy(t, fmt.Sprintf("addition-%d", i), "other", "*", cfg.Allow)).(*scope)
		require.LessOrEqual(t, len(s.segments), maxScopeSegments)
	}
	removed := s.segments[len(s.segments)-1].ordered[0].id
	previous := append([]*segment(nil), s.segments...)
	without := s.Without(removed).(*scope)
	require.False(t, without.Contains(removed))
	for _, p := range previous {
		if _, ok := p.byID[removed]; !ok {
			found := false
			for _, q := range without.segments {
				found = found || p == q
			}
			require.True(t, found)
		}
	}
}

func TestSegmentAdmissionAndDuplicateReplacement(t *testing.T) {
	list, _ := countPolicies(maxInternedPolicies+100, false)
	s := NewScope(list).(*scope)
	require.Len(t, s.Policies(), len(list))
	require.NotSame(t, s, NewScope(list))
	p := nativePolicy(t, "duplicate-identity", "read", "*", cfg.Allow)
	opaque := mutableValuePolicy{id: p.ID(), state: []sec.Result{sec.Deny}}
	a := NewScope([]sec.Policy{opaque, p})
	require.True(t, a == NewScope([]sec.Policy{p}))
	b := NewScope([]sec.Policy{p, opaque}).(*scope)
	require.True(t, b.segments[0].opaque)
	require.Equal(t, sec.Deny, b.Evaluate(sec.Actor{}, "read", "entry", nil))
	require.Nil(t, b.segments[0].plans.Load())
}

func TestCopiedPolicyLastObjectWins(t *testing.T) {
	p, err := impl.NewPolicy(registry.NewID("test", "copy"), &cfg.Config{
		Policy: cfg.Definition{Actions: "read", Resources: "*", Effect: cfg.Allow},
	})
	require.NoError(t, err)
	copyOfPolicy := *p
	expr, err := impl.NewExprPolicy(p.ID(), &cfg.ExprConfig{
		Policy: cfg.ExprDefinition{Actions: "read", Resources: "*", Effect: cfg.Allow, Expression: "true"},
	})
	require.NoError(t, err)
	copyOfExpr := *expr
	for _, test := range []struct {
		original sec.Policy
		copy     sec.Policy
		name     string
	}{
		{name: "policy", original: p, copy: &copyOfPolicy},
		{name: "expression", original: expr, copy: &copyOfExpr},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := makeScope([]sec.Policy{test.original})
			for _, path := range []struct {
				build func() sec.Scope
				name  string
			}{
				{name: "with", build: func() sec.Scope { return base.With(test.copy) }},
				{name: "list", build: func() sec.Scope { return NewScope([]sec.Policy{test.original, test.copy}) }},
				{name: "boundary", build: func() sec.Scope { return base.merge(makeScope([]sec.Policy{test.copy})) }},
			} {
				t.Run(path.name, func(t *testing.T) {
					result := path.build().(*scope)
					require.Len(t, result.Policies(), 1)
					require.Same(t, test.copy, result.Policies()[0])
					require.True(t, result.segments[0].opaque)
					require.Equal(t, sec.Allow, result.Evaluate(sec.Actor{}, "read", "entry", nil))
					require.Nil(t, result.segments[0].plans.Load())
					require.NotSame(t, result, path.build(), "copied policies are not interned")
					require.Same(t, test.original, result.With(test.original).Policies()[0])
				})
			}
		})
	}
}

func BenchmarkSegmentPlanCrossover(b *testing.B) {
	for _, n := range []int{1, 2, 4, 8, 16, 32} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			policies := make([]sec.Policy, n)
			for i := range policies {
				p, err := impl.NewPolicy(registry.NewID("crossover", fmt.Sprint(i)), &cfg.Config{Policy: cfg.Definition{Actions: "registry.get", Resources: fmt.Sprintf("entry:%d", i), Effect: cfg.Allow}})
				if err != nil {
					b.Fatal(err)
				}
				policies[i] = p
			}
			s := makeScope(policies).segments[0]
			s.plan("registry.get")
			for _, kind := range []string{"direct", "plan"} {
				b.Run(kind, func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						if kind == "direct" {
							benchmarkResult = s.evaluatePolicies(sec.Actor{}, "registry.get", "entry:42", nil)
						} else {
							benchmarkResult = s.plan("registry.get").evaluate(sec.Actor{}, "registry.get", "entry:42", nil)
						}
					}
				})
			}
			b.Run("cold-plan", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					fresh := &segment{ordered: s.ordered}
					fresh.plan("registry.get")
				}
			})
		})
	}
}

func TestConcurrentSegmentEviction(t *testing.T) {
	c := newSegmentInterner()
	policies, _ := countPolicies(600, false)
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				part := c.fromList(policies[:1+(i*17+worker)%599])
				if part.single.Evaluate(sec.Actor{}, "0", "entry", nil) != sec.Allow {
					t.Error("concurrent segment decision changes")
				}
			}
		}(worker)
	}
	wg.Wait()
}

func TestSegmentCompactionDifferential(t *testing.T) {
	s := NewScope(nil).(*scope)
	var list []sec.Policy
	for i := 0; i < 96; i++ {
		effect := cfg.Allow
		if i%5 == 0 {
			effect = cfg.Deny
		}
		p := nativePolicy(t, fmt.Sprintf("compact-%d", i), fmt.Sprintf("action%d", i%4), []any{fmt.Sprintf("entry%d", i%3), fmt.Sprintf("prefix%d.*", i%3)}, effect)
		list = append(list, p)
		s = s.With(p).(*scope)
		require.LessOrEqual(t, len(s.segments), maxScopeSegments)
		for action := 0; action < 4; action++ {
			for _, resource := range []string{"entry0", "entry1", "prefix2suffix", "missing"} {
				a := fmt.Sprintf("action%d", action)
				require.Equal(t, referenceScope(list, sec.Actor{}, a, resource, nil), s.Evaluate(sec.Actor{}, a, resource, nil))
			}
		}
	}
}
