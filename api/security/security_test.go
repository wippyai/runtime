// SPDX-License-Identifier: MPL-2.0

// Package security provides security and authentication abstractions.
package security

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
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
		{"policy register", "", PolicyRegister, "policy.register"},
		{"policy update", "", PolicyUpdate, "policy.update"},
		{"policy delete", "", PolicyDelete, "policy.delete"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.system != "" {
				assert.Equal(t, tt.expected, tt.system)
			}
			if tt.kind != "" {
				assert.Equal(t, tt.expected, tt.kind)
			}
		})
	}
}

func TestResultConstants(t *testing.T) {
	t.Run("undefined is first", func(t *testing.T) {
		assert.Equal(t, Result(0), Undefined)
	})

	t.Run("allow is second", func(t *testing.T) {
		assert.Equal(t, Result(1), Allow)
	})

	t.Run("deny is third", func(t *testing.T) {
		assert.Equal(t, Result(2), Deny)
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
					Meta: attrs.Bag{"type": "api"},
				},
				PolicyGroups: []registry.ID{
					registry.NewID("policies", "admin"),
					registry.NewID("policies", "readonly"),
				},
				Policies: []registry.ID{
					registry.NewID("policies", "custom"),
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
					registry.NewID("groups", "viewers"),
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
		actor   Actor
		name    string
		wantErr bool
	}{
		{
			name: "complete actor",
			actor: Actor{
				ID: "user-123",
				Meta: attrs.Bag{
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
					registry.NewID("groups", "admin"),
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

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      apierror.Error
		expected string
		kind     apierror.Kind
	}{
		{"ErrNoFrameContext", ErrNoFrameContext, "no frame context available", Invalid},
		{"ErrScopeNotFound", ErrScopeNotFound, "security scope not found in context", NotFound},
		{"ErrRegistryNotFound", ErrRegistryNotFound, "security registry not found in context", NotFound},
		{"ErrPolicyNotFound", ErrPolicyNotFound, "policy not found", NotFound},
		{"ErrGroupNotFound", ErrGroupNotFound, "policy group not found", NotFound},
		{"ErrTokenInvalid", ErrTokenInvalid, "invalid token format", Invalid},
		{"ErrTokenExpired", ErrTokenExpired, "token expired", Expired},
		{"ErrTokenRevoked", ErrTokenRevoked, "token revoked", Revoked},
		{"ErrTokenNotFound", ErrTokenNotFound, "token not found", NotFound},
		{"ErrUnsupportedTokenType", ErrUnsupportedTokenType, "unsupported token type", Invalid},
		{"ErrPermissionDenied", ErrPermissionDenied, "permission denied", Denied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.Equal(t, tt.kind, tt.err.Kind())
			assert.Nil(t, tt.err.Details())
			assert.Nil(t, errors.Unwrap(tt.err))
		})
	}
}

func TestErrorMethods(t *testing.T) {
	t.Run("WithCause", func(t *testing.T) {
		cause := errors.New("underlying cause")
		newErr := ErrPolicyNotFound.WithCause(cause)
		assert.Equal(t, cause, errors.Unwrap(newErr))
		assert.Equal(t, "policy not found: underlying cause", newErr.Error())
	})

	t.Run("WithDetails", func(t *testing.T) {
		details := attrs.NewBagFrom(map[string]any{"key": "value"})
		newErr := ErrPolicyNotFound.WithDetails(details)
		assert.NotNil(t, newErr.Details())
		val, _ := newErr.Details().Get("key")
		assert.Equal(t, "value", val)
	})
}

func TestKindConstants(t *testing.T) {
	assert.Equal(t, apierror.Kind("NotFound"), NotFound)
	assert.Equal(t, apierror.Kind("Invalid"), Invalid)
	assert.Equal(t, apierror.Kind("Expired"), Expired)
	assert.Equal(t, apierror.Kind("Revoked"), Revoked)
	assert.Equal(t, apierror.Kind("PermissionDenied"), Denied)
}

func TestCommandPools(t *testing.T) {
	t.Run("ValidateTokenCmd", func(t *testing.T) {
		cmd := AcquireValidateTokenCmd()
		assert.NotNil(t, cmd)
		cmd.Token = "test-token"
		assert.Equal(t, ValidateToken, cmd.CmdID())
		cmd.Release()
		assert.Empty(t, cmd.Token)
	})

	t.Run("CreateTokenCmd", func(t *testing.T) {
		cmd := AcquireCreateTokenCmd()
		assert.NotNil(t, cmd)
		cmd.Actor = Actor{ID: "test"}
		assert.Equal(t, CreateToken, cmd.CmdID())
		cmd.Release()
		assert.Empty(t, cmd.Actor.ID)
	})

	t.Run("RevokeTokenCmd", func(t *testing.T) {
		cmd := AcquireRevokeTokenCmd()
		assert.NotNil(t, cmd)
		cmd.Token = "test-token"
		assert.Equal(t, RevokeToken, cmd.CmdID())
		cmd.Release()
		assert.Empty(t, cmd.Token)
	})
}

func TestCommandIDs(t *testing.T) {
	assert.Equal(t, 130, int(ValidateToken))
	assert.Equal(t, 131, int(CreateToken))
	assert.Equal(t, 132, int(RevokeToken))
}

func TestResponseTypes(t *testing.T) {
	t.Run("ValidateTokenResponse", func(t *testing.T) {
		resp := ValidateTokenResponse{
			Actor: Actor{ID: "user1"},
			Error: nil,
		}
		assert.Equal(t, "user1", resp.Actor.ID)
		assert.Nil(t, resp.Error)
	})

	t.Run("CreateTokenResponse", func(t *testing.T) {
		resp := CreateTokenResponse{
			Token: "new-token",
			Error: nil,
		}
		assert.Equal(t, Token("new-token"), resp.Token)
	})

	t.Run("RevokeTokenResponse", func(t *testing.T) {
		resp := RevokeTokenResponse{Error: nil}
		assert.Nil(t, resp.Error)
	})
}

func TestNewTokenDetails(t *testing.T) {
	meta := attrs.NewBagFrom(map[string]any{"scope": "read"})
	details := NewTokenDetails(3600, meta)
	assert.Equal(t, 3600, int(details.Expiration))
	assert.Equal(t, meta, details.Meta)
}
