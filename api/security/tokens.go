package security

import (
	"context"
	"errors"
	"time"

	"github.com/ponyruntime/pony/api/registry"
)

// Common token errors
var (
	// ErrTokenInvalid indicates the token format is invalid
	ErrTokenInvalid = errors.New("invalid token format")

	// ErrTokenExpired indicates the token has expired
	ErrTokenExpired = errors.New("token expired")

	// ErrTokenRevoked indicates the token has been revoked
	ErrTokenRevoked = errors.New("token revoked")

	// ErrTokenNotFound indicates the token doesn't exist
	ErrTokenNotFound = errors.New("token not found")

	// ErrUnsupportedTokenType indicates the token type is not supported by the store
	ErrUnsupportedTokenType = errors.New("unsupported token type")
)

// TokenType represents the type of token (e.g., JWT, opaque)
type (
	TokenType string

	// Token represents a security token used for authentication
	Token string

	// TokenDetails defines options for token creation
	TokenDetails struct {
		// Expiration time for the token
		Expiration time.Duration

		// Additional token meta
		Meta registry.Metadata
	}

	// TokenStore defines the interface for managing authentication tokens
	TokenStore interface {
		// Create generates a new token for the given actor and scope
		Create(ctx context.Context, actor Actor, scope Scope, details TokenDetails) (Token, error)

		// Validate checks if a token is valid and returns the associated actor and scope
		Validate(ctx context.Context, token Token) (Actor, Scope, error)

		// Revoke removes a token from the store
		Revoke(ctx context.Context, token Token) error
	}
)
