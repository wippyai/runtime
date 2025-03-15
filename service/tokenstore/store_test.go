package tokenstore_test

import (
	"context"
	"encoding/json"
	"fmt"
	memstore2 "github.com/ponyruntime/pony/service/memstore"
	tokenstore2 "github.com/ponyruntime/pony/service/tokenstore"
	securitysys "github.com/ponyruntime/pony/system/security"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/api/service/memstore"
	"github.com/ponyruntime/pony/api/service/tokenstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// jsonTranscoder is a simple JSON implementation of payload.Transcoder
type jsonTranscoder struct{}

func (t *jsonTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	if p.Format() == format {
		return p, nil
	}

	data, err := json.Marshal(p.Data())
	if err != nil {
		return nil, err
	}

	return payload.NewPayload(string(data), payload.JSON), nil
}

func (t *jsonTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	switch p.Format() {
	case payload.JSON:
		jsonStr, ok := p.Data().(string)
		if !ok {
			return nil
		}
		return json.Unmarshal([]byte(jsonStr), v)
	case payload.Golang:
		// For Golang format, just set the value directly
		src := p.Data()
		if src == nil {
			return nil
		}

		// Try to use type assertion first
		if v, ok := v.(*interface{}); ok {
			*v = src
			return nil
		}

		// Otherwise use reflection via JSON marshaling/unmarshaling
		data, err := json.Marshal(src)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, v)
	default:
		return nil
	}
}

// testPolicy implements security.Policy interface for testing
type testPolicy struct {
	id       registry.ID
	decision security.Result
}

func (p *testPolicy) ID() registry.ID {
	return p.id
}

func (p *testPolicy) Evaluate(actor security.Actor, action, resource string, meta registry.Metadata) security.Result {
	return p.decision
}

// testSecurityRegistry implements security.Registry interface for testing
type testSecurityRegistry struct {
	policies map[string]security.Policy
	groups   map[string][]security.Policy
}

func newTestSecurityRegistry() *testSecurityRegistry {
	return &testSecurityRegistry{
		policies: make(map[string]security.Policy),
		groups:   make(map[string][]security.Policy),
	}
}

func (r *testSecurityRegistry) GetPolicy(id registry.ID) (security.Policy, error) {
	policy, ok := r.policies[id.String()]
	if !ok {
		return nil, security.ErrPolicyNotFound
	}
	return policy, nil
}

func (r *testSecurityRegistry) GetPolicyGroup(groupID registry.ID) (security.Scope, error) {
	policies, ok := r.groups[groupID.String()]
	if !ok {
		return nil, security.ErrGroupNotFound
	}
	return securitysys.NewScope(policies), nil
}

func (r *testSecurityRegistry) ListGroups() []registry.ID {
	groups := make([]registry.ID, 0, len(r.groups))
	for g := range r.groups {
		groups = append(groups, registry.ParseID(g))
	}
	return groups
}

func (r *testSecurityRegistry) ListPolicies() []registry.ID {
	policies := make([]registry.ID, 0, len(r.policies))
	for p := range r.policies {
		policies = append(policies, registry.ParseID(p))
	}
	return policies
}

func (r *testSecurityRegistry) AddPolicy(id registry.ID, decision security.Result) {
	r.policies[id.String()] = &testPolicy{id: id, decision: decision}
}

func (r *testSecurityRegistry) AddPolicyToGroup(policyID, groupID registry.ID) {
	policy, ok := r.policies[policyID.String()]
	if !ok {
		return
	}

	if r.groups[groupID.String()] == nil {
		r.groups[groupID.String()] = []security.Policy{}
	}

	r.groups[groupID.String()] = append(r.groups[groupID.String()], policy)
}

// testResourceRegistry implements resource.Registry interface for testing
type testResourceRegistry struct {
	resources map[string]resource.Provider
}

func newTestResourceRegistry() *testResourceRegistry {
	return &testResourceRegistry{
		resources: make(map[string]resource.Provider),
	}
}

