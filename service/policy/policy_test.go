package policy

import (
	"testing"

	"github.com/ponyruntime/pony/api/service/policy"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
)

// Test pattern matching function
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

// Tests for Policy implementation
func TestPolicy(t *testing.T) {
	// Create a test policy
	policyID := registry.ID{NS: "test", Name: "admin-policy"}
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

	// Check policy ID
	if p.ID().String() != "test:admin-policy" {
		t.Errorf("Expected policy ID to be 'test:admin-policy', got %s", p.ID().String())
	}

	// Test policy evaluation with matching role
	adminActor := security.Actor{
		ID:   "admin-user",
		Meta: registry.Metadata{"role": "admin"},
	}
	result := p.Evaluate(adminActor, "read", "document", nil)
	if result != security.Allow {
		t.Errorf("Expected Allow result for admin user, got %v", result)
	}

	// Test policy evaluation with non-matching role
	userActor := &mockActor{
		id:   "regular-user",
		meta: registry.Metadata{"role": "user"},
	}
	result = p.Evaluate(userActor, "read", "document", nil)
	if result != security.Undefined {
		t.Errorf("Expected Undefined result for regular user, got %v", result)
	}

	// Test with nil actor
	result = p.Evaluate(nil, "read", "document", nil)
	if result != security.Undefined {
		t.Errorf("Expected Undefined result for nil actor, got %v", result)
	}
}

// Test policy with wildcard patterns
func TestPolicyWithWildcards(t *testing.T) {
	// Create a test actor
	actor := &mockActor{
		id: "test-user",
		meta: registry.Metadata{
			"role": "editor",
		},
	}

	// Test cases for action wildcards
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
			p, err := NewPolicy(registry.ID{NS: "test", Name: "wildcard-test"}, tt.config)
			if err != nil {
				t.Fatalf("Failed to create policy: %v", err)
			}

			got := p.Evaluate(actor, tt.action, tt.resource, nil)
			if got != tt.want {
				t.Errorf("Policy.Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}

	// Test cases for resource wildcards
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
			p, err := NewPolicy(registry.ID{NS: "test", Name: "wildcard-test"}, tt.config)
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
