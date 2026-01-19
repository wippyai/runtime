// Package auth provides authentication types and context integration.
package auth

import (
	"context"
	"time"
)

// Scope represents the permission level of a token.
type Scope string

const (
	ScopeRead    Scope = "read"
	ScopePublish Scope = "publish"
)

// CanPublish returns true if the scope allows publishing.
func (s Scope) CanPublish() bool {
	return s == ScopePublish
}

// Credential represents authentication credentials.
type Credential struct {
	Token     string
	Registry  string
	UserID    string
	Username  string
	Scope     Scope
	Orgs      []string
	ExpiresAt time.Time
}

// IsExpired returns true if the credential has expired.
func (c *Credential) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(c.ExpiresAt)
}

// CanAccessOrg returns true if the credential can access the given organization.
func (c *Credential) CanAccessOrg(org string) bool {
	if len(c.Orgs) == 0 {
		return true
	}
	for _, o := range c.Orgs {
		if o == org {
			return true
		}
	}
	return false
}

type contextKey int

const credentialKey contextKey = iota

// WithCredential returns a context with the credential attached.
func WithCredential(ctx context.Context, cred *Credential) context.Context {
	return context.WithValue(ctx, credentialKey, cred)
}

// FromContext extracts the credential from the context.
func FromContext(ctx context.Context) *Credential {
	if cred, ok := ctx.Value(credentialKey).(*Credential); ok {
		return cred
	}
	return nil
}

// IsAuthenticated returns true if the context contains valid credentials.
func IsAuthenticated(ctx context.Context) bool {
	cred := FromContext(ctx)
	return cred != nil && cred.Token != "" && !cred.IsExpired()
}

// RequireScope checks if credentials have the required scope.
func RequireScope(ctx context.Context, required Scope) error {
	cred := FromContext(ctx)
	if cred == nil {
		return ErrNotAuthenticated
	}
	if cred.IsExpired() {
		return ErrTokenExpired
	}
	if required == ScopePublish && !cred.Scope.CanPublish() {
		return ErrInsufficientScope
	}
	return nil
}

// RequireOrgAccess checks if credentials can access the organization.
func RequireOrgAccess(ctx context.Context, org string) error {
	cred := FromContext(ctx)
	if cred == nil {
		return ErrNotAuthenticated
	}
	if cred.IsExpired() {
		return ErrTokenExpired
	}
	if !cred.CanAccessOrg(org) {
		return ErrOrgAccessDenied
	}
	return nil
}
