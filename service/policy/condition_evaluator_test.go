package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/api/service/policy"
)

func newMockActor(id string, meta registry.Metadata) security.Actor {
	return security.Actor{ID: id, Meta: meta}
}

func TestEvaluateCondition(t *testing.T) {
	evaluator, err := NewConditionEvaluator([]policy.Condition{})
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	tests := []struct {
		name      string
		condition policy.Condition
		actor     security.Actor
		action    string
		resource  string
		meta      registry.Metadata
		want      bool
		wantErr   bool
	}{
		{
			name: "simple equality - true",
			condition: policy.Condition{
				Field:    "action",
				Operator: "eq",
				Value:    "read",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "simple equality - false",
			condition: policy.Condition{
				Field:    "action",
				Operator: "eq",
				Value:    "write",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     false,
			wantErr:  false,
		},
		{
			name: "actor field access",
			condition: policy.Condition{
				Field:    "actor.id",
				Operator: "eq",
				Value:    "user123",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "actor metadata access",
			condition: policy.Condition{
				Field:    "actor.meta.role",
				Operator: "eq",
				Value:    "admin",
			},
			actor:    newMockActor("user123", registry.Metadata{"role": "admin"}),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "direct metadata access",
			condition: policy.Condition{
				Field:    "meta.owner",
				Operator: "eq",
				Value:    "user123",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{"owner": "user123"},
			want:     true,
			wantErr:  false,
		},
		{
			name: "nested metadata access",
			condition: policy.Condition{
				Field:    "meta.document.owner",
				Operator: "eq",
				Value:    "user123",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{"document": map[string]any{"owner": "user123"}},
			want:     true,
			wantErr:  false,
		},
		{
			name: "deeply nested metadata access",
			condition: policy.Condition{
				Field:    "meta.document.permissions.read",
				Operator: "eq",
				Value:    true,
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{"document": map[string]any{"permissions": map[string]any{"read": true}}},
			want:     true,
			wantErr:  false,
		},
		{
			name: "value_from field",
			condition: policy.Condition{
				Field:     "meta.owner",
				Operator:  "eq",
				ValueFrom: "actor.id",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{"owner": "user123"},
			want:     true,
			wantErr:  false,
		},
		{
			name: "exists operator - true",
			condition: policy.Condition{
				Field:    "meta.owner",
				Operator: "exists",
				Value:    true,
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{"owner": "user123"},
			want:     true,
			wantErr:  false,
		},
		{
			name: "exists operator - false",
			condition: policy.Condition{
				Field:    "meta.owner",
				Operator: "exists",
				Value:    false,
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{},
			want:     true,
			wantErr:  false,
		},
		{
			name: "nexists operator - true when field missing",
			condition: policy.Condition{
				Field:    "meta.missing",
				Operator: "nexists",
				Value:    true,
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{},
			want:     true,
			wantErr:  false,
		},
		{
			name: "nexists operator - false when field present",
			condition: policy.Condition{
				Field:    "meta.owner",
				Operator: "nexists",
				Value:    true,
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{"owner": "user123"},
			want:     false,
			wantErr:  false,
		},
		{
			name: "not equals operator",
			condition: policy.Condition{
				Field:    "action",
				Operator: "ne",
				Value:    "write",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "less than operator",
			condition: policy.Condition{
				Field:    "meta.age",
				Operator: "lt",
				Value:    30,
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{"age": 25},
			want:     true,
			wantErr:  false,
		},
		{
			name: "greater than operator",
			condition: policy.Condition{
				Field:    "meta.age",
				Operator: "gt",
				Value:    20,
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{"age": 25},
			want:     true,
			wantErr:  false,
		},
		{
			name: "less than or equal operator",
			condition: policy.Condition{
				Field:    "meta.age",
				Operator: "lte",
				Value:    25,
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{"age": 25},
			want:     true,
			wantErr:  false,
		},
		{
			name: "greater than or equal operator",
			condition: policy.Condition{
				Field:    "meta.age",
				Operator: "gte",
				Value:    25,
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{"age": 25},
			want:     true,
			wantErr:  false,
		},
		{
			name: "in operator with array",
			condition: policy.Condition{
				Field:    "action",
				Operator: "in",
				Value:    []any{"read", "list", "view"},
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "in operator with string array",
			condition: policy.Condition{
				Field:    "action",
				Operator: "in",
				Value:    []string{"read", "list", "view"},
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "in operator - single value match",
			condition: policy.Condition{
				Field:    "action",
				Operator: "in",
				Value:    "read",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "in operator - false",
			condition: policy.Condition{
				Field:    "action",
				Operator: "in",
				Value:    []any{"write", "update", "delete"},
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     false,
			wantErr:  false,
		},
		{
			name: "nin operator - not in list true",
			condition: policy.Condition{
				Field:    "action",
				Operator: "nin",
				Value:    []any{"write", "delete"},
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "nin operator - in list false",
			condition: policy.Condition{
				Field:    "action",
				Operator: "nin",
				Value:    []string{"read", "list"},
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     false,
			wantErr:  false,
		},
		{
			name: "contains operator with string",
			condition: policy.Condition{
				Field:    "resource",
				Operator: "contains",
				Value:    "doc",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "contains operator with array",
			condition: policy.Condition{
				Field:    "meta.tags",
				Operator: "contains",
				Value:    "important",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{"tags": []string{"public", "important", "archived"}},
			want:     true,
			wantErr:  false,
		},
		{
			name: "ncontains operator with string",
			condition: policy.Condition{
				Field:    "resource",
				Operator: "ncontains",
				Value:    "x",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "ncontains operator with array",
			condition: policy.Condition{
				Field:    "meta.tags",
				Operator: "ncontains",
				Value:    "missing",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     registry.Metadata{"tags": []string{"public", "important", "archived"}},
			want:     true,
			wantErr:  false,
		},
		{
			name: "invalid field path",
			condition: policy.Condition{
				Field:    "",
				Operator: "eq",
				Value:    "read",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     false,
			wantErr:  false,
		},
		{
			name: "invalid operator",
			condition: policy.Condition{
				Field:    "action",
				Operator: "invalid",
				Value:    "read",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     false,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluator.EvaluateCondition(tt.condition, tt.actor, tt.action, tt.resource, tt.meta)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateCondition() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateCondition() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegexPrecompilation(t *testing.T) {
	conditions := []policy.Condition{
		{
			Field:    "resource",
			Operator: "matches",
			Value:    "^doc.*$",
		},
		{
			Field:    "resource",
			Operator: "matches",
			Value:    "^doc[a-z]+(ent)?$",
		},
		{
			Field:    "resource",
			Operator: "nmatches",
			Value:    "^skip.*$",
		},
		{
			Field:    "action",
			Operator: "eq",
			Value:    "read",
		},
	}

	evaluator, err := NewConditionEvaluator(conditions)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	if len(evaluator.compiledPatterns) != 3 {
		t.Errorf("Expected 3 compiled patterns, got %d", len(evaluator.compiledPatterns))
	}

	if _, exists := evaluator.compiledPatterns["^doc.*$"]; !exists {
		t.Error("Pattern '^doc.*$' should be pre-compiled")
	}

	if _, exists := evaluator.compiledPatterns["^doc[a-z]+(ent)?$"]; !exists {
		t.Error("Pattern '^doc[a-z]+(ent)?$' should be pre-compiled")
	}
	if _, exists := evaluator.compiledPatterns["^skip.*$"]; !exists {
		t.Error("Pattern '^skip.*$' should be pre-compiled")
	}

	tests := []struct {
		name      string
		condition policy.Condition
		actor     security.Actor
		action    string
		resource  string
		meta      registry.Metadata
		want      bool
		wantErr   bool
	}{
		{
			name: "matches operator with pre-compiled pattern",
			condition: policy.Condition{
				Field:    "resource",
				Operator: "matches",
				Value:    "^doc.*$",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "matches operator with complex pre-compiled pattern",
			condition: policy.Condition{
				Field:    "resource",
				Operator: "matches",
				Value:    "^doc[a-z]+(ent)?$",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
		{
			name: "matches operator with non-pre-compiled pattern should fail",
			condition: policy.Condition{
				Field:    "resource",
				Operator: "matches",
				Value:    "^new.*$",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     false,
			wantErr:  true,
		},
		{
			name: "nmatches operator with pre-compiled pattern",
			condition: policy.Condition{
				Field:    "resource",
				Operator: "nmatches",
				Value:    "^skip.*$",
			},
			actor:    newMockActor("user123", nil),
			action:   "read",
			resource: "document",
			meta:     nil,
			want:     true,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluator.EvaluateCondition(tt.condition, tt.actor, tt.action, tt.resource, tt.meta)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateCondition() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateCondition() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegexPrecompilationErrors(t *testing.T) {
	conditions := []policy.Condition{
		{
			Field:    "resource",
			Operator: "matches",
			Value:    "[invalid regex",
		},
	}

	_, err := NewConditionEvaluator(conditions)
	if err == nil {
		t.Error("Expected error for invalid regex pattern")
	}
}

func TestExtractField(t *testing.T) {
	evaluator, err := NewConditionEvaluator([]policy.Condition{})
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	actor := newMockActor("user123", registry.Metadata{
		"role":    "admin",
		"profile": map[string]any{"name": "John", "email": "john@example.com"},
	})
	meta := registry.Metadata{
		"owner":   "user456",
		"created": 1635724800,
		"tags":    []string{"public", "important"},
		"document": map[string]any{
			"id":      "doc123",
			"version": 2,
			"content": map[string]any{"title": "Test Document"},
		},
	}

	tests := []struct {
		name      string
		fieldPath string
		actor     security.Actor
		action    string
		resource  string
		meta      registry.Metadata
		want      any
		wantErr   bool
	}{
		{
			name:      "empty field path",
			fieldPath: "",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      nil,
			wantErr:   false,
		},
		{
			name:      "direct actor field",
			fieldPath: "actor.id",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      "user123",
			wantErr:   false,
		},
		{
			name:      "actor meta field",
			fieldPath: "actor.meta.role",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      "admin",
			wantErr:   false,
		},
		{
			name:      "actor nested meta field",
			fieldPath: "actor.meta.profile.name",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      "John",
			wantErr:   false,
		},
		{
			name:      "direct meta field",
			fieldPath: "meta.owner",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      "user456",
			wantErr:   false,
		},
		{
			name:      "nested meta field",
			fieldPath: "meta.document.id",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      "doc123",
			wantErr:   false,
		},
		{
			name:      "deeply nested meta field",
			fieldPath: "meta.document.content.title",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      "Test Document",
			wantErr:   false,
		},
		{
			name:      "action field",
			fieldPath: "action",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      "read",
			wantErr:   false,
		},
		{
			name:      "resource field",
			fieldPath: "resource",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      "document",
			wantErr:   false,
		},
		{
			name:      "direct meta access",
			fieldPath: "owner",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      "user456",
			wantErr:   false,
		},
		{
			name:      "non-existent field",
			fieldPath: "meta.nonexistent",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      nil,
			wantErr:   false,
		},
		{
			name:      "nil meta",
			fieldPath: "meta.owner",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      nil,
			want:      nil,
			wantErr:   false,
		},
		{
			name:      "invalid actor field",
			fieldPath: "actor.unknown",
			actor:     actor,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluator.extractField(tt.fieldPath, tt.actor, tt.action, tt.resource, tt.meta)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractField() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractField() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	evaluator, err := NewConditionEvaluator([]policy.Condition{})
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	tests := []struct {
		name         string
		fieldValue   any
		compareValue any
		operator     string
		want         bool
		wantErr      bool
	}{
		{
			name:         "equals - strings match",
			fieldValue:   "test",
			compareValue: "test",
			operator:     "eq",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "equals - strings don't match",
			fieldValue:   "test",
			compareValue: "other",
			operator:     "eq",
			want:         false,
			wantErr:      false,
		},
		{
			name:         "equals - numbers match",
			fieldValue:   42,
			compareValue: 42,
			operator:     "eq",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "equals - mixed types match",
			fieldValue:   42,
			compareValue: "42",
			operator:     "eq",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "not equals - strings don't match",
			fieldValue:   "test",
			compareValue: "other",
			operator:     "ne",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "not equals - strings match",
			fieldValue:   "test",
			compareValue: "test",
			operator:     "ne",
			want:         false,
			wantErr:      false,
		},
		{
			name:         "less than - true",
			fieldValue:   10,
			compareValue: 20,
			operator:     "lt",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "less than - false",
			fieldValue:   30,
			compareValue: 20,
			operator:     "lt",
			want:         false,
			wantErr:      false,
		},
		{
			name:         "greater than - true",
			fieldValue:   30,
			compareValue: 20,
			operator:     "gt",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "greater than - false",
			fieldValue:   10,
			compareValue: 20,
			operator:     "gt",
			want:         false,
			wantErr:      false,
		},
		{
			name:         "less than or equal - equal",
			fieldValue:   20,
			compareValue: 20,
			operator:     "lte",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "less than or equal - less",
			fieldValue:   10,
			compareValue: 20,
			operator:     "lte",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "greater than or equal - equal",
			fieldValue:   20,
			compareValue: 20,
			operator:     "gte",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "greater than or equal - greater",
			fieldValue:   30,
			compareValue: 20,
			operator:     "gte",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "in - string in slice",
			fieldValue:   "test",
			compareValue: []any{"other", "test", "value"},
			operator:     "in",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "in - string in string slice",
			fieldValue:   "test",
			compareValue: []string{"other", "test", "value"},
			operator:     "in",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "in - number in slice",
			fieldValue:   42,
			compareValue: []any{10, 42, 100},
			operator:     "in",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "in - not in slice",
			fieldValue:   "missing",
			compareValue: []any{"other", "test", "value"},
			operator:     "in",
			want:         false,
			wantErr:      false,
		},
		{
			name:         "contains - substring",
			fieldValue:   "testing",
			compareValue: "test",
			operator:     "contains",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "contains - not substring",
			fieldValue:   "testing",
			compareValue: "missing",
			operator:     "contains",
			want:         false,
			wantErr:      false,
		},
		{
			name:         "contains - item in slice",
			fieldValue:   []string{"one", "two", "three"},
			compareValue: "two",
			operator:     "contains",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "contains - item not in slice",
			fieldValue:   []string{"one", "two", "three"},
			compareValue: "four",
			operator:     "contains",
			want:         false,
			wantErr:      false,
		},
		{
			name:         "exists - field exists",
			fieldValue:   "value",
			compareValue: true,
			operator:     "exists",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "exists - field does not exist",
			fieldValue:   nil,
			compareValue: false,
			operator:     "exists",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "invalid operator",
			fieldValue:   "test",
			compareValue: "test",
			operator:     "invalid",
			want:         false,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluator.compare(tt.fieldValue, tt.compareValue, tt.operator)
			if (err != nil) != tt.wantErr {
				t.Errorf("compare() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("compare() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTypeConversions(t *testing.T) {
	evaluator, err := NewConditionEvaluator([]policy.Condition{})
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	floatTests := []struct {
		name  string
		value any
		want  float64
		ok    bool
	}{
		{
			name:  "int conversion",
			value: 42,
			want:  42.0,
			ok:    true,
		},
		{
			name:  "int64 conversion",
			value: int64(42),
			want:  42.0,
			ok:    true,
		},
		{
			name:  "float32 conversion",
			value: float32(3.14),
			want:  3.140000104904175,
			ok:    true,
		},
		{
			name:  "float64 direct",
			value: 3.14159,
			want:  3.14159,
			ok:    true,
		},
		{
			name:  "string conversion",
			value: "42.5",
			want:  42.5,
			ok:    true,
		},
		{
			name:  "invalid string",
			value: "not-a-number",
			want:  0,
			ok:    false,
		},
		{
			name:  "bool cannot convert",
			value: true,
			want:  0,
			ok:    false,
		},
		{
			name:  "nil input",
			value: nil,
			want:  0,
			ok:    false,
		},
	}

	for _, tt := range floatTests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := evaluator.toFloat64(tt.value)
			if ok != tt.ok {
				t.Errorf("toFloat64() ok = %v, want %v", ok, tt.ok)
				return
			}
			if ok && got != tt.want {
				t.Errorf("toFloat64() got = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("extractNestedMap nil map", func(t *testing.T) {
		result, err := evaluator.extractNestedMap(nil, []string{"key"})
		if err != nil {
			t.Errorf("extractNestedMap() error = %v, want no error", err)
		}
		if result != nil {
			if m, ok := result.(map[string]any); ok && len(m) > 0 {
				t.Errorf("extractNestedMap() result should be nil or empty map, got %v", result)
			}
		}
	})

	t.Run("extractNestedMap empty parts", func(t *testing.T) {
		m := map[string]any{"key": "value"}
		result, err := evaluator.extractNestedMap(m, []string{})
		if err != nil {
			t.Errorf("extractNestedMap() error = %v, want no error", err)
		}
		if (result == nil) != (m == nil) {
			t.Errorf("extractNestedMap() result = %v, want similar nil status as %v", result, m)
		}
		if result != nil {
			if _, ok := result.(map[string]any)["key"]; !ok {
				t.Errorf("extractNestedMap() result missing expected key")
			}
		}
	})

	t.Run("extractActorField nil actor", func(t *testing.T) {
		_, err := evaluator.extractActorField(security.Actor{}, []string{"id"})
		if err == nil {
			t.Errorf("extractActorField() should return error for nil actor")
		}
	})

	t.Run("extractActorField empty parts", func(t *testing.T) {
		testActor := newMockActor("test-user", registry.Metadata{})
		_, err := evaluator.extractActorField(testActor, []string{})
		if err == nil {
			t.Errorf("extractActorField() should return error for empty parts")
		}
	})

	t.Run("extractMetaField nil meta", func(t *testing.T) {
		result, err := evaluator.extractMetaField(nil, []string{"key"})
		if err != nil {
			t.Errorf("extractMetaField() error = %v, want no error", err)
		}
		if result != nil {
			t.Errorf("extractMetaField() result = %v, want nil", result)
		}
	})

	t.Run("extractMetaField empty parts", func(t *testing.T) {
		testMeta := registry.Metadata{"key": "value"}
		_, err := evaluator.extractMetaField(testMeta, []string{})
		if err == nil {
			t.Errorf("extractMetaField() should return error for empty parts")
		}
	})
}

// TestConditionEvaluator_OperatorBehaviorOnRoles tests the semantic differences between
// contains/ncontains operators (which work on arrays and strings differently) and
// in/nin operators (which perform membership checks on the field value itself).
func TestConditionEvaluator_OperatorBehaviorOnRoles(t *testing.T) {
	t.Parallel()

	evaluator, err := NewConditionEvaluator([]policy.Condition{})
	require.NoError(t, err, "failed to initialize condition evaluator")

	const (
		testAction   = "read"
		testResource = "document"
		testActorID  = "user123"
	)

	t.Run("contains operator on array fields", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			field     string
			value     any
			actorMeta registry.Metadata
			want      bool
		}{
			{
				name:      "exact match found in array",
				field:     "actor.meta.roles",
				value:     "distributor-admin",
				actorMeta: registry.Metadata{"roles": []string{"distributor-admin", "super-admin"}},
				want:      true,
			},
			{
				name:      "exact match not found - substring in array element not matched",
				field:     "actor.meta.roles",
				value:     "admin",
				actorMeta: registry.Metadata{"roles": []string{"distributor-admin", "super-admin"}},
				want:      false,
			},
			{
				name:      "match found in multi-element array",
				field:     "actor.meta.roles",
				value:     "super-admin",
				actorMeta: registry.Metadata{"roles": []string{"distributor-admin", "super-admin", "viewer"}},
				want:      true,
			},
			{
				name:      "no match in single-element array",
				field:     "actor.meta.roles",
				value:     "admin",
				actorMeta: registry.Metadata{"roles": []string{"viewer"}},
				want:      false,
			},
			{
				name:      "empty array returns false",
				field:     "actor.meta.roles",
				value:     "admin",
				actorMeta: registry.Metadata{"roles": []string{}},
				want:      false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				condition := policy.Condition{
					Field:    tt.field,
					Operator: "contains",
					Value:    tt.value,
				}
				actor := newMockActor(testActorID, tt.actorMeta)

				got, err := evaluator.EvaluateCondition(condition, actor, testAction, testResource, nil)

				require.NoError(t, err, "unexpected error during evaluation")
				assert.Equal(t, tt.want, got, "condition evaluation result mismatch")
			})
		}
	})

	t.Run("ncontains operator on array fields", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			field     string
			value     any
			actorMeta registry.Metadata
			want      bool
		}{
			{
				name:      "exact match not in array returns true",
				field:     "actor.meta.roles",
				value:     "admin",
				actorMeta: registry.Metadata{"roles": []string{"distributor-admin", "super-admin"}},
				want:      true,
			},
			{
				name:      "exact match found in array returns false",
				field:     "actor.meta.roles",
				value:     "distributor-admin",
				actorMeta: registry.Metadata{"roles": []string{"distributor-admin", "super-admin"}},
				want:      false,
			},
			{
				name:      "value not in single-element array returns true",
				field:     "actor.meta.roles",
				value:     "admin",
				actorMeta: registry.Metadata{"roles": []string{"viewer"}},
				want:      true,
			},
			{
				name:      "empty array returns true - value not contained",
				field:     "actor.meta.roles",
				value:     "admin",
				actorMeta: registry.Metadata{"roles": []string{}},
				want:      true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				condition := policy.Condition{
					Field:    tt.field,
					Operator: "ncontains",
					Value:    tt.value,
				}
				actor := newMockActor(testActorID, tt.actorMeta)

				got, err := evaluator.EvaluateCondition(condition, actor, testAction, testResource, nil)

				require.NoError(t, err, "unexpected error during evaluation")
				assert.Equal(t, tt.want, got, "condition evaluation result mismatch")
			})
		}
	})

	t.Run("contains operator on string fields", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			field     string
			value     any
			actorMeta registry.Metadata
			want      bool
		}{
			{
				name:      "substring match found",
				field:     "actor.meta.role",
				value:     "admin",
				actorMeta: registry.Metadata{"role": "distributor-admin"},
				want:      true,
			},
			{
				name:      "exact match found",
				field:     "actor.meta.role",
				value:     "admin",
				actorMeta: registry.Metadata{"role": "admin"},
				want:      true,
			},
			{
				name:      "substring not found",
				field:     "actor.meta.role",
				value:     "admin",
				actorMeta: registry.Metadata{"role": "viewer"},
				want:      false,
			},
			{
				name:      "empty string field returns false",
				field:     "actor.meta.role",
				value:     "admin",
				actorMeta: registry.Metadata{"role": ""},
				want:      false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				condition := policy.Condition{
					Field:    tt.field,
					Operator: "contains",
					Value:    tt.value,
				}
				actor := newMockActor(testActorID, tt.actorMeta)

				got, err := evaluator.EvaluateCondition(condition, actor, testAction, testResource, nil)

				require.NoError(t, err, "unexpected error during evaluation")
				assert.Equal(t, tt.want, got, "condition evaluation result mismatch")
			})
		}
	})

	t.Run("ncontains operator on string fields", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			field     string
			value     any
			actorMeta registry.Metadata
			want      bool
		}{
			{
				name:      "substring found returns false",
				field:     "actor.meta.role",
				value:     "admin",
				actorMeta: registry.Metadata{"role": "distributor-admin"},
				want:      false,
			},
			{
				name:      "substring not found returns true",
				field:     "actor.meta.role",
				value:     "admin",
				actorMeta: registry.Metadata{"role": "viewer"},
				want:      true,
			},
			{
				name:      "empty string returns true - value not contained",
				field:     "actor.meta.role",
				value:     "admin",
				actorMeta: registry.Metadata{"role": ""},
				want:      true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				condition := policy.Condition{
					Field:    tt.field,
					Operator: "ncontains",
					Value:    tt.value,
				}
				actor := newMockActor(testActorID, tt.actorMeta)

				got, err := evaluator.EvaluateCondition(condition, actor, testAction, testResource, nil)

				require.NoError(t, err, "unexpected error during evaluation")
				assert.Equal(t, tt.want, got, "condition evaluation result mismatch")
			})
		}
	})

	t.Run("nin operator with scalar field values", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			field     string
			value     any
			actorMeta registry.Metadata
			want      bool
		}{
			{
				name:      "single value not in list",
				field:     "actor.meta.role",
				value:     []string{"admin", "distributor-admin", "super-admin"},
				actorMeta: registry.Metadata{"role": "viewer"},
				want:      true,
			},
			{
				name:      "single value found in list",
				field:     "actor.meta.role",
				value:     []string{"admin", "distributor-admin", "super-admin"},
				actorMeta: registry.Metadata{"role": "distributor-admin"},
				want:      false,
			},
			{
				name:      "empty string not in list",
				field:     "actor.meta.role",
				value:     []string{"admin", "viewer"},
				actorMeta: registry.Metadata{"role": ""},
				want:      true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				condition := policy.Condition{
					Field:    tt.field,
					Operator: "nin",
					Value:    tt.value,
				}
				actor := newMockActor(testActorID, tt.actorMeta)

				got, err := evaluator.EvaluateCondition(condition, actor, testAction, testResource, nil)

				require.NoError(t, err, "unexpected error during evaluation")
				assert.Equal(t, tt.want, got, "condition evaluation result mismatch")
			})
		}
	})

	t.Run("in operator with scalar field values", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			field     string
			value     any
			actorMeta registry.Metadata
			want      bool
		}{
			{
				name:      "single value found in list",
				field:     "actor.meta.role",
				value:     []string{"admin", "distributor-admin", "super-admin"},
				actorMeta: registry.Metadata{"role": "distributor-admin"},
				want:      true,
			},
			{
				name:      "single value not in list",
				field:     "actor.meta.role",
				value:     []string{"admin", "distributor-admin", "super-admin"},
				actorMeta: registry.Metadata{"role": "viewer"},
				want:      false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				condition := policy.Condition{
					Field:    tt.field,
					Operator: "in",
					Value:    tt.value,
				}
				actor := newMockActor(testActorID, tt.actorMeta)

				got, err := evaluator.EvaluateCondition(condition, actor, testAction, testResource, nil)

				require.NoError(t, err, "unexpected error during evaluation")
				assert.Equal(t, tt.want, got, "condition evaluation result mismatch")
			})
		}
	})

	t.Run("operator behavior with array field values", func(t *testing.T) {
		t.Parallel()

		// Note: nin/in operators compare the entire array value against list elements.
		// This is typically not the desired behavior for role checking.
		// Use contains/ncontains for element-wise array matching instead.
		t.Run("nin with array field compares whole array", func(t *testing.T) {
			t.Parallel()

			condition := policy.Condition{
				Field:    "actor.meta.roles",
				Operator: "nin",
				Value:    []string{"admin", "distributor-admin", "super-admin"},
			}
			actor := newMockActor(testActorID, registry.Metadata{
				"roles": []string{"distributor-admin"},
			})

			got, err := evaluator.EvaluateCondition(condition, actor, testAction, testResource, nil)

			require.NoError(t, err, "unexpected error during evaluation")
			// Returns true because the array []string{"distributor-admin"} is not equal
			// to any of the strings in the nin list. This demonstrates that nin is not
			// appropriate for checking array element membership.
			assert.True(t, got, "nin should compare entire array value, not elements")
		})
	})
}

func TestComplexNestedConditions(t *testing.T) {
	conditions := []policy.Condition{
		{
			Field:    "actor.meta.profile.contact.email",
			Operator: "matches",
			Value:    "^[a-z]+@example\\.com$",
		},
	}

	evaluator, err := NewConditionEvaluator(conditions)
	if err != nil {
		t.Fatalf("Failed to create evaluator: %v", err)
	}

	actor := newMockActor("user123", registry.Metadata{
		"role": "admin",
		"permissions": map[string]any{
			"documents": map[string]any{
				"read":  true,
				"write": false,
				"admin": map[string]any{
					"approve": true,
					"delete":  false,
				},
			},
		},
		"profile": map[string]any{
			"name": "John Smith",
			"contact": map[string]any{
				"email": "john@example.com",
				"phone": "123-456-7890",
			},
		},
	})

	meta := registry.Metadata{
		"document": map[string]any{
			"id":      "doc123",
			"version": 2,
			"security": map[string]any{
				"classification": "confidential",
				"access_control": map[string]any{
					"groups": []string{"admin", "managers"},
					"users":  []string{"user123", "user456"},
					"rules": map[string]any{
						"require_mfa": true,
						"ip_ranges":   []string{"192.168.1.0/24", "10.0.0.0/8"},
					},
				},
			},
		},
	}

	tests := []struct {
		name      string
		condition policy.Condition
		want      bool
		wantErr   bool
	}{
		{
			name: "deeply nested actor permission check",
			condition: policy.Condition{
				Field:    "actor.meta.permissions.documents.admin.approve",
				Operator: "eq",
				Value:    true,
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "actor profile contact email",
			condition: policy.Condition{
				Field:    "actor.meta.profile.contact.email",
				Operator: "matches",
				Value:    "^[a-z]+@example\\.com$",
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "check document security classification",
			condition: policy.Condition{
				Field:    "meta.document.security.classification",
				Operator: "eq",
				Value:    "confidential",
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "check user is in access control list",
			condition: policy.Condition{
				Field:     "actor.id",
				Operator:  "in",
				ValueFrom: "meta.document.security.access_control.users",
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "check MFA requirement",
			condition: policy.Condition{
				Field:    "meta.document.security.access_control.rules.require_mfa",
				Operator: "eq",
				Value:    true,
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "check non-existent deep path",
			condition: policy.Condition{
				Field:    "meta.document.security.access_control.nonexistent.field",
				Operator: "exists",
				Value:    false,
			},
			want:    true,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluator.EvaluateCondition(tt.condition, actor, "read", "document", meta)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateCondition() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateCondition() got = %v, want %v", got, tt.want)
			}
		})
	}
}
