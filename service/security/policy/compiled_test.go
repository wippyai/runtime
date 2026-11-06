// SPDX-License-Identifier: MPL-2.0

package policy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	cfg "github.com/wippyai/runtime/api/service/security/policy"
)

func TestCompiledFilterMatchesOriginal(t *testing.T) {
	filters := []any{nil, 12, "", "*", "foo", "foo.*", "foo*", ".*", "a**", []any{42, nil, "foo.*", "other"}, []string{"foo.*", "exact", ""}, []any{false}}
	values := []string{"", "foo", "foobar", "foo.bar", "other", "exact", ".*", "a*", strings.Repeat("f", 65536)}
	for _, value := range filters {
		compiled := compileFilter(value)
		for _, input := range values {
			require.Equal(t, matchesFilter(value, input), compiled.matches(input), "filter=%v value=%q", value, input)
		}
	}
}
func TestPolicyDetachedConfig(t *testing.T) {
	actions := []any{"read"}
	resources := []any{"entry"}
	values := []any{"a"}
	config := &cfg.Config{Policy: cfg.Definition{Actions: actions, Resources: resources, Effect: cfg.Allow, Conditions: []cfg.Condition{{Field: "actor.id", Operator: "in", Value: values}}}}
	p, err := NewPolicy(registry.NewID("test", "detached"), config)
	require.NoError(t, err)
	actions[0] = "write"
	resources[0] = "other"
	values[0] = "b"
	config.Policy.Effect = cfg.Deny
	config.Policy.Conditions[0].Field = "resource"
	require.Equal(t, security.Allow, p.Evaluate(security.Actor{ID: "a"}, "read", "entry", nil))
	require.Equal(t, security.Undefined, p.Evaluate(security.Actor{ID: "b"}, "read", "entry", nil))
	_, actionNames, _ := p.ActionFilter()
	actionNames[0] = "changed"
	require.True(t, p.AppliesToAction("read"))
	_, exact, prefixes := p.ResourceFilter()
	exact[0] = "changed"
	_ = prefixes
	require.Equal(t, security.Allow, p.Evaluate(security.Actor{ID: "a"}, "read", "entry", nil))
}
func TestExprDetachedConfigAndChangingInputs(t *testing.T) {
	actions := []string{"read"}
	resources := []string{"foo.*"}
	config := &cfg.ExprConfig{Policy: cfg.ExprDefinition{Actions: actions, Resources: resources, Effect: cfg.Allow, Expression: `actor.ID == meta.owner`}}
	p, err := NewExprPolicy(registry.NewID("test", "expr-detached"), config)
	require.NoError(t, err)
	actions[0] = "write"
	resources[0] = "other"
	config.Policy.Effect = cfg.Deny
	config.Policy.Expression = "false"
	require.Equal(t, security.Allow, p.Evaluate(security.Actor{ID: "a"}, "read", "foobar", attrs.Bag{"owner": "a"}))
	require.Equal(t, security.Undefined, p.Evaluate(security.Actor{ID: "b"}, "read", "foobar", attrs.Bag{"owner": "a"}))
	_, actionNames, _ := p.ActionFilter()
	actionNames[0] = "changed"
	require.True(t, p.AppliesToAction("read"))
	_, static := p.StaticEffect()
	require.False(t, static)
}
func TestPolicyIdentityReplacement(t *testing.T) {
	config := &cfg.Config{Policy: cfg.Definition{Actions: "*", Resources: "*", Effect: cfg.Allow}}
	a, err := NewPolicy(registry.NewID("test", "identity"), config)
	require.NoError(t, err)
	b, err := NewPolicy(a.ID(), config)
	require.NoError(t, err)
	require.NotZero(t, a.PolicyIdentity(a))
	require.NotEqual(t, a.PolicyIdentity(a), b.PolicyIdentity(b))
}

func TestPolicyIdentityBoundToConstructedObject(t *testing.T) {
	p, err := NewPolicy(registry.NewID("test", "copy"), &cfg.Config{
		Policy: cfg.Definition{Actions: "*", Resources: "*", Effect: cfg.Allow},
	})
	require.NoError(t, err)
	copyOfPolicy := *p
	expr, err := NewExprPolicy(p.ID(), &cfg.ExprConfig{
		Policy: cfg.ExprDefinition{Actions: "*", Resources: "*", Effect: cfg.Allow, Expression: "true"},
	})
	require.NoError(t, err)
	copyOfExpr := *expr
	for _, test := range []struct {
		original security.Policy
		copy     security.Policy
		name     string
	}{
		{name: "policy", original: p, copy: &copyOfPolicy},
		{name: "expression", original: expr, copy: &copyOfExpr},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := test.original.(security.PolicyDescriptor)
			copied := test.copy.(security.PolicyDescriptor)
			require.NotZero(t, original.PolicyIdentity(test.original))
			require.Zero(t, copied.PolicyIdentity(test.copy))
			require.Zero(t, copied.PolicyIdentity(test.original))
			require.Zero(t, original.PolicyIdentity(test.copy))
			require.Equal(t, security.Allow, test.copy.Evaluate(security.Actor{}, "read", "entry", nil))
		})
	}
}

func TestDetachedConditionCollectionsPreserveCycles(t *testing.T) {
	source := make([]any, 2)
	source[0], source[1] = source, "original"
	cloned := cloneConditionValue(source).([]any)
	cloned[0].([]any)[1] = "changed"
	require.Equal(t, "changed", cloned[1])
	require.Equal(t, "original", source[1])
	mapping := map[string]any{"value": "original"}
	mapping["self"] = mapping
	copied := cloneConditionValue(mapping).(map[string]any)
	copied["self"].(map[string]any)["value"] = "changed"
	require.Equal(t, "changed", copied["value"])
	require.Equal(t, "original", mapping["value"])
}
