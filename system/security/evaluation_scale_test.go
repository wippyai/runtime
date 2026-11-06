// SPDX-License-Identifier: MPL-2.0

package security

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand"
	"strings"
	"sync/atomic"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	sec "github.com/wippyai/runtime/api/security"
	cfg "github.com/wippyai/runtime/api/service/security/policy"
	impl "github.com/wippyai/runtime/service/security/policy"
)

// referenceMatch and referenceScope retain the original uncompiled algorithm.
func referencePattern(pattern, value string) bool {
	if pattern == "*" || pattern == value {
		return true
	}
	if len(pattern) > 2 && pattern[len(pattern)-2:] == ".*" {
		prefix := pattern[:len(pattern)-2]
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}
	if len(pattern) > 1 && pattern[len(pattern)-1:] == "*" {
		prefix := pattern[:len(pattern)-1]
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}
	return false
}
func referenceMatch(filter any, value string) bool {
	switch f := filter.(type) {
	case string:
		return referencePattern(f, value)
	case []any:
		for _, v := range f {
			if s, ok := v.(string); ok && referencePattern(s, value) {
				return true
			}
		}
	case []string:
		for _, s := range f {
			if referencePattern(s, value) {
				return true
			}
		}
	}
	return false
}

type referencePolicy struct {
	evaluator          *impl.ConditionEvaluator
	expression         *impl.ExprEvaluator
	actions, resources any
	id                 registry.ID
	conditions         []cfg.Condition
	effect             sec.Result
}

func (p *referencePolicy) ID() registry.ID { return p.id }
func (p *referencePolicy) Evaluate(actor sec.Actor, action, resource string, meta attrs.Bag) sec.Result {
	if !referenceMatch(p.actions, action) || !referenceMatch(p.resources, resource) {
		return sec.Undefined
	}
	for _, c := range p.conditions {
		match, err := p.evaluator.EvaluateCondition(c, actor, action, resource, meta)
		if err != nil || !match {
			return sec.Undefined
		}
	}
	if p.expression != nil {
		match, err := p.expression.Evaluate(map[string]any{"actor": actor, "action": action, "resource": resource, "meta": meta})
		if err != nil || !match {
			return sec.Undefined
		}
	}
	return p.effect
}
func referenceScope(policies []sec.Policy, actor sec.Actor, action, resource string, meta attrs.Bag) sec.Result {
	mapping := map[registry.ID]sec.Policy{}
	for _, p := range policies {
		mapping[p.ID()] = p
	}
	result := sec.Undefined
	for _, p := range mapping {
		r := p.Evaluate(actor, action, resource, meta)
		if r == sec.Deny {
			return r
		}
		if r != sec.Undefined {
			result = r
		}
	}
	return result
}
func TestEvaluationDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(94731))
	filters := []any{"*", "read", "read.*", "foo*", ".*", []any{"read", "write", 42, nil}, []any{"item:one", "item:two", "foo.*"}, []any{false}}
	values := []string{"", "read", "reader", "read.x", "write", "foo", "foobar", "item:one", "item:two", "missing"}
	for trial := 0; trial < 180; trial++ {
		var actual, oracle []sec.Policy
		for i := 0; i < 1+rng.Intn(35); i++ {
			id := registry.NewID("test", fmt.Sprint(rng.Intn(25)))
			actions := filters[rng.Intn(len(filters))]
			resources := filters[rng.Intn(len(filters))]
			effect := cfg.Allow
			result := sec.Allow
			if rng.Intn(3) == 0 {
				effect = cfg.Deny
				result = sec.Deny
			}
			ref := &referencePolicy{id: id, actions: actions, resources: resources, effect: result}
			var p sec.Policy
			var err error
			if rng.Intn(5) == 0 {
				expression := []string{`actor.ID == "a"`, `meta.allowed == true`, `int(meta.number) > 0`, `true`}[rng.Intn(4)]
				ref.expression, err = impl.NewExprEvaluator(expression)
				require.NoError(t, err)
				p, err = impl.NewExprPolicy(id, &cfg.ExprConfig{Policy: cfg.ExprDefinition{Actions: actions, Resources: resources, Effect: effect, Expression: expression}})
			} else {
				if rng.Intn(3) == 0 {
					ref.conditions = []cfg.Condition{{Field: []string{"actor.id", "meta.owner", "meta.number"}[rng.Intn(3)], Operator: []string{"eq", "gt"}[rng.Intn(2)], Value: "a"}}
				}
				ref.evaluator, err = impl.NewConditionEvaluator(ref.conditions)
				require.NoError(t, err)
				p, err = impl.NewPolicy(id, &cfg.Config{Policy: cfg.Definition{Actions: actions, Resources: resources, Effect: effect, Conditions: ref.conditions}})
			}
			require.NoError(t, err)
			actual = append(actual, p)
			oracle = append(oracle, ref)
		}
		s := NewScope(actual)
		for check := 0; check < 90; check++ {
			actor := sec.Actor{ID: []string{"a", "b", ""}[rng.Intn(3)]}
			meta := attrs.Bag{"allowed": rng.Intn(2) == 0, "owner": actor.ID, "number": []any{1, "bad", nil}[rng.Intn(3)]}
			action, resource := values[rng.Intn(len(values))], values[rng.Intn(len(values))]
			require.Equal(t, referenceScope(oracle, actor, action, resource, meta), s.Evaluate(actor, action, resource, meta), "trial=%d action=%q resource=%q", trial, action, resource)
		}
	}
}

