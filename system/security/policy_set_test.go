// SPDX-License-Identifier: MPL-2.0

package security

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	sec "github.com/wippyai/runtime/api/security"
	cfg "github.com/wippyai/runtime/api/service/security/policy"
	impl "github.com/wippyai/runtime/service/security/policy"
)

func nativePolicy(t *testing.T, name string, actions, resources any, effect cfg.Effect) sec.Policy {
	t.Helper()
	p, err := impl.NewPolicy(registry.NewID("test", name), &cfg.Config{Policy: cfg.Definition{Actions: actions, Resources: resources, Effect: effect}})
	require.NoError(t, err)
	return p
}
func TestSetReplacementAndWithout(t *testing.T) {
	allow := nativePolicy(t, "replace", "read", "*", cfg.Allow)
	deny := nativePolicy(t, "replace", "read", "*", cfg.Deny)
	a := NewScope([]sec.Policy{allow, deny})
	b := NewScope([]sec.Policy{deny, allow})
	require.Equal(t, sec.Deny, a.Evaluate(sec.Actor{}, "read", "entry", nil))
	require.Equal(t, sec.Allow, b.Evaluate(sec.Actor{}, "read", "entry", nil))
	require.True(t, b == NewScope([]sec.Policy{allow}))
	require.True(t, a == b.With(deny))
	require.True(t, b == a.With(allow))
	require.Equal(t, sec.Undefined, a.Without(deny.ID()).Evaluate(sec.Actor{}, "read", "entry", nil))
	require.True(t, a == a.Without(registry.NewID("test", "missing")))
	require.Empty(t, NewScope(nil).Policies())
}

type mutableValuePolicy struct {
	id    registry.ID
	state []sec.Result
}

func (p mutableValuePolicy) ID() registry.ID { return p.id }
func (p mutableValuePolicy) Evaluate(_ sec.Actor, _, _ string, _ attrs.Bag) sec.Result {
	return p.state[0]
}
func TestOpaqueNonComparableMutablePolicy(t *testing.T) {
	p := mutableValuePolicy{id: registry.NewID("test", "opaque"), state: []sec.Result{sec.Undefined}}
	allow := nativePolicy(t, "allow", "read", "*", cfg.Allow)
	s := NewScope([]sec.Policy{allow, p})
	other := NewScope([]sec.Policy{allow, p})
	require.False(t, s == other)
	require.Equal(t, sec.Allow, s.Evaluate(sec.Actor{}, "read", "entry", nil))
	p.state[0] = sec.Deny
	require.Equal(t, sec.Deny, s.Evaluate(sec.Actor{}, "read", "entry", nil))
	require.Equal(t, sec.Deny, s.Evaluate(sec.Actor{}, "unknown", "entry", nil))
}

type dispatchScope struct {
	sec.Scope
	calls int
}

