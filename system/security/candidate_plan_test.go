// SPDX-License-Identifier: MPL-2.0

package security

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	sec "github.com/wippyai/runtime/api/security"
	cfg "github.com/wippyai/runtime/api/service/security/policy"
	impl "github.com/wippyai/runtime/service/security/policy"
)

func TestExactActionsShareCandidatePlanAndStorage(t *testing.T) {
	names, resources := make([]string, 2000), make([]string, 2000)
	for i := range names {
		names[i] = fmt.Sprintf("action.%04d", i)
		resources[i] = fmt.Sprintf("resource.%04d", i)
	}
	p := nativePolicy(t, "shared-resources", shapeAny(names), shapeAny(resources), cfg.Allow)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	part := freshShapedScope([]sec.Policy{p}).(*scope).segments[0]
	plans := make(map[*actionPlan]struct{})
	for _, name := range names {
		require.Equal(t, sec.Allow, part.evaluate(sec.Actor{}, name, resources[0], nil))
		plans[part.plan(name)] = struct{}{}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("2000 actions x 2000 resources: plans=%d retained=%d bytes", len(plans), retained)
	require.Equal(t, 1, len(plans))
	require.Less(t, retained, int64(2<<20), "segment/index/plan storage excludes existing policy data")
	runtime.KeepAlive(part)
	runtime.KeepAlive(p)
}

func TestExactAndPatternCandidatePlanIdentity(t *testing.T) {
	part := freshShapedScope([]sec.Policy{
		nativePolicy(t, "common", []any{"*", "read", "write", "prefix*", "prefix.deep*"}, "*", cfg.Allow),
		nativePolicy(t, "additional", "write", "private*", cfg.Deny),
	}).(*scope).segments[0]
	common := part.plan("read")
	for _, action := range []string{"unknown", "prefix", "prefix.deep.value"} {
		require.Same(t, common, part.plan(action), action)
	}
	require.NotSame(t, common, part.plan("write"))
	require.Equal(t, sec.Deny, part.evaluate(sec.Actor{}, "write", "private", nil))
	require.Equal(t, sec.Allow, part.evaluate(sec.Actor{}, "read", "private", nil))
	different := freshShapedScope([]sec.Policy{
		nativePolicy(t, "first", "read", "*", cfg.Allow),
		nativePolicy(t, "second", "write", "*", cfg.Allow),
	}).(*scope).segments[0]
	require.NotSame(t, different.plan("read"), different.plan("write"), "equal resources do not identify candidates")
}

func TestSharedExactPlanPassesOriginalArguments(t *testing.T) {
	p, err := impl.NewExprPolicy(registry.NewID("test", "shared-dynamic"), &cfg.ExprConfig{Policy: cfg.ExprDefinition{
		Actions: []any{"read", "write", "prefix*"}, Resources: "*", Effect: cfg.Deny,
		Expression: `action == meta.action && actor.ID == meta.owner`,
	}})
	require.NoError(t, err)
	part := freshShapedScope([]sec.Policy{p}).(*scope).segments[0]
	meta := attrs.Bag{"action": "read", "owner": "a"}
	for _, action := range []string{"read", "write", "prefix.other"} {
		want := sec.Undefined
		if action == "read" {
			want = sec.Deny
		}
		require.Equal(t, want, part.evaluate(sec.Actor{ID: "a"}, action, "r", meta))
		require.Same(t, part.plan("read"), part.plan(action))
		require.Equal(t, sec.Undefined, part.evaluate(sec.Actor{ID: "b"}, action, "r", meta))
	}
	meta["action"] = "write"
	require.Equal(t, sec.Deny, part.evaluate(sec.Actor{ID: "a"}, "write", "r", meta))
}

func TestConcurrentEquivalentCandidatePlans(t *testing.T) {
	p := &resourceFilterCounter{Policy: nativePolicy(t, "concurrent", []any{"read", "write", "prefix*"}, "resource*", cfg.Allow).(*impl.Policy)}
	part := freshShapedScope([]sec.Policy{p}).(*scope).segments[0]
	var wg sync.WaitGroup
	start := make(chan struct{})
	plans := make([]*actionPlan, 32)
	for i := range plans {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			plans[i] = part.plan([]string{"read", "write", "prefix.other"}[i%3])
		}(i)
	}
	close(start)
	wg.Wait()
	for _, plan := range plans {
		require.Same(t, plans[0], plan)
	}
	require.EqualValues(t, 1, p.filters.Load(), "candidate identity is resolved before building resources")
}

type resourceFilterCounter struct {
	*impl.Policy
	filters atomic.Int64
}

func (p *resourceFilterCounter) PolicyIdentity(candidate sec.Policy) uint64 {
	if candidate != p {
		return 0
	}
	return p.Policy.PolicyIdentity(p.Policy)
}

func (p *resourceFilterCounter) ResourceFilter() (bool, []string, []string) {
	p.filters.Add(1)
	return p.Policy.ResourceFilter()
}

func TestShapedCandidatePlanIdentity(t *testing.T) {
	for _, n := range []int{430, 700, 1024} {
		list, oracle := shapedPolicies(t, n)
		part := freshShapedScope(list).(*scope).segments[0]
		byCandidates := make(map[string]*actionPlan)
		byPlan := make(map[*actionPlan]string)
		for _, action := range append(shapeOtherActions(), append(shapedHotActions, "unnamed")...) {
			var key string
			for i, p := range oracle {
				if referenceMatch(p.(*referencePolicy).actions, action) {
					key += fmt.Sprintf("%d,", i)
				}
			}
			plan := part.plan(action)
			if previous, ok := byCandidates[key]; ok {
				require.Same(t, previous, plan)
			}
			if previous, ok := byPlan[plan]; ok {
				require.Equal(t, previous, key, "different candidate sets never share")
			}
			byCandidates[key], byPlan[plan] = plan, key
			for _, meta := range []attrs.Bag{nil, {"enabled": true}, {"enabled": false}} {
				require.Equal(t, referenceScope(oracle, sec.Actor{}, action, "missing", meta), part.evaluate(sec.Actor{}, action, "missing", meta))
			}
		}
	}
}

func TestDeepPatternLinks(t *testing.T) {
	var tree actionRadix
	node := &tree.radix
	const depth = 20000
	for i := 0; i < depth; i++ {
		child := &radix[*patternClass]{prefix: "a", value: &patternClass{candidates: []int{i}}}
		node.children = map[byte]*radix[*patternClass]{'a': child}
		node = child
	}
	tree.link(nil)
	class := node.value
	for i := depth - 1; i >= 0; i-- {
		require.NotNil(t, class)
		require.Equal(t, []int{i}, class.candidates)
		class = class.parent
	}
	require.Nil(t, class)
}
