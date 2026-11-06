// SPDX-License-Identifier: MPL-2.0

package security

import (
	"fmt"
	"math/rand"
	"strings"
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

func freshShapedScope(policies []sec.Policy) sec.Scope {
	entries := make([]policyEntry, len(policies))
	for i, p := range policies {
		entries[i] = describePolicy(p)
	}
	return &buildSegment(entries).single
}

type shapedCounter struct {
	*impl.Policy
	calls, matches atomic.Int64
}

func (p *shapedCounter) PolicyIdentity(candidate sec.Policy) uint64 {
	if candidate != p {
		return 0
	}
	return p.Policy.PolicyIdentity(p.Policy)
}
func (p *shapedCounter) Evaluate(actor sec.Actor, action, resource string, meta attrs.Bag) sec.Result {
	p.calls.Add(1)
	return p.Policy.Evaluate(actor, action, resource, meta)
}
func (p *shapedCounter) AppliesToAction(action string) bool {
	p.matches.Add(1)
	return p.Policy.AppliesToAction(action)
}

func TestShapedActionsKeepPlans(t *testing.T) {
	for _, n := range []int{430, 700, 1024} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			list, _ := shapedPolicies(t, n)
			var counters []*shapedCounter
			for i, p := range list {
				_, static := p.(sec.PolicyDescriptor).StaticEffect()
				if static {
					counter := &shapedCounter{Policy: p.(*impl.Policy)}
					counters = append(counters, counter)
					list[i] = counter
				}
			}
			s := freshShapedScope(list).(*scope)
			part := s.segments[0]
			for _, action := range append(shapeOtherActions(), shapedHotActions...) {
				s.Evaluate(sec.Actor{}, action, "missing", nil)
			}
			for _, action := range shapedHotActions {
				for _, p := range counters {
					p.calls.Store(0)
					p.matches.Store(0)
				}
				s.Evaluate(sec.Actor{}, action, "entry:0429:prefix:0000:xchild", nil)
				require.NotNil(t, part.plans.Load().byAction[action].value.Load(), action)
				for _, p := range counters {
					require.Zero(t, p.calls.Load(), action)
					require.Zero(t, p.matches.Load(), action)
				}
			}
			require.Len(t, part.plans.Load().byAction, 96)
		})
	}
}

func TestOpaquePoliciesKeepNativePlans(t *testing.T) {
	list, _ := countPolicies(400, false)
	opaque := mutableValuePolicy{id: list[0].ID(), state: []sec.Result{sec.Undefined}}
	list = append(list[1:], opaque)
	s := freshShapedScope(list).(*scope)
	require.True(t, s.segments[0].opaque)
	require.NotNil(t, s.segments[0].plan("1"))
	require.Equal(t, sec.Allow, s.Evaluate(sec.Actor{}, "1", "entry", nil))
	opaque.state[0] = sec.Deny
	require.Equal(t, sec.Deny, s.Evaluate(sec.Actor{}, "1", "entry", nil))
}

func TestShapedDifferential(t *testing.T) {
	for _, n := range []int{430, 700, 1024} {
		list, oracle := shapedPolicies(t, n)
		// Nested allow/deny prefixes exercise joins at internal nodes and exact ends.
		for i, test := range []struct {
			resource string
			effect   cfg.Effect
		}{{"nest*", cfg.Allow}, {"nested*", cfg.Deny}, {"nested.safe*", cfg.Allow}, {"nested.safe.value", cfg.Allow}, {"nest.*", cfg.Allow}} {
			p := nativePolicy(t, fmt.Sprintf("nested-%d", i), shapeAny(shapedHotActions), test.resource, test.effect)
			list = append(list, p)
			result := sec.Allow
			if test.effect == cfg.Deny {
				result = sec.Deny
			}
			oracle = append(oracle, &referencePolicy{id: p.ID(), actions: shapedHotActions, resources: test.resource, effect: result})
		}
		s := NewScope(list)
		resources := []string{"", "missing", "nest", "nested", "nested.safe.value", "nesting", "entry:0000:prefix:0000:x", "entry:0000:prefix:0000:xchild", "entry:0429:prefix:0000:xchild", strings.Repeat("x", 65536)}
		for _, action := range append(shapedHotActions, shapeOtherActions()...) {
			for _, resource := range resources {
				for _, meta := range []attrs.Bag{nil, {"enabled": true}, {"enabled": false}} {
					require.Equal(t, referenceScope(oracle, sec.Actor{}, action, resource, meta), s.Evaluate(sec.Actor{}, action, resource, meta), "n=%d action=%s resource=%s", n, action, resource)
				}
			}
		}
	}
}

