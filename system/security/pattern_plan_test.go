// SPDX-License-Identifier: MPL-2.0

package security

import (
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	sec "github.com/wippyai/runtime/api/security"
	cfg "github.com/wippyai/runtime/api/service/security/policy"
	impl "github.com/wippyai/runtime/service/security/policy"
)

func TestPatternClassPlansShareAcrossCallerActions(t *testing.T) {
	for _, n := range []int{430, 1024} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			list, oracle := shapedPolicies(t, n)
			for i, actions := range []any{"overlay.*", "overlay.read*", []any{"overlay.read.deep*", "overlay.*"}} {
				p := nativePolicy(t, fmt.Sprintf("pattern-%d", i), actions, "private*", cfg.Deny)
				list = append(list, p)
				oracle = append(oracle, &referencePolicy{id: p.ID(), actions: actions, resources: "private*", effect: sec.Deny})
			}
			var counters []*shapedCounter
			for i, p := range list {
				if _, static := p.(sec.PolicyDescriptor).StaticEffect(); static {
					counter := &shapedCounter{Policy: p.(*impl.Policy)}
					list[i] = counter
					counters = append(counters, counter)
				}
			}
			part := freshShapedScope(list).(*scope).segments[0]
			roots := []string{"never.", "overlay.write.", "overlay.read.", "overlay.read.deep."}
			actions := make([]string, 1000)
			for i := range actions {
				actions[i] = roots[i%len(roots)] + fmt.Sprint(i)
				if i%17 == 0 {
					actions[i] += strings.Repeat("x", 65536)
				}
			}
			plans := make([]*actionPlan, len(roots))
			for i, root := range roots {
				plans[i] = part.plan(root + "warm")
			}
			require.Len(t, retainedPatternPlans(part.plans.Load()), len(roots))
			for _, p := range counters {
				p.calls.Store(0)
				p.matches.Store(0)
			}
			for i, action := range actions {
				require.Same(t, plans[i%len(roots)], part.plan(action))
				for _, resource := range []string{"missing", "private", "private.child"} {
					require.Equal(t, referenceScope(oracle, sec.Actor{}, action, resource, nil), part.evaluate(sec.Actor{}, action, resource, nil))
				}
			}
			for _, p := range counters {
				require.Zero(t, p.calls.Load())
				require.Zero(t, p.matches.Load())
			}
			require.Len(t, retainedPatternPlans(part.plans.Load()), len(roots))
			require.Zero(t, testing.AllocsPerRun(5, func() {
				for _, action := range actions {
					part.evaluate(sec.Actor{}, action, "missing", nil)
				}
			}))
		})
	}
}

func TestPatternAndExactActionCandidates(t *testing.T) {
	var list, oracle []sec.Policy
	for i, test := range []struct {
		actions any
		effect  cfg.Effect
	}{
		{"*", cfg.Allow}, {"read.*", cfg.Deny}, {"read.safe", cfg.Allow},
		{[]any{"foo*", "foobar*", "foo.*"}, cfg.Deny}, {"foo", cfg.Allow}, {"edit*", cfg.Allow}, {"edit", cfg.Deny},
		{[]any{".*", "a*"}, cfg.Allow},
	} {
		p := nativePolicy(t, fmt.Sprintf("mixed-%d", i), test.actions, "*", test.effect)
		list = append(list, p)
		result := sec.Allow
		if test.effect == cfg.Deny {
			result = sec.Deny
		}
		oracle = append(oracle, &referencePolicy{id: p.ID(), actions: test.actions, resources: "*", effect: result})
	}
	part := freshShapedScope(list).(*scope).segments[0]
	for _, action := range []string{"", "read", "reading", "read.safe", "read.other", "foo", "foobar", "foobarbaz", "edit", "editing", "edit.suffix", "a", "z", "\xff"} {
		require.Equal(t, referenceScope(oracle, sec.Actor{}, action, "r", nil), part.evaluate(sec.Actor{}, action, "r", nil), action)
	}
	require.NotSame(t, part.plan("foo"), part.plan("foox"), "exact actions keep their own candidates")
	require.Same(t, part.plan("foox"), part.plan("fooy"))
}

func TestUnmatchedActionsBuildNoPlan(t *testing.T) {
	part := freshShapedScope([]sec.Policy{nativePolicy(t, "only", "read.*", "*", cfg.Allow)}).(*scope).segments[0]
	for i := 0; i < 1000; i++ {
		require.Nil(t, part.plan(fmt.Sprintf("unknown.%d", i)))
	}
	require.Zero(t, testing.AllocsPerRun(100, func() {
		require.Equal(t, sec.Undefined, part.evaluate(sec.Actor{}, "unknown", "r", nil))
	}))
}

func TestConcurrentPatternClassPublication(t *testing.T) {
	list, _ := shapedPolicies(t, 430)
	part := freshShapedScope(list).(*scope).segments[0]
	var wg sync.WaitGroup
	plans := make([]*actionPlan, 16)
	for i := range plans {
		wg.Add(1)
		go func(i int) { defer wg.Done(); plans[i] = part.plan(fmt.Sprintf("never.%d", i)) }(i)
	}
	wg.Wait()
	for _, plan := range plans {
		require.Same(t, plans[0], plan)
	}
}

func retainedPatternPlans(index *actionPlans) []*actionPlan {
	var result []*actionPlan
	var visit func(*radix[*patternClass])
	visit = func(node *radix[*patternClass]) {
		if node.value != nil {
			if plan := node.value.slot.value.Load(); plan != nil {
				result = append(result, plan)
			}
		}
		for _, child := range node.children {
			visit(child)
		}
	}
	visit(&index.patterns.radix)
	return result
}

