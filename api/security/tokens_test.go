// Package security provides security and authentication abstractions.
package security

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

func TestTokenErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"token invalid", ErrTokenInvalid, "invalid token format"},
		{"token expired", ErrTokenExpired, "token expired"},
		{"token revoked", ErrTokenRevoked, "token revoked"},
		{"token not found", ErrTokenNotFound, "token not found"},
		{"unsupported token type", ErrUnsupportedTokenType, "unsupported token type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
			assert.True(t, errors.Is(tt.err, tt.err))
		})
	}
}

func TestTokenType(t *testing.T) {
	t.Run("type alias", func(t *testing.T) {
		var tt TokenType = "jwt"
		assert.Equal(t, "jwt", string(tt))
		assert.IsType(t, TokenType(""), tt)
	})
}

func TestToken(t *testing.T) {
	t.Run("type alias", func(t *testing.T) {
		//nolint:gosec // test data
		var token Token = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"
		assert.Equal(t, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9", string(token))
		assert.IsType(t, Token(""), token)
	})
}

func TestTokenDetails_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		details TokenDetails
		wantErr bool
	}{
		{
			name: "complete details",
			details: TokenDetails{
				Expiration: 1 * time.Hour,
				Meta: registry.Metadata{
					"issuer": "auth-service",
					"scope":  "read:write",
				},
			},
			wantErr: false,
		},
		{
			name: "with expiration only",
			details: TokenDetails{
				Expiration: 30 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "with meta only",
			details: TokenDetails{
				Meta: registry.Metadata{
					"custom": "value",
				},
			},
			wantErr: false,
		},
		{
			name:    "empty details",
			details: TokenDetails{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(&tt.details)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var decoded TokenDetails
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.details.Expiration, decoded.Expiration)
			assert.Equal(t, tt.details.Meta, decoded.Meta)
		})
	}
}