func (r *testResourceRegistry) Acquire(ctx context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	provider, ok := r.resources[id.String()]
	if !ok {
		return nil, resource.ErrResourceNotFound
	}
	return provider.Acquire(ctx, id, mode)
}

func (r *testResourceRegistry) Register(id registry.ID, provider resource.Provider) {
	r.resources[id.String()] = provider
}

// TestTokenStoreCreateValidateRevoke tests the full lifecycle of a token
func TestTokenStoreCreateValidateRevoke(t *testing.T) {
	// Setup
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	// Create and configure MemoryStore
	storeID := registry.ID{Name: "test-store"}
	memConfig := &memstore.MemoryConfig{
		MaxSize:         1000,
		CleanupInterval: time.Second,
	}
	memStore := memstore2.NewMemoryStore(storeID, memConfig, logger)

	// Start the memory store
	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer memStore.Stop(ctx)

	// Wait for "started" message
	select {
	case <-statusChan:
		// Store started
	case <-time.After(time.Second):
		t.Fatal("store failed to start in time")
	}

	// Create resource registry and register the store
	resources := newTestResourceRegistry()
	resources.Register(storeID, memStore)

	// Create security registry
	secRegistry := newTestSecurityRegistry()

	// Add some test policies
	policyID := registry.ID{Name: "test-policy"}
	secRegistry.AddPolicy(policyID, security.Allow)

	// Configure token store
	tokenConfig := &tokenstore.Config{
		Store:             storeID,
		TokenLength:       32,
		TokenKey:          "test-signing-key",
		DefaultExpiration: time.Hour,
	}

	// Create token store
	ts, err := tokenstore2.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
	require.NoError(t, err)

	// Test token creation
	actor := security.Actor{
		ID:   "test-user",
		Meta: registry.Metadata{"role": "admin"},
	}

	// Create a scope with the test policy
	scope := securitysys.NewScope([]security.Policy{
		&testPolicy{id: policyID, decision: security.Allow},
	})

	// Create token with details
	details := security.TokenDetails{
		Expiration: 30 * time.Minute,
		Meta:       registry.Metadata{"purpose": "testing"},
	}

	token, err := ts.Create(ctx, actor, scope, details)
	require.NoError(t, err)
	require.NotEmpty(t, token, "Token should not be empty")

	// Test token validation
	validatedActor, validatedScope, err := ts.Validate(ctx, token)
	require.NoError(t, err)

	// Verify actor was preserved
	assert.Equal(t, actor.ID, validatedActor.ID)
	assert.Equal(t, actor.Meta["role"], validatedActor.Meta["role"])

	// Verify scope contains our policy
	assert.True(t, validatedScope.Contains(policyID))

	// Test token evaluation
	result := validatedScope.Evaluate(validatedActor, "read", "resource", nil)
	assert.Equal(t, security.Allow, result)

	// Test token revocation
	err = ts.Revoke(ctx, token)
	require.NoError(t, err)

	// Verify token is no longer valid
	_, _, err = ts.Validate(ctx, token)
	assert.Error(t, err)
	assert.Equal(t, security.ErrTokenNotFound, err)
}

// TestTokenExpiration tests that tokens expire correctly
func TestTokenExpiration(t *testing.T) {
	// Setup similar to previous test
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	storeID := registry.ID{Name: "test-store"}
	memConfig := &memstore.MemoryConfig{
		MaxSize:         1000,
		CleanupInterval: 100 * time.Millisecond, // Short cleanup for testing
	}
	memStore := memstore2.NewMemoryStore(storeID, memConfig, logger)

	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer memStore.Stop(ctx)

	select {
	case <-statusChan:
	case <-time.After(time.Second):
		t.Fatal("store failed to start in time")
	}

	resources := newTestResourceRegistry()
	resources.Register(storeID, memStore)
	secRegistry := newTestSecurityRegistry()

	// Configure token store with very short default expiration
	tokenConfig := &tokenstore.Config{
		Store:             storeID,
		TokenLength:       32,
		TokenKey:          "test-signing-key",
		DefaultExpiration: 500 * time.Millisecond, // Short expiration for testing
	}

	ts, err := tokenstore2.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
	require.NoError(t, err)

	// Create a token with default expiration
	actor := security.Actor{ID: "test-user"}
	token, err := ts.Create(ctx, actor, nil, security.TokenDetails{})
	require.NoError(t, err)

	// Token should be valid immediately
	_, _, err = ts.Validate(ctx, token)
	require.NoError(t, err)

	// Wait for token to expire
	time.Sleep(600 * time.Millisecond)

	// Token should now be expired
	_, _, err = ts.Validate(ctx, token)
	assert.Error(t, err)
}