func TestPatternStreamRetainedMemory(t *testing.T) {
	for _, n := range []int{430, 1024} {
		list, _ := shapedPolicies(t, n)
		part := freshShapedScope(list).(*scope).segments[0]
		warm := part.plan("unnamed.warm")
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for i := 0; i < 10000; i++ {
			part.evaluate(sec.Actor{}, fmt.Sprintf("unnamed.%08d", i), "missing", nil)
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		t.Logf("policies=%d live heap delta after 10000 actions=%d bytes", n, int64(after.HeapAlloc)-int64(before.HeapAlloc))
		if warm != part.plan("unnamed.final") {
			t.Fatal("pattern plan changes with caller key")
		}
		runtime.KeepAlive(part)
	}
}

func TestPatternPlanPassesOriginalArguments(t *testing.T) {
	p, err := impl.NewExprPolicy(registry.NewID("test", "dynamic-pattern"), &cfg.ExprConfig{Policy: cfg.ExprDefinition{
		Actions: "read*", Resources: "*", Effect: cfg.Deny, Expression: `action == meta.action && actor.ID == meta.owner`,
	}})
	require.NoError(t, err)
	part := freshShapedScope([]sec.Policy{p, nativePolicy(t, "allow-pattern", "*", "*", cfg.Allow)}).(*scope).segments[0]
	meta := attrs.Bag{"action": "read.one", "owner": "a"}
	require.Equal(t, sec.Deny, part.evaluate(sec.Actor{ID: "a"}, "read.one", "r", meta))
	require.Equal(t, sec.Allow, part.evaluate(sec.Actor{ID: "b"}, "read.one", "r", meta))
	require.Equal(t, sec.Allow, part.evaluate(sec.Actor{ID: "a"}, "read.two", "r", meta))
	require.Same(t, part.plan("read.one"), part.plan("read.two"))
	meta["action"] = "read.two"
	require.Equal(t, sec.Deny, part.evaluate(sec.Actor{ID: "a"}, "read.two", "r", meta))
}

func TestActionPatternIndexDifferentialAndBound(t *testing.T) {
	rng := rand.New(rand.NewSource(56781))
	var list, oracle []sec.Policy
	patternBytes, patternEntries := 0, 0
	for i := 0; i < 150; i++ {
		raw := make([]byte, 1+rng.Intn(24))
		for j := range raw {
			raw[j] = byte('a' + rng.Intn(5))
		}
		prefix := string(raw)
		if i == 149 {
			prefix += strings.Repeat("x", 65536)
		}
		actions := []any{prefix + "*", prefix + "deeper.*", prefix + ".exact"}
		patternBytes += len(prefix) + len(prefix+"deeper")
		patternEntries += 2
		effect, result := cfg.Allow, sec.Allow
		if i%3 == 0 {
			effect, result = cfg.Deny, sec.Deny
		}
		p := nativePolicy(t, fmt.Sprintf("random-pattern-%d", i), actions, "*", effect)
		list = append(list, p)
		oracle = append(oracle, &referencePolicy{id: p.ID(), actions: actions, resources: "*", effect: result})
	}
	part := freshShapedScope(list).(*scope).segments[0]
	for _, p := range oracle {
		for _, action := range p.(*referencePolicy).actions.([]any) {
			prefix := strings.TrimSuffix(action.(string), "*")
			for _, query := range []string{prefix, prefix + "suffix", prefix[:len(prefix)/2]} {
				require.Equal(t, referenceScope(oracle, sec.Actor{}, query, "r", nil), part.evaluate(sec.Actor{}, query, "r", nil))
			}
		}
	}
	index := part.plans.Load()
	nodes, labels, entries := 0, 0, 0
	var visit func(*radix[*patternClass])
	visit = func(node *radix[*patternClass]) {
		nodes++
		labels += len(node.prefix)
		if node.value != nil {
			entries += len(node.value.candidates)
		}
		for _, child := range node.children {
			visit(child)
		}
	}
	visit(&index.patterns.radix)
	require.LessOrEqual(t, nodes, 2*patternEntries+1)
	require.LessOrEqual(t, labels, patternBytes)
	require.Equal(t, patternEntries, entries)
	require.LessOrEqual(t, len(retainedPatternPlans(index)), patternEntries)
	require.Greater(t, len(retainedPatternPlans(index)), 32, "policy-owned pattern sets have no fixed plan cap")
}

func TestPatternPlanEvaluatesDynamicCandidateOnce(t *testing.T) {
	p, err := impl.NewPolicy(registry.NewID("test", "duplicate-pattern"), &cfg.Config{Policy: cfg.Definition{
		Actions: []any{"a*", "ab*", "a.*"}, Resources: "*", Effect: cfg.Allow,
		Conditions: []cfg.Condition{{Field: "meta.enabled", Operator: "eq", Value: true}},
	}})
	require.NoError(t, err)
	counter := &shapedCounter{Policy: p}
	part := freshShapedScope([]sec.Policy{counter}).(*scope).segments[0]
	for _, action := range []string{"abc", "abxyz"} {
		counter.calls.Store(0)
		require.Equal(t, sec.Allow, part.evaluate(sec.Actor{}, action, "r", attrs.Bag{"enabled": true}))
		require.EqualValues(t, 1, counter.calls.Load())
	}
	require.Same(t, part.plan("abc"), part.plan("abxyz"))
}
