package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Scope ---

func TestScope_CanPublish(t *testing.T) {
	assert.True(t, ScopePublish.CanPublish())
	assert.False(t, ScopeRead.CanPublish())
	assert.False(t, Scope("").CanPublish())
}

// --- Credential ---

func TestCredential_IsExpired_Zero(t *testing.T) {
	c := &Credential{}
	assert.False(t, c.IsExpired())
}

func TestCredential_IsExpired_Future(t *testing.T) {
	c := &Credential{ExpiresAt: time.Now().Add(time.Hour)}
	assert.False(t, c.IsExpired())
}

func TestCredential_IsExpired_Past(t *testing.T) {
	c := &Credential{ExpiresAt: time.Now().Add(-time.Hour)}
	assert.True(t, c.IsExpired())
}

func TestCredential_CanAccessOrg_NoOrgs(t *testing.T) {
	c := &Credential{}
	assert.True(t, c.CanAccessOrg("any-org"))
}

func TestCredential_CanAccessOrg_Match(t *testing.T) {
	c := &Credential{Orgs: []string{"org-1", "org-2"}}
	assert.True(t, c.CanAccessOrg("org-2"))
}

func TestCredential_CanAccessOrg_NoMatch(t *testing.T) {
	c := &Credential{Orgs: []string{"org-1", "org-2"}}
	assert.False(t, c.CanAccessOrg("org-3"))
}

// --- Context ---

func TestWithCredential_FromContext(t *testing.T) {
	cred := &Credential{Token: "tok", Registry: "https://hub.example.com"}
	ctx := WithCredential(context.Background(), cred)

	got := FromContext(ctx)
	require.NotNil(t, got)
	assert.Equal(t, "tok", got.Token)
}

func TestFromContext_Empty(t *testing.T) {
	assert.Nil(t, FromContext(context.Background()))
}

func TestIsAuthenticated_Valid(t *testing.T) {
	cred := &Credential{
		Token:     "valid-token",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	ctx := WithCredential(context.Background(), cred)
	assert.True(t, IsAuthenticated(ctx))
}

func TestIsAuthenticated_NoCred(t *testing.T) {
	assert.False(t, IsAuthenticated(context.Background()))
}

func TestIsAuthenticated_EmptyToken(t *testing.T) {
	ctx := WithCredential(context.Background(), &Credential{})
	assert.False(t, IsAuthenticated(ctx))
}

func TestIsAuthenticated_Expired(t *testing.T) {
	cred := &Credential{
		Token:     "valid-token",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	ctx := WithCredential(context.Background(), cred)
	assert.False(t, IsAuthenticated(ctx))
}

// --- RequireScope ---

func TestRequireScope_NoCred(t *testing.T) {
	err := RequireScope(context.Background(), ScopeRead)
	assert.Equal(t, ErrNotAuthenticated, err)
}

func TestRequireScope_Expired(t *testing.T) {
	cred := &Credential{
		Token:     "tok",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	ctx := WithCredential(context.Background(), cred)
	assert.Equal(t, ErrTokenExpired, RequireScope(ctx, ScopeRead))
}

func TestRequireScope_ReadAllowed(t *testing.T) {
	cred := &Credential{
		Token:     "tok",
		Scope:     ScopeRead,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	ctx := WithCredential(context.Background(), cred)
	assert.NoError(t, RequireScope(ctx, ScopeRead))
}

func TestRequireScope_PublishDenied(t *testing.T) {
	cred := &Credential{
		Token:     "tok",
		Scope:     ScopeRead,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	ctx := WithCredential(context.Background(), cred)
	assert.Equal(t, ErrInsufficientScope, RequireScope(ctx, ScopePublish))
}

func TestRequireScope_PublishAllowed(t *testing.T) {
	cred := &Credential{
		Token:     "tok",
		Scope:     ScopePublish,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	ctx := WithCredential(context.Background(), cred)
	assert.NoError(t, RequireScope(ctx, ScopePublish))
}

// --- RequireOrgAccess ---

func TestRequireOrgAccess_NoCred(t *testing.T) {
	assert.Equal(t, ErrNotAuthenticated, RequireOrgAccess(context.Background(), "org"))
}

func TestRequireOrgAccess_Expired(t *testing.T) {
	cred := &Credential{
		Token:     "tok",
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	ctx := WithCredential(context.Background(), cred)
	assert.Equal(t, ErrTokenExpired, RequireOrgAccess(ctx, "org"))
}

func TestRequireOrgAccess_Allowed(t *testing.T) {
	cred := &Credential{
		Token:     "tok",
		Orgs:      []string{"org-1"},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	ctx := WithCredential(context.Background(), cred)
	assert.NoError(t, RequireOrgAccess(ctx, "org-1"))
}

func TestRequireOrgAccess_Denied(t *testing.T) {
	cred := &Credential{
		Token:     "tok",
		Orgs:      []string{"org-1"},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	ctx := WithCredential(context.Background(), cred)
	assert.Equal(t, ErrOrgAccessDenied, RequireOrgAccess(ctx, "org-2"))
}