func TestAllExactActionsAdmitted(t *testing.T) {
	names := make([]string, 130)
	for i := range names {
		names[i] = fmt.Sprintf("action-%d", i)
	}
	names[129] = strings.Repeat("a", 65536)
	resources := make([]string, 2000)
	for i := range resources {
		resources[i] = fmt.Sprintf("resource-%04d-%s*", i, strings.Repeat("x", 40))
	}
	p := nativePolicy(t, "many-actions", shapeAny(names), shapeAny(resources), cfg.Allow)
	s := freshShapedScope([]sec.Policy{p}).(*scope)
	for _, action := range names {
		require.Equal(t, sec.Allow, s.Evaluate(sec.Actor{}, action, strings.TrimSuffix(resources[1999], "*"), nil))
		require.NotNil(t, s.segments[0].plan(action))
	}
	require.Len(t, s.segments[0].plans.Load().byAction, len(names))
}

func TestResourceRadixDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(87123))
	var tree resourceRadix
	prefixes := map[string]uint8{}
	for i := 0; i < 2000; i++ {
		bytes := make([]byte, 1+rng.Intn(48))
		for j := range bytes {
			bytes[j] = byte(rng.Intn(256))
		}
		prefix := string(bytes)
		effects := uint8(1 + rng.Intn(3))
		prefixes[prefix] |= effects
		tree.insert(prefix, effects)
		for _, resource := range []string{prefix, prefix + "suffix", prefix[:len(prefix)/2], "missing"} {
			var want uint8
			for key, value := range prefixes {
				if strings.HasPrefix(resource, key) {
					want |= value
				}
			}
			require.Equal(t, want, tree.match(resource))
		}
	}
}

func TestPatternClassesDoNotDisplaceExactPlans(t *testing.T) {
	actions := append(shapeAny(shapeOtherActions()), "pattern.*")
	p := nativePolicy(t, "action-shapes", actions, "prefix*", cfg.Allow)
	part := freshShapedScope([]sec.Policy{p}).(*scope).segments[0]
	first := part.plan("pattern.first")
	for i := 0; i < 1000; i++ {
		require.Equal(t, sec.Allow, part.evaluate(sec.Actor{}, fmt.Sprintf("pattern.%d", i), "prefix", nil))
		require.Same(t, first, part.plan(fmt.Sprintf("pattern.%d", i)))
	}
	for _, action := range shapeOtherActions() {
		require.NotNil(t, part.plan(action))
	}
	require.Len(t, part.plans.Load().byAction, 86)
	require.Len(t, retainedPatternPlans(part.plans.Load()), 1)
}

func TestConcurrentShapedPlans(t *testing.T) {
	list, _ := shapedPolicies(t, 1024)
	list = append(list, mutableValuePolicy{id: registry.NewID("shape", "opaque"), state: []sec.Result{sec.Undefined}})
	part := freshShapedScope(list).(*scope).segments[0]
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, action := range append(shapedHotActions, shapeOtherActions()...) {
				part.evaluate(sec.Actor{}, action, "missing", nil)
				if part.plan(action) == nil {
					t.Error("missing named plan")
				}
			}
		}()
	}
	wg.Wait()
	require.Len(t, part.plans.Load().byAction, 96)
}

func radixSize(root *resourceRadix) (nodes, labelBytes int) {
	pending := []*radix[uint8]{&root.radix}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		nodes++
		labelBytes += len(node.prefix)
		for _, child := range node.children {
			pending = append(pending, child)
		}
	}
	return nodes, labelBytes
}

func TestPlanStorageBoundedByCandidates(t *testing.T) {
	for _, n := range []int{430, 700, 1024} {
		list, _ := shapedPolicies(t, n)
		part := freshShapedScope(list).(*scope).segments[0]
		for _, action := range append(shapedHotActions, shapeOtherActions()...) {
			plan := part.plan(action)
			require.NotNil(t, plan)
			var exactCount, prefixCount, prefixBytes, dynamic int
			for _, p := range list {
				d := p.(sec.PolicyDescriptor)
				if !d.AppliesToAction(action) {
					continue
				}
				if _, static := d.StaticEffect(); !static {
					dynamic++
					continue
				}
				_, exact, prefixes := d.ResourceFilter()
				exactCount += len(exact)
				prefixCount += len(prefixes)
				for _, prefix := range prefixes {
					prefixBytes += len(prefix)
				}
			}
			nodes, bytes := radixSize(&plan.prefixes)
			require.LessOrEqual(t, len(plan.exact), exactCount)
			require.LessOrEqual(t, nodes, 2*prefixCount+1)
			require.LessOrEqual(t, bytes, prefixBytes)
			require.Len(t, plan.dynamic, dynamic)
		}
	}
}
