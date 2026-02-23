// SPDX-License-Identifier: MPL-2.0

// Package policy provides policy service configuration.
package policy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

func TestKindConstant(t *testing.T) {
	assert.Equal(t, "security.policy", Policy)
}

func TestEffectConstants(t *testing.T) {
	tests := []struct {
		name     string
		effect   Effect
		expected string
	}{
		{"allow", Allow, "allow"},
		{"deny", Deny, "deny"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.effect))
		})
	}
}

func TestCondition_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name      string
		condition Condition
		wantErr   bool
	}{
		{
			name: "complete condition with value",
			condition: Condition{
				Field:    "actor.meta.role",
				Operator: "eq",
				Value:    "admin",
			},
			wantErr: false,
		},
		{
			name: "condition with value_from",
			condition: Condition{
				Field:     "meta.owner",
				Operator:  "eq",
				ValueFrom: "actor.id",
			},
			wantErr: false,
		},
		{
			name: "exists operator",
			condition: Condition{
				Field:    "meta.tag",
				Operator: "exists",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.condition)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded Condition
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.condition.Field, decoded.Field)
			assert.Equal(t, tt.condition.Operator, decoded.Operator)
		})
	}
}

func TestDefinition_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		def     Definition
		wantErr bool
	}{
		{
			name: "complete definition",
			def: Definition{
				Actions:   "*",
				Resources: "*",
				Effect:    Allow,
				Conditions: []Condition{
					{Field: "actor.role", Operator: "eq", Value: "admin"},
				},
			},
			wantErr: false,
		},
		{
			name: "list actions and resources",
			def: Definition{
				Actions:   []any{"read", "write"},
				Resources: []any{"documents/*", "images/*"},
				Effect:    Deny,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.def)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded Definition
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.def.Effect, decoded.Effect)
		})
	}
}

func TestConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "complete config",
			config: Config{
				Policy: Definition{
					Actions:   "*",
					Resources: "*",
					Effect:    Allow,
				},
				Groups: []string{"admin", "users"},
			},
			wantErr: false,
		},
		{
			name: "minimal config",
			config: Config{
				Policy: Definition{
					Actions:   "read",
					Resources: "documents",
					Effect:    Allow,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded Config
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config.Policy.Effect, decoded.Policy.Effect)
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		errMsg  string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Policy: Definition{
					Actions:   "*",
					Resources: "*",
					Effect:    Allow,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid effect",
			config: Config{
				Policy: Definition{
					Actions:   "*",
					Resources: "*",
					Effect:    "invalid",
				},
			},
			wantErr: true,
			errMsg:  "invalid policy effect",
		},
		{
			name: "empty actions string",
			config: Config{
				Policy: Definition{
					Actions:   "",
					Resources: "*",
					Effect:    Allow,
				},
			},
			wantErr: true,
			errMsg:  "actions string cannot be empty",
		},
		{
			name: "empty actions list",
			config: Config{
				Policy: Definition{
					Actions:   []any{},
					Resources: "*",
					Effect:    Allow,
				},
			},
			wantErr: true,
			errMsg:  "actions list cannot be empty",
		},
		{
			name: "invalid actions type",
			config: Config{
				Policy: Definition{
					Actions:   123,
					Resources: "*",
					Effect:    Allow,
				},
			},
			wantErr: true,
			errMsg:  "actions must be a string or list",
		},
		{
			name: "empty resources string",
			config: Config{
				Policy: Definition{
					Actions:   "*",
					Resources: "",
					Effect:    Allow,
				},
			},
			wantErr: true,
			errMsg:  "resources string cannot be empty",
		},
		{
			name: "condition without field",
			config: Config{
				Policy: Definition{
					Actions:   "*",
					Resources: "*",
					Effect:    Allow,
					Conditions: []Condition{
						{Operator: "eq", Value: "test"},
					},
				},
			},
			wantErr: true,
			errMsg:  "field is required",
		},
		{
			name: "condition without operator",
			config: Config{
				Policy: Definition{
					Actions:   "*",
					Resources: "*",
					Effect:    Allow,
					Conditions: []Condition{
						{Field: "test", Value: "test"},
					},
				},
			},
			wantErr: true,
			errMsg:  "operator is required",
		},
		{
			name: "condition without value or value_from",
			config: Config{
				Policy: Definition{
					Actions:   "*",
					Resources: "*",
					Effect:    Allow,
					Conditions: []Condition{
						{Field: "test", Operator: "eq"},
					},
				},
			},
			wantErr: true,
			errMsg:  "value or value_from is required",
		},
		{
			name: "invalid operator",
			config: Config{
				Policy: Definition{
					Actions:   "*",
					Resources: "*",
					Effect:    Allow,
					Conditions: []Condition{
						{Field: "test", Operator: "invalid", Value: "test"},
					},
				},
			},
			wantErr: true,
			errMsg:  "invalid operator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfig_GetActions(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected []string
	}{
		{
			name: "wildcard",
			config: Config{
				Policy: Definition{Actions: "*"},
			},
			expected: []string{"*"},
		},
		{
			name: "single string",
			config: Config{
				Policy: Definition{Actions: "read"},
			},
			expected: []string{"read"},
		},
		{
			name: "list",
			config: Config{
				Policy: Definition{Actions: []any{"read", "write", "delete"}},
			},
			expected: []string{"read", "write", "delete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetActions()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_GetResources(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected []string
	}{
		{
			name: "wildcard",
			config: Config{
				Policy: Definition{Resources: "*"},
			},
			expected: []string{"*"},
		},
		{
			name: "single string",
			config: Config{
				Policy: Definition{Resources: "documents"},
			},
			expected: []string{"documents"},
		},
		{
			name: "list",
			config: Config{
				Policy: Definition{Resources: []any{"docs/*", "images/*"}},
			},
			expected: []string{"docs/*", "images/*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetResources()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfig_GetGroupIDs(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		defaultNS registry.Namespace
		expected  []registry.ID
	}{
		{
			name: "with namespace",
			config: Config{
				Groups: []string{"ns:group1", "ns:group2"},
			},
			defaultNS: "default",
			expected: []registry.ID{
				registry.NewID("ns", "group1"),
				registry.NewID("ns", "group2"),
			},
		},
		{
			name: "without namespace uses default",
			config: Config{
				Groups: []string{"group1", "group2"},
			},
			defaultNS: "policies",
			expected: []registry.ID{
				registry.NewID("policies", "group1"),
				registry.NewID("policies", "group2"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetGroupIDs(tt.defaultNS)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExprKind(t *testing.T) {
	assert.Equal(t, "security.policy.expr", ExprKind)
}

func TestExprConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		errMsg  string
		config  ExprConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: ExprConfig{
				Policy: ExprDefinition{
					Actions:    "*",
					Resources:  "*",
					Expression: "actor.role == \"admin\"",
					Effect:     Allow,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid effect",
			config: ExprConfig{
				Policy: ExprDefinition{
					Actions:    "*",
					Resources:  "*",
					Expression: "true",
					Effect:     "invalid",
				},
			},
			wantErr: true,
			errMsg:  "invalid policy effect",
		},
		{
			name: "empty actions string",
			config: ExprConfig{
				Policy: ExprDefinition{
					Actions:    "",
					Resources:  "*",
					Expression: "true",
					Effect:     Allow,
				},
			},
			wantErr: true,
			errMsg:  "actions string cannot be empty",
		},
		{
			name: "empty actions list",
			config: ExprConfig{
				Policy: ExprDefinition{
					Actions:    []any{},
					Resources:  "*",
					Expression: "true",
					Effect:     Allow,
				},
			},
			wantErr: true,
			errMsg:  "actions list cannot be empty",
		},
		{
			name: "invalid actions type",
			config: ExprConfig{
				Policy: ExprDefinition{
					Actions:    123,
					Resources:  "*",
					Expression: "true",
					Effect:     Allow,
				},
			},
			wantErr: true,
			errMsg:  "actions must be a string or list",
		},
		{
			name: "empty resources string",
			config: ExprConfig{
				Policy: ExprDefinition{
					Actions:    "*",
					Resources:  "",
					Expression: "true",
					Effect:     Allow,
				},
			},
			wantErr: true,
			errMsg:  "resources string cannot be empty",
		},
		{
			name: "empty resources list",
			config: ExprConfig{
				Policy: ExprDefinition{
					Actions:    "*",
					Resources:  []any{},
					Expression: "true",
					Effect:     Allow,
				},
			},
			wantErr: true,
			errMsg:  "resources list cannot be empty",
		},
		{
			name: "invalid resources type",
			config: ExprConfig{
				Policy: ExprDefinition{
					Actions:    "*",
					Resources:  123,
					Expression: "true",
					Effect:     Allow,
				},
			},
			wantErr: true,
			errMsg:  "resources must be a string or list",
		},
		{
			name: "empty expression",
			config: ExprConfig{
				Policy: ExprDefinition{
					Actions:    "*",
					Resources:  "*",
					Expression: "",
					Effect:     Allow,
				},
			},
			wantErr: true,
			errMsg:  "expression cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExprConfig_GetGroupIDs(t *testing.T) {
	config := ExprConfig{
		Groups: []string{"group1", "group2"},
	}
	ids := config.GetGroupIDs("test-ns")
	require.Len(t, ids, 2)
	assert.Equal(t, registry.NewID("test-ns", "group1"), ids[0])
	assert.Equal(t, registry.NewID("test-ns", "group2"), ids[1])
}

func TestErrorFactories(t *testing.T) {
	t.Run("NewInvalidPolicyEffectError", func(t *testing.T) {
		err := NewInvalidPolicyEffectError("unknown")
		assert.Contains(t, err.Error(), "invalid policy effect: unknown")
	})

	t.Run("NewConditionFieldEmptyError", func(t *testing.T) {
		err := NewConditionFieldEmptyError(0)
		assert.Contains(t, err.Error(), "condition 0: field is required")
	})

	t.Run("NewConditionOperatorEmptyError", func(t *testing.T) {
		err := NewConditionOperatorEmptyError(1)
		assert.Contains(t, err.Error(), "condition 1: operator is required")
	})

	t.Run("NewConditionValueRequiredError", func(t *testing.T) {
		err := NewConditionValueRequiredError(2)
		assert.Contains(t, err.Error(), "condition 2: value or value_from is required")
	})

	t.Run("NewConditionInvalidOperatorError", func(t *testing.T) {
		err := NewConditionInvalidOperatorError(3, "invalid_op")
		assert.Contains(t, err.Error(), "condition 3: invalid operator: invalid_op")
	})
}

func TestErrorConstants(t *testing.T) {
	assert.Contains(t, ErrActionsStringEmpty.Error(), "actions string cannot be empty")
	assert.Contains(t, ErrActionsListEmpty.Error(), "actions list cannot be empty")
	assert.Contains(t, ErrActionsInvalidType.Error(), "actions must be a string or list")
	assert.Contains(t, ErrResourcesStringEmpty.Error(), "resources string cannot be empty")
	assert.Contains(t, ErrResourcesListEmpty.Error(), "resources list cannot be empty")
	assert.Contains(t, ErrResourcesInvalidType.Error(), "resources must be a string or list")
	assert.Contains(t, ErrExpressionEmpty.Error(), "expression cannot be empty")
}