func (s *dispatchScope) With(p sec.Policy) sec.Scope { s.calls++; return s }
func TestBoundaryForeignScopeDispatch(t *testing.T) {
	p := nativePolicy(t, "declared", "read", "*", cfg.Allow)
	reg := NewPolicyRegistry(nil, nil)
	reg.policies.Store(p.ID(), &sec.PolicyEntry{Policy: p})
	ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	defer ctxapi.ReleaseFrameContext(frame)
	ctx = sec.WithRegistry(ctx, reg)
	foreign := &dispatchScope{Scope: NewScope(nil)}
	require.NoError(t, sec.SetScope(ctx, foreign))
	_, err := ResolveConfigPairs(ctx, &sec.Config{Policies: []registry.ID{p.ID()}})
	require.NoError(t, err)
	require.Equal(t, 1, foreign.calls)
}
func TestRetainedScopeRegistrySnapshot(t *testing.T) {
	reg := NewPolicyRegistry(nil, nil)
	allow := nativePolicy(t, "snapshot", "read", "*", cfg.Allow)
	deny := nativePolicy(t, "snapshot", "read", "*", cfg.Deny)
	group := registry.NewID("test", "group")
	apply := func(kind event.Kind, p sec.Policy) {
		id := p.ID()
		reg.handleEvent(event.Event{Kind: kind, Path: id.String(), Data: &sec.PolicyEntry{Policy: p, Groups: []registry.ID{group}}})
	}
	apply(sec.PolicyRegister, allow)
	retained, err := reg.GetPolicyGroup(group)
	require.NoError(t, err)
	apply(sec.PolicyUpdate, deny)
	updated, err := reg.GetPolicyGroup(group)
	require.NoError(t, err)
	require.Equal(t, sec.Allow, retained.Evaluate(sec.Actor{}, "read", "entry", nil))
	require.Equal(t, sec.Deny, updated.Evaluate(sec.Actor{}, "read", "entry", nil))
	apply(sec.PolicyDelete, deny)
	require.Equal(t, sec.Allow, retained.Evaluate(sec.Actor{}, "read", "entry", nil))
	apply(sec.PolicyRegister, deny)
	again, err := reg.GetPolicyGroup(group)
	require.NoError(t, err)
	require.Equal(t, sec.Deny, again.Evaluate(sec.Actor{}, "read", "entry", nil))
	require.Equal(t, sec.Allow, retained.Evaluate(sec.Actor{}, "read", "entry", nil))
}
func TestPlanNoCapturedActorOrMetadata(t *testing.T) {
	for _, expression := range []bool{false, true} {
		t.Run(fmt.Sprint(expression), func(t *testing.T) {
			id := registry.NewID("test", "dynamic")
			var p sec.Policy
			var err error
			if expression {
				p, err = impl.NewExprPolicy(id, &cfg.ExprConfig{Policy: cfg.ExprDefinition{Actions: "read", Resources: "*", Effect: cfg.Deny, Expression: `actor.ID == meta.owner`}})
			} else {
				p, err = impl.NewPolicy(id, &cfg.Config{Policy: cfg.Definition{Actions: "read", Resources: "*", Effect: cfg.Deny, Conditions: []cfg.Condition{{Field: "actor.id", Operator: "eq", ValueFrom: "meta.owner"}}}})
			}
			require.NoError(t, err)
			policies := []sec.Policy{p, nativePolicy(t, "allow", "read", "*", cfg.Allow), nativePolicy(t, "actor-pad1", "other", "*", cfg.Allow), nativePolicy(t, "actor-pad2", "other", "*", cfg.Allow)}
			s := NewScope(policies)
			other := NewScope(policies)
			require.True(t, s == other)
			meta := attrs.Bag{"owner": "a"}
			require.Equal(t, sec.Deny, s.Evaluate(sec.Actor{ID: "a"}, "read", "entry", meta))
			require.NotNil(t, s.(*scope).segments[0].plans.Load(), "dynamic candidates exercise the shared plan")
			require.Equal(t, sec.Allow, other.Evaluate(sec.Actor{ID: "b"}, "read", "entry", meta))
			meta["owner"] = "b"
			require.Equal(t, sec.Allow, s.Evaluate(sec.Actor{ID: "a"}, "read", "entry", meta))
			require.Equal(t, sec.Deny, other.Evaluate(sec.Actor{ID: "b"}, "read", "entry", meta))
		})
	}
}
func internTestSegment(c *segmentInterner, key uint64, s *scope) *segment {
	entries := s.segments[0].ordered
	ids := make([]policyIdentity, len(entries))
	policies := make([]sec.Policy, len(entries))
	for i, e := range entries {
		ids[i] = policyIdentity{id: e.id, token: e.identity}
		policies[i] = e.policy
	}
	return c.intern(key, ids, policies)
}
func TestSetHashCollisionAndEviction(t *testing.T) {
	interner := newSegmentInterner()
	a := NewScope([]sec.Policy{nativePolicy(t, "a", "read", "*", cfg.Allow)}).(*scope)
	b := NewScope([]sec.Policy{nativePolicy(t, "b", "read", "*", cfg.Deny)}).(*scope)
	first := internTestSegment(interner, 1, a)
	second := internTestSegment(interner, 1, b)
	require.NotSame(t, first, second)
	require.Same(t, first, internTestSegment(interner, 1, a))
	for i := 0; i < segmentsPerShard; i++ {
		next := NewScope([]sec.Policy{nativePolicy(t, fmt.Sprintf("collision-%d", i), "read", "*", cfg.Allow)}).(*scope)
		internTestSegment(interner, 1, next)
	}
	require.NotSame(t, first, internTestSegment(interner, 1, a))
	require.Equal(t, sec.Allow, first.evaluate(sec.Actor{}, "read", "entry", nil))
	require.Equal(t, sec.Deny, second.evaluate(sec.Actor{}, "read", "entry", nil))
}
func TestPlanBoundsAndUncachedActions(t *testing.T) {
	p := nativePolicy(t, "wild", "read.*", "foo.*", cfg.Allow)
	s := NewScope([]sec.Policy{p, nativePolicy(t, "pad1", "other", "*", cfg.Allow), nativePolicy(t, "pad2", "other", "*", cfg.Allow), nativePolicy(t, "pad3", "other", "*", cfg.Allow)}).(*scope)
	for i := 0; i < maxActionPlans*3; i++ {
		action := fmt.Sprintf("read.%d", i)
		require.Equal(t, sec.Allow, s.Evaluate(sec.Actor{}, action, "foobar", nil))
	}
	require.LessOrEqual(t, len(s.segments[0].plans.Load().byAction), maxActionPlans)
	require.LessOrEqual(t, s.segments[0].plans.Load().bytes, maxSegmentPlanBytes)
	before := len(s.segments[0].plans.Load().byAction)
	for i := 0; i < 500; i++ {
		require.Equal(t, sec.Undefined, s.Evaluate(sec.Actor{}, fmt.Sprintf("unknown.%d", i), "foo", nil))
	}
	require.Equal(t, before, len(s.segments[0].plans.Load().byAction))
	require.Equal(t, sec.Allow, s.Evaluate(sec.Actor{}, "read."+strings.Repeat("x", 65536), "foo"+strings.Repeat("x", 65536), nil))
	require.Equal(t, before, len(s.segments[0].plans.Load().byAction))
	// An oversized exact union keeps the original compiled evaluation path.
	resources := make([]any, 2000)
	for i := range resources {
		resources[i] = fmt.Sprintf("entry:%d:%s", i, strings.Repeat("x", 64))
	}
	large := NewScope([]sec.Policy{nativePolicy(t, "large", "read", resources, cfg.Deny), nativePolicy(t, "large-pad1", "other", "*", cfg.Allow), nativePolicy(t, "large-pad2", "other", "*", cfg.Allow), nativePolicy(t, "large-pad3", "other", "*", cfg.Allow)}).(*scope)
	require.Equal(t, sec.Deny, large.Evaluate(sec.Actor{}, "read", resources[1999].(string), nil))
	plan, known := large.segments[0].plans.Load().byAction["read"]
	require.True(t, known)
	require.Nil(t, plan)
	require.LessOrEqual(t, large.segments[0].plans.Load().bytes, maxSegmentPlanBytes)
}
func TestConcurrentConstructionAndEvaluation(t *testing.T) {
	policies, _ := countPolicies(600, false)
	base := NewScope(policies)
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 80; i++ {
				s := NewScope(policies)
				if s != base || s.Evaluate(sec.Actor{}, "0", "entry", nil) != sec.Allow {
					t.Error("concurrent scope differs")
				}
			}
		}()
	}
	wg.Wait()
}
func BenchmarkScopeRetainedBytes(b *testing.B) {
	for _, kind := range []string{"identical", "distinct", "opaque"} {
		b.Run(kind, func(b *testing.B) {
			isolateBenchmarkInterner(b)
			policies := benchmarkPolicies(b, 600, "mixed")
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			live := make([]sec.Scope, b.N)
			b.ResetTimer()
			for i := range live {
				list := policies
				if kind == "distinct" {
					list = policies[:300+i%300]
				}
				if kind == "opaque" {
					list = append(append([]sec.Policy(nil), policies...), newMockPolicy(fmt.Sprint(i), sec.Undefined))
				}
				live[i] = NewScope(list)
				live[i].Evaluate(sec.Actor{ID: "actor"}, "registry.get", "entry:42", nil)
			}
			b.StopTimer()
			runtime.GC()
			runtime.ReadMemStats(&after)
			if after.HeapAlloc > before.HeapAlloc {
				b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/float64(b.N), "retained-B/scope")
			}
			runtime.KeepAlive(live)
			runtime.KeepAlive(policies)
		})
	}
}