// These methods also implement the optional descriptor contract on the old code.
type countingPolicy struct {
	id                  registry.ID
	action              string
	token               uint64
	calls, matches, ids atomic.Int64
	dynamic             bool
}

func (p *countingPolicy) ID() registry.ID { p.ids.Add(1); return p.id }
func (p *countingPolicy) PolicyIdentity(candidate sec.Policy) uint64 {
	own, ok := candidate.(*countingPolicy)
	if !ok || own != p {
		return 0
	}
	return p.token
}
func (p *countingPolicy) AppliesToAction(action string) bool {
	p.matches.Add(1)
	return p.action == action
}
func (p *countingPolicy) ResourceFilter() (bool, []string, []string) {
	return false, []string{"entry"}, nil
}
func (p *countingPolicy) StaticEffect() (sec.Result, bool) { return sec.Allow, !p.dynamic }
func (p *countingPolicy) Evaluate(_ sec.Actor, action, resource string, _ attrs.Bag) sec.Result {
	p.calls.Add(1)
	if p.AppliesToAction(action) && resource == "entry" {
		return sec.Allow
	}
	return sec.Undefined
}

var testIdentities atomic.Uint64

func countPolicies(n int, dynamic bool) ([]sec.Policy, []*countingPolicy) {
	policies := make([]sec.Policy, n)
	counters := make([]*countingPolicy, n)
	for i := range policies {
		p := &countingPolicy{id: registry.NewID("count", fmt.Sprint(i)), token: (uint64(1) << 63) + testIdentities.Add(1), action: fmt.Sprint(i), dynamic: dynamic}
		policies[i] = p
		counters[i] = p
	}
	return policies, counters
}
func TestScopeSharedSet(t *testing.T) {
	policies, _ := countPolicies(500, false)
	a := NewScope(policies)
	reversed := append([]sec.Policy(nil), policies...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	require.True(t, a == NewScope(reversed), "identical ID-to-identity mappings share the resident set")
}
func TestScopePlanOperationCounts(t *testing.T) {
	for _, dynamic := range []bool{false, true} {
		t.Run(fmt.Sprint(dynamic), func(t *testing.T) {
			policies, counts := countPolicies(600, dynamic)
			s := NewScope(policies)
			require.Equal(t, sec.Allow, s.Evaluate(sec.Actor{}, "0", "entry", nil))
			for _, p := range counts {
				p.calls.Store(0)
				p.matches.Store(0)
			}
			for i := 0; i < 100; i++ {
				require.Equal(t, sec.Allow, s.Evaluate(sec.Actor{}, "0", "entry", nil))
			}
			for i, p := range counts {
				expected := int64(0)
				if dynamic && i == 0 {
					expected = 100
				}
				require.Equal(t, expected, p.calls.Load(), "policy %d", i)
				if i != 0 || !dynamic {
					require.Zero(t, p.matches.Load(), "static exact checks and noncandidates do no per-policy work")
				}
			}
		})
	}
}
func TestScopeAssemblyLinear(t *testing.T) {
	policies, counts := countPolicies(600, false)
	NewScope(policies)
	for _, p := range counts {
		require.EqualValues(t, 1, p.ids.Load(), "each input is read once")
	}
	// Lua must collect the list before construction; a With loop copies each prefix.
	f, err := parser.ParseFile(token.NewFileSet(), "../../runtime/lua/modules/security/module.go", nil, 0)
	require.NoError(t, err)
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "newScope" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					require.NotEqual(t, "With", sel.Sel.Name, "Lua list assembly constructs one scope")
				}
			}
			return true
		})
	}
}

