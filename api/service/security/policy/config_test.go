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
	assert.Equal(t, "security.policy", Kind)
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
		config  Config
		wantErr bool
		errMsg  string
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
			errMsg:  "actions must be either a string or a list of strings",
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
			errMsg:  "field cannot be empty",
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
			errMsg:  "operator cannot be empty",
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
			errMsg:  "either value or value_from must be provided",
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
