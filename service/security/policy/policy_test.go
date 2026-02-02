package policy

import (
	"testing"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/service/security/policy"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		{
			name:    "exact match",
			pattern: "registry.read.document",
			value:   "registry.read.document",
			want:    true,
		},
		{
			name:    "wildcard suffix match",
			pattern: "registry.read.*",
			value:   "registry.read.document",
			want:    true,
		},
		{
			name:    "wildcard suffix with colon match",
			pattern: "functions:call.*",
			value:   "functions:call.lambda",
			want:    true,
		},
		{
			name:    "wildcard suffix with different path",
			pattern: "registry.read.*",
			value:   "registry.write.document",
			want:    false,
		},
		{
			name:    "non-wildcard no match",
			pattern: "registry.read",
			value:   "registry.read.document",
			want:    false,
		},
		{
			name:    "global wildcard",
			pattern: "*",
			value:   "anything",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesPattern(tt.pattern, tt.value)
			if got != tt.want {
				t.Errorf("matchesPattern(%q, %q) = %v, want %v",
					tt.pattern, tt.value, got, tt.want)
			}
		})
	}
}

func TestPolicy(t *testing.T) {
	policyID := registry.NewID("test", "admin-policy")
	policyConfig := &policy.Config{
		Policy: policy.Definition{
			Actions:   "*",
			Resources: "*",
			Effect:    policy.Allow,
			Conditions: []policy.Condition{
				{
					Field:    "actor.meta.role",
					Operator: "eq",
					Value:    "admin",
				},
			},
		},
	}

	p, err := NewPolicy(policyID, policyConfig)
	if err != nil {
		t.Fatalf("Failed to create policy: %v", err)
	}

	policyIDStr := p.ID()
	if policyIDStr.String() != "test:admin-policy" {
		t.Errorf("Expected policy ID to be 'test:admin-policy', got %s", policyIDStr.String())
	}

	adminActor := newMockActor("admin-user", attrs.Bag{"role": "admin"})
	result := p.Evaluate(adminActor, "read", "document", nil)
	if result != security.Allow {
		t.Errorf("Expected Allow result for admin user, got %v", result)
	}

	userActor := newMockActor("regular-user", attrs.Bag{"role": "user"})
	result = p.Evaluate(userActor, "read", "document", nil)
	if result != security.Undefined {
		t.Errorf("Expected Undefined result for regular user, got %v", result)
	}

	emptyActor := newMockActor("", nil)
	result = p.Evaluate(emptyActor, "read", "document", nil)
	if result != security.Undefined {
		t.Errorf("Expected Undefined result for empty actor, got %v", result)
	}
}

func TestPolicyWithRegexConditions(t *testing.T) {
	policyID := registry.NewID("test", "regex-policy")
	policyConfig := &policy.Config{
		Policy: policy.Definition{
			Actions:   "*",
			Resources: "*",
			Effect:    policy.Allow,
			Conditions: []policy.Condition{
				{
					Field:    "resource",
					Operator: "matches",
					Value:    "^doc.*$",
				},
			},
		},
	}

	p, err := NewPolicy(policyID, policyConfig)
	if err != nil {
		t.Fatalf("Failed to create policy: %v", err)
	}

	actor := newMockActor("user123", nil)

	result := p.Evaluate(actor, "read", "document", nil)
	if result != security.Allow {
		t.Errorf("Expected Allow result for document resource, got %v", result)
	}

	result = p.Evaluate(actor, "read", "image", nil)
	if result != security.Undefined {
		t.Errorf("Expected Undefined result for image resource, got %v", result)
	}
}

func TestPolicyWithInvalidRegex(t *testing.T) {
	policyID := registry.NewID("test", "invalid-regex-policy")
	policyConfig := &policy.Config{
		Policy: policy.Definition{
			Actions:   "*",
			Resources: "*",
			Effect:    policy.Allow,
			Conditions: []policy.Condition{
				{
					Field:    "resource",
					Operator: "matches",
					Value:    "[invalid regex",
				},
			},
		},
	}

	_, err := NewPolicy(policyID, policyConfig)
	if err == nil {
		t.Error("Expected error for policy with invalid regex pattern")
	}
}