func benchmarkPolicies(b *testing.B, n int, kind string) []sec.Policy {
	b.Helper()
	policies := make([]sec.Policy, n)
	for i := range policies {
		// The tail and total budgets keep the fixture near 3.3 actions and 3.5
		// resources, including wildcard resources and the longest lists.
		tail := max(1, n*6/100)
		actionBase := n - tail
		actionThrees := n*33/10 - 53 - (tail-1)*9 - 2*actionBase
		actionCount := 2
		if i < actionThrees {
			actionCount = 3
		}
		if i >= actionBase {
			actionCount = 9
		}
		if i == n-1 {
			actionCount = 53
		}
		actions := make([]any, actionCount)
		for j := range actions {
			actions[j] = fmt.Sprintf("other.%d.%d", i, j)
		}
		if i%3 == 0 {
			actions[0] = "registry.get"
		}
		wildcards := (n + 4) / 5
		resourceBase := n - wildcards - tail
		resourceFours := n*35/10 - wildcards - 53 - (tail-1)*13 - 3*resourceBase
		resourceIndex := i - (i+4)/5
		resourceCount := 3
		if resourceIndex < resourceFours {
			resourceCount = 4
		}
		if resourceIndex >= resourceBase {
			resourceCount = 13
		}
		if i == n-1 {
			resourceCount = 53
		}
		list := make([]any, resourceCount)
		for j := range list {
			list[j] = fmt.Sprintf("entry:%d", i+j)
		}
		resources := any(list)
		if i%5 == 0 {
			resources = "*"
		}

		if kind == "prefix" {
			resources = []any{fmt.Sprintf("entry:%d.*", i), fmt.Sprintf("other:%d*", i)}
		}
		if kind == "large" {
			list := make([]any, 256)
			for j := range list {
				list[j] = fmt.Sprintf("entry:%d:%d", i, j)
			}
			resources = list
		}
		id := registry.NewID("bench", fmt.Sprint(i))
		var p sec.Policy
		var err error
		if kind == "expression" && i%30 == 0 {
			p, err = impl.NewExprPolicy(id, &cfg.ExprConfig{Policy: cfg.ExprDefinition{Actions: actions, Resources: resources, Effect: cfg.Allow, Expression: `actor.ID != ""`}})
		} else {
			var conditions []cfg.Condition
			if (kind == "condition" || kind == "mixed") && i%30 == 0 {
				conditions = []cfg.Condition{{Field: "actor.id", Operator: "eq", Value: "actor"}}
			}
			p, err = impl.NewPolicy(id, &cfg.Config{Policy: cfg.Definition{Actions: actions, Resources: resources, Effect: cfg.Allow, Conditions: conditions}})
		}
		if err != nil {
			b.Fatal(err)
		}
		policies[i] = p
	}
	return policies
}

