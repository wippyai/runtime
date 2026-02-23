// SPDX-License-Identifier: MPL-2.0

package error

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
)

// BuildChain serializes error details (which may contain sensitive data
// like tokens, passwords, connection strings) without any filtering.

func TestBuildChain_RichErrorSensitiveDetailsExposed(t *testing.T) {
	sensitiveDetails := map[string]any{
		"api_key":       "sk-live-abc123def456",
		"password":      "hunter2",
		"internal_path": "/var/lib/app/secrets/token.key",
		"db_connection": "postgresql://admin:secretpass@db.internal:5432",
	}

	err := NewRich(Internal, "database connection failed").
		WithDetails(sensitiveDetails)

	chain := BuildChain(err)
	require.NotNil(t, chain)
	require.Len(t, chain.Errors, 1)

	assert.Equal(t, "sk-live-abc123def456", chain.Errors[0].Details["api_key"],
		"API key exposed in serialized error chain")
	assert.Equal(t, "hunter2", chain.Errors[0].Details["password"],
		"password exposed in serialized error chain")
	assert.Equal(t, "postgresql://admin:secretpass@db.internal:5432", chain.Errors[0].Details["db_connection"],
		"connection string exposed in serialized error chain")
}

func TestBuildChain_StackTracesLeakInternalPaths(t *testing.T) {
	err := NewRich(Internal, "auth failed").
		WithStack([]string{
			"/app/src/internal/auth/handler.go:45 in validateToken",
			"/app/src/internal/db/queries.go:112 in getUserByToken",
			"/app/src/internal/secrets/vault.go:23 in decrypt",
		})

	chain := BuildChain(err)
	require.NotNil(t, chain)
	require.Len(t, chain.Errors, 1)
	require.Len(t, chain.Errors[0].Stack, 3)

	assert.Contains(t, chain.Errors[0].Stack[0], "/app/src/internal/auth/",
		"internal directory structure exposed")
	assert.Contains(t, chain.Errors[0].Stack[2], "/app/src/internal/secrets/",
		"secrets directory path exposed")
}

// FromChain reconstructs all sensitive data without sanitization.
// An attacker intercepting serialized error chains gets full access
// to all details.

func TestFromChain_ReconstructsSensitiveData(t *testing.T) {
	chain := &Chain{
		Errors: []ChainedError{
			{
				Message: "auth failed",
				Kind:    "Internal",
				Details: map[string]any{
					"session_token": "eyJhbGciOiJIUzI1NiJ9.secret",
					"user_email":    "admin@company.com",
					"internal_id":   "usr_12345_secret",
				},
				Stack: []string{"/app/internal/auth.go:10 in verify"},
			},
		},
	}

	rich := FromChain(chain)
	require.NotNil(t, rich)

	assert.Equal(t, "eyJhbGciOiJIUzI1NiJ9.secret", rich.Details()["session_token"],
		"session token reconstructed from serialized chain")
	assert.Equal(t, "admin@company.com", rich.Details()["user_email"],
		"user email reconstructed from serialized chain")
	assert.Contains(t, rich.StackFrames()[0], "/app/internal/auth.go",
		"internal path reconstructed from serialized chain")
}

// Basic Error interface leaks details via attrs.Bag.

func TestBuildChain_BasicErrorAttrsBagExposed(t *testing.T) {
	err := New(PermissionDenied, "access denied").
		WithDetails(attrs.Bag{
			"attempted_resource": "/admin/users",
			"client_ip":          "192.168.1.100",
			"auth_header":        "Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig",
		})

	chain := BuildChain(err)
	require.NotNil(t, chain)
	require.Len(t, chain.Errors, 1)

	assert.Equal(t, "Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig",
		chain.Errors[0].Details["auth_header"],
		"auth header from attrs.Bag exposed in chain")
	assert.Equal(t, "192.168.1.100",
		chain.Errors[0].Details["client_ip"],
		"client IP from attrs.Bag exposed in chain")
}

// Chained errors cascade sensitive details from all layers.

func TestBuildChain_NestedErrorsLeakMultipleLayers(t *testing.T) {
	inner := NewRich(Internal, "token decrypt failed").
		WithDetails(map[string]any{
			"encryption_key_id": "key-prod-2024",
			"vault_path":        "/secrets/tokens/master",
		})

	outer := NewRich(PermissionDenied, "authentication failed").
		WithDetails(map[string]any{
			"user_token": "tok_live_abc123",
		}).
		WithCause(inner)

	chain := BuildChain(outer)
	require.NotNil(t, chain)
	require.Len(t, chain.Errors, 2)

	// Outer error leaks token
	assert.Equal(t, "tok_live_abc123", chain.Errors[0].Details["user_token"])

	// Inner error leaks encryption key info
	assert.Equal(t, "key-prod-2024", chain.Errors[1].Details["encryption_key_id"])
	assert.Equal(t, "/secrets/tokens/master", chain.Errors[1].Details["vault_path"])
}