func TestResidentSetRecreation(t *testing.T) {
	interner := newSegmentInterner()
	p := nativePolicy(t, "resident", "read", "*", cfg.Allow)
	original := NewScope([]sec.Policy{p}).(*scope)
	retained := internTestSegment(interner, 1, original)
	for i := 0; i < segmentsPerShard; i++ {
		s := NewScope([]sec.Policy{nativePolicy(t, fmt.Sprintf("eviction-%d", i), "read", "*", cfg.Allow)}).(*scope)
		internTestSegment(interner, 1, s)
	}
	recreated := internTestSegment(interner, 1, original)
	require.NotSame(t, retained, recreated)
	require.Equal(t, retained.evaluate(sec.Actor{}, "read", "entry", nil), recreated.evaluate(sec.Actor{}, "read", "entry", nil))
}

func BenchmarkCacheRetainedBytes(b *testing.B) {
	isolateBenchmarkInterner(b)
	var policies []sec.Policy
	actions := make([]any, maxActionPlans)
	for i := range actions {
		actions[i] = fmt.Sprintf("read.%d", i)
	}
	for i := 0; i < 4; i++ {
		resources := make([]any, 90)
		for j := range resources {
			resources[j] = fmt.Sprintf("entry:%d:%d:%s", i, j, strings.Repeat("x", 32))
		}
		p, err := impl.NewPolicy(registry.NewID("cache", fmt.Sprint(i)), &cfg.Config{Policy: cfg.Definition{Actions: actions, Resources: resources, Effect: cfg.Allow}})
		if err != nil {
			b.Fatal(err)
		}
		policies = append(policies, p)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := impl.NewPolicy(registry.NewID("function", fmt.Sprint(i)), &cfg.Config{Policy: cfg.Definition{Actions: "call", Resources: "*", Effect: cfg.Allow}})
		if err != nil {
			b.Fatal(err)
		}
		s := NewScope(append(append([]sec.Policy(nil), policies...), p))
		for action := 0; action < maxActionPlans; action++ {
			s.Evaluate(sec.Actor{}, fmt.Sprintf("read.%d", action), "missing", nil)
		}
	}
	b.StopTimer()
	runtime.GC()
	runtime.ReadMemStats(&after)
	if after.HeapAlloc > before.HeapAlloc {
		b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc), "retained-B/cache")
	}
	charged := 0
	count := 0
	for i := range sharedSegments.shards {
		if table := sharedSegments.shards[i].table.Load(); table != nil {
			for _, slot := range table.slots {
				if slot.value != nil {
					if plans := slot.value.plans.Load(); plans != nil {
						charged += plans.bytes
					}
					count++
				}
			}
		}
	}
	if charged > maxInternedSegments*maxSegmentPlanBytes {
		b.Fatal("aggregate plan budget exceeds bound")
	}
	b.ReportMetric(float64(charged), "charged-plan-B/cache")
	b.ReportMetric(float64(count), "sets/cache")
	runtime.KeepAlive(policies)
}

