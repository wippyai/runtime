package tokenstore

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	securitysys "github.com/ponyruntime/pony/system/security"
	"strings"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/security"
	"github.com/ponyruntime/pony/api/service/tokenstore"
	"github.com/ponyruntime/pony/api/store"
)

// tokenData is the internal structure stored in the key-value store for each token
type tokenData struct {
	// ActorID is the ID of the actor associated with the token
	ActorID string `json:"actor_id"`

	// ActorMeta contains the actor's metadata
	ActorMeta registry.Metadata `json:"actor_meta,omitempty"`

	// ScopePolicies contains the IDs of policies in the scope
	ScopePolicies []registry.ID `json:"scope_policies,omitempty"`

	// Created is the time when the token was created
	Created time.Time `json:"created"`

	// Expires is the time when the token expires (nil = never)
	Expires *time.Time `json:"expires,omitempty"`

	// Meta contains additional metadata provided at creation time
	Meta registry.Metadata `json:"meta,omitempty"`
}

// TokenStore implements security.TokenStore using a key-value store
type TokenStore struct {
	config    *tokenstore.Config
	dtt       payload.Transcoder
	resources resource.Registry
	registry  security.Registry
}

// NewStoreTokenStore creates a new token store that uses a key-value store for backend storage
func NewStoreTokenStore(
	config *tokenstore.Config,
	dtt payload.Transcoder,
	resources resource.Registry,
	securityRegistry security.Registry,
) (*TokenStore, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid token store config: %w", err)
	}

	return &TokenStore{
		config:    config,
		dtt:       dtt,
		resources: resources,
		registry:  securityRegistry,
	}, nil
}

// acquireStore gets the backing store only when needed
func (s *TokenStore) acquireStore(ctx context.Context) (store.Store, resource.Resource[any], error) {
	// Acquire the backing store
	storeRes, err := s.resources.Acquire(ctx, s.config.Store, resource.ModeNormal)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to acquire backing store '%s': %w", s.config.Store, err)
	}

	// Get the store implementation
	storeImpl, err := storeRes.Get()
	if err != nil {
		storeRes.Release()
		return nil, nil, fmt.Errorf("failed to get store implementation: %w", err)
	}

	// Ensure it's a store.Store
	kvStore, ok := storeImpl.(store.Store)
	if !ok {
		storeRes.Release()
		return nil, nil, fmt.Errorf("resource '%s' is not a key-value store", s.config.Store)
	}

	return kvStore, storeRes, nil
}

// Create generates a new token for the given actor and scope
func (s *TokenStore) Create(
	ctx context.Context,
	actor security.Actor,
	scope security.Scope,
	details security.TokenDetails,
) (security.Token, error) {
	// Acquire store only when needed
	kvStore, storeRes, err := s.acquireStore(ctx)
	if err != nil {
		return "", err
	}
	defer storeRes.Release() // Release after use

	// Generate token string
	tokenStr, err := s.generateToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	// Set expiration
	expiration := details.Expiration
	if expiration == 0 {
		expiration = s.config.DefaultExpiration
	}

	// Compute expiration time if an expiration is set
	var expires *time.Time
	if expiration > 0 {
		exp := time.Now().Add(expiration)
		expires = &exp
	}

	// Extract policies from scope
	var policies []registry.ID
	if scope != nil {
		for _, policy := range scope.Policies() {
			policies = append(policies, policy.ID())
		}
	}

	// Create token data
	data := tokenData{
		ActorID:       actor.ID,
		ActorMeta:     actor.Meta,
		ScopePolicies: policies,
		Created:       time.Now(),
		Expires:       expires,
		Meta:          details.Meta,
	}

	// Store token data in backing store
	value := payload.New(data)
	tokenKey := registry.ParseID(string(tokenStr))

	err = kvStore.Set(ctx, store.Entry{
		Key:   tokenKey,
		Value: value,
		TTL:   expiration,
	})

	if err != nil {
		return "", fmt.Errorf("failed to store token: %w", err)
	}

	return security.Token(tokenStr), nil
}