func TestPolicyWithWildcards(t *testing.T) {
	actor := newMockActor("test-user", attrs.Bag{
		"role": "editor",
	})

	actionWildcardTests := []struct {
		name     string
		config   *policy.Config
		action   string
		resource string
		want     security.Result
	}{
		{
			name: "exact action match",
			config: &policy.Config{
				Policy: policy.Definition{
					Actions:   "registry.read.document",
					Resources: "*",
					Effect:    policy.Allow,
				},
			},
			action:   "registry.read.document",
			resource: "any",
			want:     security.Allow,
		},
		{
			name: "wildcard action match",
			config: &policy.Config{
				Policy: policy.Definition{
					Actions:   "registry.read.*",
					Resources: "*",
					Effect:    policy.Allow,
				},
			},
			action:   "registry.read.document",
			resource: "any",
			want:     security.Allow,
		},
		{
			name: "wildcard action no match",
			config: &policy.Config{
				Policy: policy.Definition{
					Actions:   "registry.read.*",
					Resources: "*",
					Effect:    policy.Allow,
				},
			},
			action:   "registry.write.document",
			resource: "any",
			want:     security.Undefined,
		},
		{
			name: "array with wildcard match",
			config: &policy.Config{
				Policy: policy.Definition{
					Actions:   []any{"registry.read.*", "registry.list.*"},
					Resources: "*",
					Effect:    policy.Allow,
				},
			},
			action:   "registry.read.document",
			resource: "any",
			want:     security.Allow,
		},
	}

	for _, tt := range actionWildcardTests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewPolicy(registry.NewID("test", "wildcard-test"), tt.config)
			if err != nil {
				t.Fatalf("Failed to create policy: %v", err)
			}

			got := p.Evaluate(actor, tt.action, tt.resource, nil)
			if got != tt.want {
				t.Errorf("Policy.Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}

	resourceWildcardTests := []struct {
		name     string
		config   *policy.Config
		action   string
		resource string
		want     security.Result
	}{
		{
			name: "exact resource match",
			config: &policy.Config{
				Policy: policy.Definition{
					Actions:   "*",
					Resources: "document:financial",
					Effect:    policy.Allow,
				},
			},
			action:   "read",
			resource: "document:financial",
			want:     security.Allow,
		},
		{
			name: "wildcard resource match",
			config: &policy.Config{
				Policy: policy.Definition{
					Actions:   "*",
					Resources: "document:*",
					Effect:    policy.Allow,
				},
			},
			action:   "read",
			resource: "document:financial",
			want:     security.Allow,
		},
		{
			name: "wildcard resource no match",
			config: &policy.Config{
				Policy: policy.Definition{
					Actions:   "*",
					Resources: "document:*",
					Effect:    policy.Allow,
				},
			},
			action:   "read",
			resource: "image:logo",
			want:     security.Undefined,
		},
		{
			name: "array with wildcard match",
			config: &policy.Config{
				Policy: policy.Definition{
					Actions:   "*",
					Resources: []any{"document:*", "image:*"},
					Effect:    policy.Allow,
				},
			},
			action:   "read",
			resource: "document:financial",
			want:     security.Allow,
		},
	}

	for _, tt := range resourceWildcardTests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewPolicy(registry.NewID("test", "wildcard-test"), tt.config)
			if err != nil {
				t.Fatalf("Failed to create policy: %v", err)
			}

			got := p.Evaluate(actor, tt.action, tt.resource, nil)
			if got != tt.want {
				t.Errorf("Policy.Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPolicyWithComplexRegexConditions(t *testing.T) {
	policyID := registry.NewID("test", "complex-regex-policy")
	policyConfig := &policy.Config{
		Policy: policy.Definition{
			Actions:   "*",
			Resources: "*",
			Effect:    policy.Allow,
			Conditions: []policy.Condition{
				{
					Field:    "actor.meta.email",
					Operator: "matches",
					Value:    "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$",
				},
				{
					Field:    "resource",
					Operator: "matches",
					Value:    "^(document|file):[a-z]+$",
				},
			},
		},
	}

	p, err := NewPolicy(policyID, policyConfig)
	if err != nil {
		t.Fatalf("Failed to create policy: %v", err)
	}

	tests := []struct {
		name     string
		actor    security.Actor
		action   string
		resource string
		want     security.Result
	}{
		{
			name: "valid email and resource",
			actor: newMockActor("user123", attrs.Bag{
				"email": "user@example.com",
			}),
			action:   "read",
			resource: "document:financial",
			want:     security.Allow,
		},
		{
			name: "invalid email format",
			actor: newMockActor("user123", attrs.Bag{
				"email": "invalid-email",
			}),
			action:   "read",
			resource: "document:financial",
			want:     security.Undefined,
		},
		{
			name: "valid email but invalid resource",
			actor: newMockActor("user123", attrs.Bag{
				"email": "user@example.com",
			}),
			action:   "read",
			resource: "image:logo",
			want:     security.Undefined,
		},
		{
			name: "file resource matches",
			actor: newMockActor("user123", attrs.Bag{
				"email": "user@example.com",
			}),
			action:   "read",
			resource: "file:data",
			want:     security.Allow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.Evaluate(tt.actor, tt.action, tt.resource, nil)
			if got != tt.want {
				t.Errorf("Policy.Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPolicyPrecompilationEfficiency(t *testing.T) {
	conditions := []policy.Condition{
		{
			Field:    "resource",
			Operator: "matches",
			Value:    "^doc.*$",
		},
		{
			Field:    "actor.meta.email",
			Operator: "matches",
			Value:    "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$",
		},
		{
			Field:    "resource",
			Operator: "matches",
			Value:    "^doc.*$",
		},
	}

	policyConfig := &policy.Config{
		Policy: policy.Definition{
			Actions:    "*",
			Resources:  "*",
			Effect:     policy.Allow,
			Conditions: conditions,
		},
	}

	p, err := NewPolicy(registry.NewID("test", "efficiency-test"), policyConfig)
	if err != nil {
		t.Fatalf("Failed to create policy: %v", err)
	}

	if len(p.evaluator.compiledPatterns) != 2 {
		t.Errorf("Expected 2 unique compiled patterns, got %d", len(p.evaluator.compiledPatterns))
	}

	if _, exists := p.evaluator.compiledPatterns["^doc.*$"]; !exists {
		t.Error("Pattern '^doc.*$' should be pre-compiled")
	}

	if _, exists := p.evaluator.compiledPatterns["^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"]; !exists {
		t.Error("Email pattern should be pre-compiled")
	}
}