// TestTokenSignature tests that token signatures are properly validated
func TestTokenSignature(t *testing.T) {
	// Setup
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	storeID := registry.ID{Name: "test-store"}
	memStore := memstore2.NewMemoryStore(storeID, nil, logger)

	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer memStore.Stop(ctx)

	select {
	case <-statusChan:
	case <-time.After(time.Second):
		t.Fatal("store failed to start in time")
	}

	resources := newTestResourceRegistry()
	resources.Register(storeID, memStore)
	secRegistry := newTestSecurityRegistry()

	// Configure token store with signing key
	tokenConfig := &tokenstore.Config{
		Store:             storeID,
		TokenLength:       32,
		TokenKey:          "test-signing-key",
		DefaultExpiration: time.Hour,
	}

	ts, err := tokenstore.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
	require.NoError(t, err)

	// Create a token
	actor := security.Actor{ID: "test-user"}
	token, err := ts.Create(ctx, actor, nil, security.TokenDetails{})
	require.NoError(t, err)

	// Verify token has a signature part
	assert.Contains(t, string(token), ".")

	// Tamper with the token signature
	tamperedToken := security.Token(string(token) + "corrupted")

	// Validation should fail
	_, _, err = ts.Validate(ctx, tamperedToken)
	assert.Error(t, err)
	assert.Equal(t, security.ErrTokenInvalid, err)

	// Now tamper with just the token part
	parts := strings.Split(string(token), ".")
	require.Len(t, parts, 2)
	tamperedToken = security.Token(parts[0] + "x." + parts[1])

	// Validation should also fail
	_, _, err = ts.Validate(ctx, tamperedToken)
	assert.Error(t, err)
	assert.Equal(t, security.ErrTokenInvalid, err)
}

// TestEdgeCases tests various edge cases
func TestEdgeCases(t *testing.T) {
	// Setup
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	storeID := registry.ID{Name: "test-store"}
	memStore := memstore.NewMemoryStore(storeID, nil, logger)

	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer memStore.Stop(ctx)

	select {
	case <-statusChan:
	case <-time.After(time.Second):
		t.Fatal("store failed to start in time")
	}

	resources := newTestResourceRegistry()
	resources.Register(storeID, memStore)
	secRegistry := newTestSecurityRegistry()

	tokenConfig := &tokenstore.Config{
		Store:             storeID,
		TokenLength:       32,
		TokenKey:          "test-signing-key",
		DefaultExpiration: time.Hour,
	}

	ts, err := tokenstore.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
	require.NoError(t, err)

	// Test case: Empty token
	_, _, err = ts.Validate(ctx, "")
	assert.Error(t, err)
	assert.Equal(t, security.ErrTokenInvalid, err)

	// Test case: Revoke non-existent token
	err = ts.Revoke(ctx, "non-existent-token")
	assert.Error(t, err)
	assert.Equal(t, security.ErrTokenInvalid, err)

	// Test case: Create token with never expiring TTL
	actor := security.Actor{ID: "test-user"}
	token, err := ts.Create(ctx, actor, nil, security.TokenDetails{
		Expiration: 0, // Should use default from config
	})
	require.NoError(t, err)

	// Validate it works
	_, _, err = ts.Validate(ctx, token)
	require.NoError(t, err)

	// Test case: Create token with explicit non-expiring TTL
	nonExpiringDetails := security.TokenDetails{
		Expiration: -1, // Should be interpreted as non-expiring
	}
	tokenNoExpiry, err := ts.Create(ctx, actor, nil, nonExpiringDetails)
	require.NoError(t, err)

	// Validate it works
	_, _, err = ts.Validate(ctx, tokenNoExpiry)
	require.NoError(t, err)
}