// Validate checks if a token is valid and returns the associated actor and scope
func (s *TokenStore) Validate(ctx context.Context, token security.Token) (security.Actor, security.Scope, error) {
	// Check token validity
	if token == "" {
		return security.Actor{}, nil, security.ErrTokenInvalid
	}

	// Verify token signature if a key is configured
	if s.config.TokenKey != "" {
		parts := splitToken(string(token))
		if len(parts) != 2 {
			return security.Actor{}, nil, security.ErrTokenInvalid
		}

		// Verify signature
		if !s.verifySignature(parts[0], parts[1]) {
			return security.Actor{}, nil, security.ErrTokenInvalid
		}

		// Use only the token part for lookup
		token = security.Token(parts[0])
	}

	// Acquire store only when needed
	kvStore, storeRes, err := s.acquireStore(ctx)
	if err != nil {
		return security.Actor{}, nil, err
	}
	defer storeRes.Release() // Release after use

	// Retrieve token data from store
	tokenKey := registry.ParseID(string(token))
	value, err := kvStore.Get(ctx, tokenKey)
	if err != nil {
		if errors.Is(err, store.ErrKeyNotFound) {
			return security.Actor{}, nil, security.ErrTokenNotFound
		}
		return security.Actor{}, nil, fmt.Errorf("failed to retrieve token: %w", err)
	}

	// Unmarshal token data
	var data tokenData
	if err := s.dtt.Unmarshal(value, &data); err != nil {
		return security.Actor{}, nil, fmt.Errorf("failed to unmarshal token data: %w", err)
	}

	// Check expiration
	if data.Expires != nil && time.Now().After(*data.Expires) {
		// Clean up expired token
		_ = kvStore.Delete(ctx, tokenKey)
		return security.Actor{}, nil, security.ErrTokenExpired
	}

	// Reconstruct actor
	actor := security.Actor{
		ID:   data.ActorID,
		Meta: data.ActorMeta,
	}

	// Collect policies from registry
	var policies []security.Policy
	for _, policyID := range data.ScopePolicies {
		policy, err := s.registry.GetPolicy(policyID)
		if err == nil && policy != nil {
			policies = append(policies, policy)
		}
	}

	// Create scope from policies
	scope := securitysys.NewScope(policies)

	return actor, scope, nil
}

// Revoke invalidates a token
func (s *TokenStore) Revoke(ctx context.Context, token security.Token) error {
	// Check token validity
	if token == "" {
		return security.ErrTokenInvalid
	}

	// Extract token part if signed
	if s.config.TokenKey != "" {
		parts := splitToken(string(token))
		if len(parts) != 2 {
			return security.ErrTokenInvalid
		}
		token = security.Token(parts[0])
	}

	// Acquire store only when needed
	kvStore, storeRes, err := s.acquireStore(ctx)
	if err != nil {
		return err
	}
	defer storeRes.Release() // Release after use

	// Delete the token from store
	tokenKey := registry.ParseID(string(token))
	if err := kvStore.Delete(ctx, tokenKey); err != nil {
		if errors.Is(err, store.ErrKeyNotFound) {
			return security.ErrTokenNotFound
		}
		return fmt.Errorf("failed to delete token: %w", err)
	}

	return nil
}

// generateToken creates a new random token string
func (s *TokenStore) generateToken() (string, error) {
	// Generate random bytes
	tokenBytes := make([]byte, s.config.TokenLength)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}

	// Base64-encode the token
	tokenStr := base64.URLEncoding.EncodeToString(tokenBytes)

	// Sign the token if a key is configured
	if s.config.TokenKey != "" {
		signature := s.generateSignature(tokenStr)
		return tokenStr + "." + signature, nil
	}

	return tokenStr, nil
}

// generateSignature creates an HMAC signature for a token
func (s *TokenStore) generateSignature(token string) string {
	h := hmac.New(sha256.New, []byte(s.config.TokenKey))
	h.Write([]byte(token))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}

// verifySignature checks if a signature is valid for a token
func (s *TokenStore) verifySignature(token, signature string) bool {
	expectedSig := s.generateSignature(token)
	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

// splitToken splits a token into the token part and signature part
func splitToken(token string) []string {
	parts := strings.Split(token, ".")
	if len(parts) == 2 {
		return parts
	}
	return []string{token, ""}
}
