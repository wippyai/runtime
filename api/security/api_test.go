// Package security provides security and authentication abstractions.
package security

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

func TestEventConstants(t *testing.T) {
	tests := []struct {
		name     string
		system   event.System
		kind     event.Kind
		expected string
	}{
		{"system", System, "", "security"},
		{"policy register", "", PolicyRegister, "security.policy.register"},
		{"policy update", "", PolicyUpdate, "security.policy.update"},
		{"policy delete", "", PolicyDelete, "security.policy.delete"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.system != "" {
				assert.Equal(t, tt.expected, string(tt.system))
			}
			if tt.kind != "" {
				assert.Equal(t, tt.expected, string(tt.kind))
			}
		})
	}
}

func TestResultConstants(t *testing.T) {
	t.Run("undefined is first", func(t *testing.T) {
		assert.Equal(t, Result(4), Undefined)
	})

	t.Run("allow is second", func(t *testing.T) {
		assert.Equal(t, Result(5), Allow)
	})

	t.Run("deny is third", func(t *testing.T) {
		assert.Equal(t, Result(6), Deny)
	})

	t.Run("results are distinct", func(t *testing.T) {
		assert.NotEqual(t, Undefined, Allow)
		assert.NotEqual(t, Allow, Deny)
		assert.NotEqual(t, Undefined, Deny)
	})
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"policy not found", ErrPolicyNotFound, "policy not found"},
		{"group not found", ErrGroupNotFound, "policy group not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.True(t, errors.Is(tt.err, tt.err))
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
				Actor: Actor{
					ID:   "service-123",
					Meta: registry.Metadata{"type": "api"},
				},
				PolicyGroups: []registry.ID{
					{NS: "policies", Name: "admin"},
					{NS: "policies", Name: "readonly"},
				},
				Policies: []registry.ID{
					{NS: "policies", Name: "custom"},
				},
			},
			wantErr: false,
		},
		{
			name: "minimal config",
			config: Config{
				Actor: Actor{ID: "user-456"},
			},
			wantErr: false,
		},
		{
			name: "with groups only",
			config: Config{
				Actor: Actor{ID: "service-789"},
				PolicyGroups: []registry.ID{
					{NS: "groups", Name: "viewers"},
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
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestActor_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		actor   Actor
		wantErr bool
	}{
		{
			name: "complete actor",
			actor: Actor{
				ID: "user-123",
				Meta: registry.Metadata{
					"role":  "admin",
					"email": "admin@example.com",
				},
			},
			wantErr: false,
		},
		{
			name: "minimal actor",
			actor: Actor{
				ID: "service-456",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.actor)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded Actor
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.actor, decoded)
		})
	}
}

func TestPolicyEntry_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		entry   PolicyEntry
		wantErr bool
	}{
		{
			name: "with groups",
			entry: PolicyEntry{
				Policy: nil,
				Groups: []registry.ID{
					{NS: "groups", Name: "admin"},
				},
			},
			wantErr: false,
		},
		{
			name: "without groups",
			entry: PolicyEntry{
				Policy: nil,
				Groups: []registry.ID{},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.entry)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded PolicyEntry
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.entry.Groups, decoded.Groups)
		})
	}
}