// TestTokenStoreWithoutSigningKey tests token store functionality without a signing key
func TestTokenStoreWithoutSigningKey(t *testing.T) {
	// Setup
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	storeID := registry.ID{Name: "test-store"}
	memStore := memstore.NewMemoryStore(storeID, nil, logger)

	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer memStore.Stop(ctx)

	select {
	case <-statusChan:
	case <-time.After(time.Second):
		t.Fatal("store failed to start in time")
	}

	resources := newTestResourceRegistry()
	resources.Register(storeID, memStore)
	secRegistry := newTestSecurityRegistry()

	// Configure token store WITHOUT signing key
	tokenConfig := &tokenstore.Config{
		Store:             storeID,
		TokenLength:       32,
		TokenKey:          "", // No signing key
		DefaultExpiration: time.Hour,
	}

	ts, err := tokenstore.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
	require.NoError(t, err)

	// Create a token
	actor := security.Actor{ID: "test-user"}
	token, err := ts.Create(ctx, actor, nil, security.TokenDetails{})
	require.NoError(t, err)

	// Verify token has no signature part
	assert.NotContains(t, string(token), ".")

	// Validation should work
	validatedActor, _, err := ts.Validate(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, actor.ID, validatedActor.ID)

	// Revocation should work
	err = ts.Revoke(ctx, token)
	require.NoError(t, err)

	// Token should no longer be valid
	_, _, err = ts.Validate(ctx, token)
	assert.Error(t, err)
}

// TestConcurrentAccess tests the token store under concurrent access
func TestConcurrentAccess(t *testing.T) {
	// Setup
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	storeID := registry.ID{Name: "test-store"}
	memStore := memstore.NewMemoryStore(storeID, nil, logger)

	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer memStore.Stop(ctx)

	select {
	case <-statusChan:
	case <-time.After(time.Second):
		t.Fatal("store failed to start in time")
	}

	resources := newTestResourceRegistry()
	resources.Register(storeID, memStore)
	secRegistry := newTestSecurityRegistry()

	tokenConfig := &tokenstore.Config{
		Store:             storeID,
		TokenLength:       32,
		TokenKey:          "test-signing-key",
		DefaultExpiration: time.Hour,
	}

	ts, err := tokenstore.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
	require.NoError(t, err)

	// Number of concurrent operations
	const numOps = 100

	// Create a wait group to synchronize goroutines
	var wg sync.WaitGroup
	wg.Add(numOps)

	// Track tokens for validation
	tokenChan := make(chan security.Token, numOps)

	// Run concurrent token creations
	for i := 0; i < numOps; i++ {
		go func(idx int) {
			defer wg.Done()

			// Create unique actor for each goroutine
			actor := security.Actor{
				ID:   fmt.Sprintf("user-%d", idx),
				Meta: registry.Metadata{"index": idx},
			}

			// Create token
			token, err := ts.Create(ctx, actor, nil, security.TokenDetails{})
			if err == nil && token != "" {
				tokenChan <- token
			}
		}(i)
	}

	// Wait for all token creations to complete
	wg.Wait()
	close(tokenChan)

	// Collect tokens
	var tokens []security.Token
	for token := range tokenChan {
		tokens = append(tokens, token)
	}

	// Verify that we got the expected number of tokens
	assert.Len(t, tokens, numOps, "Should have created all tokens successfully")

	// Now validate and revoke tokens concurrently
	wg.Add(len(tokens) * 2) // For validate and revoke

	// Validation errors
	errChan := make(chan error, len(tokens))

	// Run concurrent validations
	for _, token := range tokens {
		go func(tok security.Token) {
			defer wg.Done()

			_, _, err := ts.Validate(ctx, tok)
			if err != nil {
				errChan <- err
			}
		}(token)
	}

	// Run concurrent revocations
	for _, token := range tokens {
		go func(tok security.Token) {
			defer wg.Done()

			err := ts.Revoke(ctx, tok)
			if err != nil {
				errChan <- err
			}
		}(token)
	}

	// Wait for all operations to complete
	wg.Wait()
	close(errChan)

	// Collect errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	// Some operations may fail due to concurrent revocations, but we should
	// have significantly fewer errors than operations
	assert.Less(t, len(errors), numOps, "Should have fewer errors than operations")

	// After all revocations, all tokens should be invalid
	for _, token := range tokens {
		_, _, err := ts.Validate(ctx, token)
		assert.Error(t, err, "Token should be invalid after revocation")
	}
}

