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
)

func TestSmallResidentListStackAndAllocations(t *testing.T) {
	policies, _ := countPolicies(600, false)
	for _, n := range []int{2, 3, 8, 32, 400, 600} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			list := policies[:n]
			resident := NewScope(list)
			require.Zero(t, testing.AllocsPerRun(100, func() { benchmarkScope = NewScope(list) }))
			require.True(t, resident == benchmarkScope)
			if n <= 3 {
				growth := stackGrowth(64, func() { NewScope(list) })
				require.LessOrEqual(t, growth, uint64(16<<10), "small declarations keep small goroutine stacks")
			}
		})
	}
}

func TestNestedDeclarationsKeepLargeBase(t *testing.T) {
	bases := make([]*scope, 10)
	for i := range bases {
		policies, _ := countPolicies(400, false)
		bases[i] = makeScope(policies)
	}
	for chain := 0; chain < 100; chain++ {
		declarations := make([]*scope, 12)
		for depth := range declarations {
			effect := cfg.Allow
			if depth%3 == 0 {
				effect = cfg.Deny
			}
			declarations[depth] = makeScope([]sec.Policy{nativePolicy(t, fmt.Sprintf("chain-%d-%d", chain, depth), "nested", fmt.Sprintf("entry-%d", depth%3), effect)})
		}
		for _, base := range bases {
			part := base.segments[0]
			base.Evaluate(sec.Actor{}, "0", "entry", nil)
			plan := part.plan("0")
			s := base
			flat := base.Policies()
			for _, declaration := range declarations {
				s = s.merge(declaration)
				flat = append(flat, declaration.Policies()...)
				require.True(t, hasSegment(s, part), "nested declarations retain the shared base")
				require.Same(t, plan, part.plan("0"))
				require.LessOrEqual(t, len(s.segments), maxScopeSegments)
				for _, resource := range []string{"entry-0", "entry-1", "entry-2", "entry"} {
					require.Equal(t, referenceScope(flat, sec.Actor{}, "nested", resource, nil), s.Evaluate(sec.Actor{}, "nested", resource, nil))
				}
			}
		}
	}
}

func TestWithLoopKeepsLargeBase(t *testing.T) {
	policies, _ := countPolicies(400, false)
	s := makeScope(policies)
	base := s.segments[0]
	for i := 0; i < 64; i++ {
		s = s.With(nativePolicy(t, fmt.Sprintf("loop-%d", i), "other", "*", cfg.Allow)).(*scope)
		require.True(t, hasSegment(s, base), "single additions retain the shared base")
		require.LessOrEqual(t, len(s.segments), maxScopeSegments)
	}
}

func hasSegment(s *scope, part *segment) bool {
	for _, candidate := range s.segments {
		if candidate == part {
			return true
		}
	}
	return false
}

func TestNativeScopeResolverAndEmptyShape(t *testing.T) {
	reg := NewPolicyRegistry(nil, nil)
	resolved, err := resolveScope(reg, nil, nil)
	require.NoError(t, err)
	require.True(t, resolved.empty())
	require.True(t, (&scope{}).empty())
	p := nativePolicy(t, "resolved", "read", "*", cfg.Allow)
	reg.policies.Store(p.ID(), &sec.PolicyEntry{Policy: p})
	resolved, err = resolveScope(reg, nil, []registry.ID{p.ID()})
	require.NoError(t, err)
	require.False(t, resolved.empty())
	shaped := &scope{segments: []*segment{makeScope(nil).segments[0], resolved.segments[0]}}
	require.False(t, shaped.empty(), "emptiness belongs to the whole scope")
	public, err := ResolveScope(reg, nil, []registry.ID{p.ID()})
	require.NoError(t, err)
	require.Same(t, resolved, public)
}

func TestCompactionLargeSegmentBudget(t *testing.T) {
	s := makeScope(nil)
	var large []*segment
	for group := 0; group < maxScopeSegments; group++ {
		list := make([]sec.Policy, 64)
		for i := range list {
			list[i] = nativePolicy(t, fmt.Sprintf("large-%d-%d", group, i), "read", "*", cfg.Allow)
		}
		part := makeScope(list)
		large = append(large, part.segments[0])
		s = s.merge(part)
		require.LessOrEqual(t, len(s.segments), maxScopeSegments)
		if group == maxScopeSegments-2 {
			small := s.With(nativePolicy(t, "small-one", "write", "*", cfg.Allow)).With(nativePolicy(t, "small-two", "write", "*", cfg.Deny)).(*scope)
			require.LessOrEqual(t, len(small.segments), maxScopeSegments)
			for _, base := range large {
				require.True(t, hasSegment(small, base))
			}
			require.Equal(t, sec.Deny, small.Evaluate(sec.Actor{}, "write", "entry", nil))
		}
	}
	s = s.With(nativePolicy(t, "overflow", "write", "*", cfg.Allow)).(*scope)
	require.Len(t, s.segments, 1, "overflow with eight large segments compacts the whole scope")
	require.Len(t, s.Policies(), maxScopeSegments*64+1)
	require.Equal(t, sec.Allow, s.Evaluate(sec.Actor{}, "read", "entry", nil))
}

func TestConcurrentNestedCompaction(t *testing.T) {
	list, _ := countPolicies(400, false)
	base := makeScope(list)
	declarations := make([]*scope, 16)
	for i := range declarations {
		declarations[i] = makeScope([]sec.Policy{nativePolicy(t, fmt.Sprintf("concurrent-depth-%d", i), "nested", "*", cfg.Allow)})
	}
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repeat := 0; repeat < 20; repeat++ {
				s := base
				for _, declared := range declarations {
					s = s.merge(declared)
					if !hasSegment(s, base.segments[0]) || len(s.segments) > maxScopeSegments || s.Evaluate(sec.Actor{}, "0", "entry", nil) != sec.Allow {
						t.Error("concurrent compaction loses shared authority")
					}
				}
			}
		}()
	}
	wg.Wait()
}
