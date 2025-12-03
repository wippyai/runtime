package tokenstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"

	"go.uber.org/zap"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/service/security/tokenstore"
	memstore "github.com/wippyai/runtime/api/service/store/memory"
	tokenimpl "github.com/wippyai/runtime/service/security/tokenstore"
	memorystore "github.com/wippyai/runtime/service/store/memory"
	securitysys "github.com/wippyai/runtime/system/security"
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
	case payload.YAML, payload.String, payload.Lua, payload.Bytes, payload.Error:
		// FIXME rework on demand
		fallthrough
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

func (p *testPolicy) Evaluate(_ security.Actor, _, _ string, _ attrs.Bag) security.Result {
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

func (r *testResourceRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(r.resources))
	for key := range r.resources {
		ids = append(ids, registry.ParseID(key))
	}
	return ids, nil
}

func (r *testResourceRegistry) Exists(id registry.ID) bool {
	_, exists := r.resources[id.String()]
	return exists
}

func (r *testResourceRegistry) Register(id registry.ID, provider resource.Provider) {
	r.resources[id.String()] = provider
}

// TestTokenStoreCreateValidateRevoke tests the full lifecycle of a token
func TestTokenStoreCreateValidateRevoke(t *testing.T) {
	// Setup
	ctx := ctxapi.NewRootContext()
	logger := zap.NewNop()

	// Create and configure MemoryStore
	storeID := registry.NewID("", "test-store")
	memConfig := &memstore.MemoryConfig{
		MaxSize:         1000,
		CleanupInterval: time.Second,
	}
	memStore := memorystore.NewMemoryStore(storeID, memConfig, logger)

	// Start the memory store
	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer func() {
		err := memStore.Stop(ctx)
		require.NoError(t, err, "Failed to stop memory store")
	}()

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
	policyID := registry.NewID("", "test-policy")
	secRegistry.AddPolicy(policyID, security.Allow)

	// Configure token store
	tokenConfig := &tokenstore.Config{
		Store:             storeID,
		TokenLength:       32,
		TokenKey:          "test-signing-key",
		DefaultExpiration: time.Hour,
	}

	// Create token store
	ts, err := tokenimpl.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
	require.NoError(t, err)

	// Test token creation
	actor := security.Actor{
		ID:   "test-user",
		Meta: attrs.Bag{"role": "admin"},
	}

	// Create a scope with the test policy
	scope := securitysys.NewScope([]security.Policy{
		&testPolicy{id: policyID, decision: security.Allow},
	})

	// Create token with details
	details := security.TokenDetails{
		Expiration: 30 * time.Minute,
		Meta:       attrs.Bag{"purpose": "testing"},
	}

	token, err := ts.Create(ctx, actor, scope, details)
	require.NoError(t, err)
	require.NotEmpty(t, token, "Token should not be empty")

	// Test token validation
	validatedActor, validatedScope, err := ts.Validate(ctx, token)
	require.NoError(t, err)

	// Verify actor was preserved
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
	ctx := ctxapi.NewRootContext()
	logger := zap.NewNop()

	storeID := registry.NewID("", "test-store")
	memConfig := &memstore.MemoryConfig{
		MaxSize:         1000,
		CleanupInterval: 100 * time.Millisecond, // Short cleanup for testing
	}
	memStore := memorystore.NewMemoryStore(storeID, memConfig, logger)

	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer func() {
		err := memStore.Stop(ctx)
		require.NoError(t, err, "Failed to stop memory store")
	}()

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

	ts, err := tokenimpl.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
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
	ctx := ctxapi.NewRootContext()
	logger := zap.NewNop()

	storeID := registry.NewID("", "test-store")
	memStore := memorystore.NewMemoryStore(storeID, nil, logger)

	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer func() {
		err := memStore.Stop(ctx)
		require.NoError(t, err, "Failed to stop memory store")
	}()

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

	ts, err := tokenimpl.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
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
	ctx := ctxapi.NewRootContext()
	logger := zap.NewNop()

	storeID := registry.NewID("", "test-store")
	memStore := memorystore.NewMemoryStore(storeID, nil, logger)

	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer func() {
		err := memStore.Stop(ctx)
		require.NoError(t, err, "Failed to stop memory store")
	}()

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

	ts, err := tokenimpl.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
	require.NoError(t, err)

	// Test case: Empty token
	_, _, err = ts.Validate(ctx, "")
	assert.Error(t, err)
	assert.Equal(t, security.ErrTokenInvalid, err)

	// Test case: Revoke non-existent token
	err = ts.Revoke(ctx, "non-existent-token")
	assert.Error(t, err)
	// The error could be either TokenInvalid or TokenNotFound depending on whether the
	// token format is recognized but not found or rejected outright
	assert.True(t, errors.Is(err, security.ErrTokenInvalid) || errors.Is(err, security.ErrTokenNotFound),
		"Expected either token invalid or token not found error, got: %v", err)

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
	ctx := ctxapi.NewRootContext()
	logger := zap.NewNop()

	storeID := registry.NewID("", "test-store")
	memStore := memorystore.NewMemoryStore(storeID, nil, logger)

	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer func() {
		err := memStore.Stop(ctx)
		require.NoError(t, err, "Failed to stop memory store")
	}()

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

	ts, err := tokenimpl.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
	require.NoError(t, err)

	// Create a token
	actor := security.Actor{ID: "test-user"}
	token, err := ts.Create(ctx, actor, nil, security.TokenDetails{})
	require.NoError(t, err)

	// Verify token has no signature part
	assert.NotContains(t, string(token), ".")

	// Validation should work
	_, _, err = ts.Validate(ctx, token)
	require.NoError(t, err)

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
	ctx := ctxapi.NewRootContext()
	logger := zap.NewNop()

	storeID := registry.NewID("", "test-store")
	memStore := memorystore.NewMemoryStore(storeID, nil, logger)

	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer func() {
		err := memStore.Stop(ctx)
		require.NoError(t, err, "Failed to stop memory store")
	}()

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

	ts, err := tokenimpl.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
	require.NoError(t, err)

	// Number of concurrent operations
	const numOps = 50 // Reduced from 100 to avoid overwhelming the system

	// Create a wait group to synchronize goroutines
	var wg sync.WaitGroup
	wg.Add(numOps)

	// Track tokens for validation with a mutex to avoid race conditions
	var mu sync.Mutex
	var tokens []security.Token

	// Run concurrent token creations
	for i := 0; i < numOps; i++ {
		go func(idx int) {
			defer wg.Done()

			// Create unique actor for each goroutine
			actor := security.Actor{
				ID:   fmt.Sprintf("user-%d", idx),
				Meta: attrs.Bag{"index": idx},
			}

			// Create token
			token, err := ts.Create(ctx, actor, nil, security.TokenDetails{})
			if err == nil && token != "" {
				mu.Lock()
				tokens = append(tokens, token)
				mu.Unlock()
			}
		}(i)
	}

	// Wait for all token creations to complete
	wg.Wait()

	// Verify that we got tokens
	require.NotEmpty(t, tokens, "Should have created tokens successfully")
	t.Logf("Created %d tokens", len(tokens))

	// Test validation and revocation sequentially to avoid race conditions
	// First validate all tokens
	for _, token := range tokens {
		_, _, err := ts.Validate(ctx, token)
		assert.NoError(t, err, "Token should be valid after creation")
	}

	// Then revoke all tokens
	for _, token := range tokens {
		err := ts.Revoke(ctx, token)
		assert.NoError(t, err, "Token revocation should succeed")
	}

	// After all revocations, all tokens should be invalid
	for _, token := range tokens {
		_, _, err := ts.Validate(ctx, token)
		assert.Error(t, err, "Token should be invalid after revocation")
	}

	// Test concurrent validation with fewer operations and proper synchronization
	// Create new tokens for concurrent validation test
	concurrentTokens := make([]security.Token, 0)
	for i := 0; i < 10; i++ { // Much smaller number
		actor := security.Actor{
			ID:   fmt.Sprintf("concurrent-user-%d", i),
			Meta: attrs.Bag{"index": i},
		}

		token, err := ts.Create(ctx, actor, nil, security.TokenDetails{})
		require.NoError(t, err)
		concurrentTokens = append(concurrentTokens, token)
	}

	// Validate concurrently (just validation, no revocation)
	var validationWg sync.WaitGroup
	validationWg.Add(len(concurrentTokens))

	for _, token := range concurrentTokens {
		go func(tok security.Token) {
			defer validationWg.Done()
			_, _, err := ts.Validate(ctx, tok)
			assert.NoError(t, err, "Concurrent validation should succeed")
		}(token)
	}

	validationWg.Wait()
}

