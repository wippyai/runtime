// SPDX-License-Identifier: MPL-2.0

package security

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
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
		{[]any{"foo*", "foobar*", "foo.*"}, cfg.Deny}, {"foo", cfg.Allow},
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
	for _, action := range []string{"", "read", "reading", "read.safe", "read.other", "foo", "foobar", "foobarbaz", "a", "z", "\xff"} {
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