func BenchmarkScopeGroupCombinations(b *testing.B) {
	policies := benchmarkPolicies(b, 600, "mixed")
	functions := benchmarkPolicies(b, 64, "static")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		list := make([]sec.Policy, 0, 601)
		mask := uint(i % 1024)
		for group := uint(0); group < 10; group++ {
			if mask&(1<<group) != 0 {
				list = append(list, policies[group*60:(group+1)*60]...)
			}
		}
		list = append(list, functions[(i/1024)%len(functions)])
		benchmarkScope = NewScope(list)
		benchmarkResult = benchmarkScope.Evaluate(sec.Actor{ID: "actor"}, "registry.get", "entry:42", nil)
	}
}

func TestDuplicateInputsDoNotInflateRetainedMapping(t *testing.T) {
	p := nativePolicy(t, "duplicate-capacity", "read", "*", cfg.Allow)
	inputs := make([]sec.Policy, 65536)
	for i := range inputs {
		inputs[i] = p
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	s := NewScope(inputs)
	runtime.ReadMemStats(&after)
	require.LessOrEqual(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20), "map reservation follows the bounded unique mapping, not duplicate input length")
	require.Len(t, s.Policies(), 1)
	runtime.KeepAlive(inputs)
}

func isolateBenchmarkInterner(b *testing.B) {
	b.Helper()
	previous := sharedSegments
	isolated := newSegmentInterner()
	sharedSegments = isolated
	b.Cleanup(func() { sharedSegments = previous })
}

func TestOpaqueScopeDoesNotBuildUnknownActionPlans(t *testing.T) {
	p := mutableValuePolicy{id: registry.NewID("test", "opaque-allocation"), state: []sec.Result{sec.Allow}}
	s := NewScope([]sec.Policy{p})
	allocations := testing.AllocsPerRun(50, func() { s.Evaluate(sec.Actor{}, "read", "entry", nil) })
	require.Zero(t, allocations, "an opaque-only scope evaluates directly without constructing an uncacheable plan")
}

type denyingEmbeddedPolicy struct{ *impl.Policy }

func (p *denyingEmbeddedPolicy) Evaluate(sec.Actor, string, string, attrs.Bag) sec.Result {
	return sec.Deny
}

type denyingEmbeddedExprPolicy struct{ *impl.ExprPolicy }

func (p *denyingEmbeddedExprPolicy) Evaluate(sec.Actor, string, string, attrs.Bag) sec.Result {
	return sec.Deny
}

func TestInheritedDescriptorDoesNotDescribeWrapper(t *testing.T) {
	base, err := impl.NewPolicy(registry.NewID("test", "embedded"), &cfg.Config{Policy: cfg.Definition{Actions: "read", Resources: "*", Effect: cfg.Allow}})
	require.NoError(t, err)
	expression, err := impl.NewExprPolicy(registry.NewID("test", "embedded-expression"), &cfg.ExprConfig{Policy: cfg.ExprDefinition{Actions: "read", Resources: "*", Effect: cfg.Allow, Expression: "false"}})
	require.NoError(t, err)
	for _, wrapped := range []sec.Policy{&denyingEmbeddedPolicy{Policy: base}, &denyingEmbeddedExprPolicy{ExprPolicy: expression}} {
		s := NewScope([]sec.Policy{wrapped, nativePolicy(t, "other-authority", "write", "*", cfg.Allow)})
		for _, action := range []string{"read", "write"} {
			require.Equal(t, sec.Deny, s.Evaluate(sec.Actor{}, action, "entry", nil), "an inherited native description cannot bypass %T Evaluate", wrapped)
		}
	}
}
