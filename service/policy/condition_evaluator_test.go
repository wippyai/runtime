package policy

import (
	"testing"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/api/service/policy"
)

// mockActor implements the security.Actor interface for testing
type mockActor struct {
	id   string
	meta registry.Metadata
}

func (m *mockActor) ID() string {
	return m.id
}

func (m *mockActor) Meta() registry.Metadata {
	return m.meta
}

// newMockActor creates a new mock actor for testing
func newMockActor(id string, meta registry.Metadata) security.Actor {
	return &mockActor{id: id, meta: meta}
}

// Test cases for EvaluateCondition
func TestEvaluateCondition(t *testing.T) {
	evaluator := NewConditionEvaluator()

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
			name: "matches operator",
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
			name: "matches operator with complex regex",
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
			wantErr:  false, // The implementation doesn't return an error for empty field path
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

// Tests for extractField
func TestExtractField(t *testing.T) {
	evaluator := NewConditionEvaluator()
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
			wantErr:   false, // The implementation doesn't return an error for empty field path
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
			name:      "nil actor",
			fieldPath: "actor.id",
			actor:     nil,
			action:    "read",
			resource:  "document",
			meta:      meta,
			want:      nil,
			wantErr:   true,
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

// Tests for compare method
func TestCompare(t *testing.T) {
	evaluator := NewConditionEvaluator()

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
			name:         "matches - regex match",
			fieldValue:   "testing123",
			compareValue: "^test.*\\d+$",
			operator:     "matches",
			want:         true,
			wantErr:      false,
		},
		{
			name:         "matches - regex no match",
			fieldValue:   "test",
			compareValue: "^\\d+$",
			operator:     "matches",
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

// Tests for type conversion helpers
func TestTypeConversions(t *testing.T) {
	evaluator := NewConditionEvaluator()

	// Test toFloat64
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
			want:  3.140000104904175, // Account for float32 to float64 precision issues
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

	// Test edge cases for extractNestedMap
	t.Run("extractNestedMap nil map", func(t *testing.T) {
		result, err := evaluator.extractNestedMap(nil, []string{"key"})
		if err != nil {
			t.Errorf("extractNestedMap() error = %v, want no error", err)
		}
		// The function may return nil or an empty map for nil input, either is acceptable
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
		// Check if both are nil or non-nil
		if (result == nil) != (m == nil) {
			t.Errorf("extractNestedMap() result = %v, want similar nil status as %v", result, m)
		}
		// If non-nil, check if the key exists in the result
		if result != nil {
			if _, ok := result.(map[string]any)["key"]; !ok {
				t.Errorf("extractNestedMap() result missing expected key")
			}
		}
	})

	// Test edge cases for extractActorField
	t.Run("extractActorField nil actor", func(t *testing.T) {
		_, err := evaluator.extractActorField(nil, []string{"id"})
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

	// Test edge cases for extractMetaField
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

func TestComplexNestedConditions(t *testing.T) {
	evaluator := NewConditionEvaluator()

	// Create a complex nested actor
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

	// Create complex nested metadata
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