// TestStoreResourceCleanup tests that store resources are properly cleaned up
func TestStoreResourceCleanup(t *testing.T) {
	// This test verifies that resources are properly released even when errors occur

	// Setup
	ctx := context.Background()
	logger := zaptest.NewLogger(t)

	storeID := registry.ID{Name: "test-store"}
	memStore := memstore.NewMemoryStore(storeID, nil, logger)

	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer memStore.Stop(ctx)

	select {
	case <-statusChan:
	case <-time.After(time.Second):
		t.Fatal("store failed to start in time")
	}

	resources := newTestResourceRegistry()
	resources.Register(storeID, memStore)
	secRegistry := newTestSecurityRegistry()

	tokenConfig := &tokenstore.Config{
		Store:             storeID,
		TokenLength:       32,
		TokenKey:          "test-signing-key",
		DefaultExpiration: time.Hour,
	}

	ts, err := tokenstore.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
	require.NoError(t, err)

	// Create a token
	actor := security.Actor{ID: "test-user"}
	token, err := ts.Create(ctx, actor, nil, security.TokenDetails{})
	require.NoError(t, err)

	// Validate with a cancelled context to simulate an error
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel() // Cancel immediately

	// This should return an error but not panic
	_, _, err = ts.Validate(cancelCtx, token)
	assert.Error(t, err)

	// We should still be able to validate with a valid context
	validatedActor, _, err := ts.Validate(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, actor.ID, validatedActor.ID)

	// Now stop the store to simulate resource unavailability
	err = memStore.Stop(ctx)
	require.NoError(t, err)

	// Operations should now fail with appropriate errors
	_, err = ts.Create(ctx, actor, nil, security.TokenDetails{})
	assert.Error(t, err)

	_, _, err = ts.Validate(ctx, token)
	assert.Error(t, err)

	err = ts.Revoke(ctx, token)
	assert.Error(t, err)
}

// TestInvalidTokenStore tests that token store creation fails with invalid config
func TestInvalidTokenStore(t *testing.T) {
	// Test with nil config
	_, err := tokenstore.NewStoreTokenStore(nil, &jsonTranscoder{}, nil, nil)
	assert.Error(t, err)

	// Test with invalid config (no store ID)
	invalidConfig := &tokenstore.Config{
		TokenLength:       32,
		DefaultExpiration: time.Hour,
	}
	_, err = tokenstore.NewStoreTokenStore(invalidConfig, &jsonTranscoder{}, nil, nil)
	assert.Error(t, err)

	// Test with invalid config (invalid token length)
	invalidConfig = &tokenstore.Config{
		Store:             registry.ID{Name: "test-store"},
		TokenLength:       0, // Invalid
		DefaultExpiration: time.Hour,
	}
	_, err = tokenstore.NewStoreTokenStore(invalidConfig, &jsonTranscoder{}, nil, nil)
	assert.Error(t, err)
}