var benchmarkScope sec.Scope
var benchmarkResult sec.Result

func BenchmarkEvaluationScale(b *testing.B) {
	for _, n := range []int{400, 600} {
		for _, kind := range []string{"static", "mixed", "prefix", "large", "condition", "expression"} {
			b.Run(fmt.Sprintf("%d/%s", n, kind), func(b *testing.B) {
				policies := benchmarkPolicies(b, n, kind)
				s := NewScope(policies)
				actor := sec.Actor{ID: "actor"}
				s.Evaluate(actor, "registry.get", "entry:42", nil)
				b.Run("check", func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						benchmarkResult = s.Evaluate(actor, "registry.get", "entry:42", nil)
					}
				})
				if kind == "mixed" {
					for _, size := range []int{1500, 5400} {
						resources := make([]string, size)
						for i := range resources {
							resources[i] = fmt.Sprintf("entry:%d", i)
						}
						b.Run(fmt.Sprintf("scan%d", size), func(b *testing.B) {
							b.ReportAllocs()
							for i := 0; i < b.N; i++ {
								for _, resource := range resources {
									benchmarkResult = s.Evaluate(actor, "registry.get", resource, nil)
								}
							}
						})
					}
				}
				b.Run("assemble", func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						benchmarkScope = NewScope(policies)
					}
				})
			})
		}
	}
}
func BenchmarkScopePopulations(b *testing.B) {
	policies := benchmarkPolicies(b, 500, "mixed")
	for _, kind := range []string{"identical-users", "distinct-sets", "unique-restriction"} {
		b.Run(kind, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				list := policies
				if kind == "distinct-sets" {
					list = policies[:250+i%250]
				}
				if kind == "unique-restriction" {
					list = append(append([]sec.Policy(nil), policies...), newMockPolicy(fmt.Sprint(i), sec.Undefined))
				}
				benchmarkScope = NewScope(list)
				benchmarkResult = benchmarkScope.Evaluate(sec.Actor{ID: fmt.Sprint(i)}, "registry.get", "entry:42", nil)
			}
		})
	}
}
func TestReferenceMatchingEdges(t *testing.T) {
	for _, filter := range []any{"*", "foo.*", "foo*", ".*", "", []any{nil, 42, "foo.*"}, []string{"foo.*"}} {
		for _, value := range []string{"", "foo", "foobar", "foo.bar", "bar", strings.Repeat("f", 16384)} {
			p, err := impl.NewExprPolicy(registry.NewID("test", "edges"), &cfg.ExprConfig{Policy: cfg.ExprDefinition{Actions: "*", Resources: filter, Effect: cfg.Allow, Expression: "true"}})
			require.NoError(t, err)
			expected := sec.Undefined
			if referenceMatch(filter, value) {
				expected = sec.Allow
			}
			require.Equal(t, expected, NewScope([]sec.Policy{p}).Evaluate(sec.Actor{}, "read", value, nil))
		}
	}
}

func BenchmarkScopeBoundary(b *testing.B) {
	for _, n := range []int{400, 600} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			policies := benchmarkPolicies(b, n, "mixed")
			reg := NewPolicyRegistry(nil, nil)
			ids := make([]registry.ID, len(policies))
			for i, p := range policies {
				ids[i] = p.ID()
				reg.policies.Store(ids[i], &sec.PolicyEntry{Policy: p})
			}
			ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
			defer ctxapi.ReleaseFrameContext(frame)
			ctx = sec.WithRegistry(ctx, reg)
			if err := sec.SetScope(ctx, NewScope(policies[:n/2])); err != nil {
				b.Fatal(err)
			}
			config := &sec.Config{Policies: ids[n/2:]}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				pairs, err := ResolveConfigPairs(ctx, config)
				if err != nil {
					b.Fatal(err)
				}
				_ = pairs
			}
		})
	}
}