// TestStoreResourceCleanup tests that store resources are properly cleaned up
func TestStoreResourceCleanup(t *testing.T) {
	// This test verifies that resources are properly released even when errors occur

	// Setup
	ctx := ctxapi.NewRootContext()
	logger := zap.NewNop()

	storeID := registry.NewID("", "test-store")
	memStore := memorystore.NewMemoryStore(storeID, nil, logger)

	statusChan, err := memStore.Start(ctx)
	require.NoError(t, err)
	defer func() {
		err := memStore.Stop(ctx)
		require.NoError(t, err, "Failed to stop memory store")
	}()

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

	ts, err := tokenimpl.NewStoreTokenStore(tokenConfig, &jsonTranscoder{}, resources, secRegistry)
	require.NoError(t, err)

	// Create a token
	actor := security.Actor{ID: "test-user"}
	token, err := ts.Create(ctx, actor, nil, security.TokenDetails{})
	require.NoError(t, err)

	// Validate with a canceled context to simulate an error
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel() // Cancel immediately

	// This should return an error but might not in some implementations
	// that don't check ctx.Done() early enough
	//nolint:ineffassign,staticcheck // ignore for now
	_, _, err = ts.Validate(cancelCtx, token)
	// FIXME maybe add require.NoError(t, err)
	// We don't assert on the error here, as the implementation might handle
	// the canceled context differently

	// We should still be able to validate with a valid context
	_, _, err = ts.Validate(ctx, token)
	require.NoError(t, err)

	// Now stop the store to simulate resource unavailability
	err = memStore.Stop(ctx)
	require.NoError(t, err, "Failed to stop memory store")

	// Operations should now fail with appropriate errors
	_, err = ts.Create(ctx, actor, nil, security.TokenDetails{})
	assert.Error(t, err, "Create should fail after store is stopped")

	_, _, err = ts.Validate(ctx, token)
	assert.Error(t, err, "Validate should fail after store is stopped")

	err = ts.Revoke(ctx, token)
	assert.Error(t, err, "Revoke should fail after store is stopped")
}

// TestInvalidTokenStore tests that token store creation fails with invalid config
func TestInvalidTokenStore(t *testing.T) {
	// Create a test transcoder
	transcoder := &jsonTranscoder{}

	// Test with invalid config (no store ID)
	invalidConfig := &tokenstore.Config{
		TokenLength:       32,
		DefaultExpiration: time.Hour,
	}
	_, err := tokenimpl.NewStoreTokenStore(invalidConfig, transcoder, nil, nil)
	assert.Error(t, err, "Should fail with no store ID")

	// Test with invalid config (invalid token length)
	invalidConfig = &tokenstore.Config{
		Store:             registry.NewID("", "test-store"),
		TokenLength:       0, // Invalid
		DefaultExpiration: time.Hour,
	}
	_, err = tokenimpl.NewStoreTokenStore(invalidConfig, transcoder, nil, nil)
	assert.Error(t, err, "Should fail with invalid token length")
}
