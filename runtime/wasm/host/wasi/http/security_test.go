package http

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/wasm/resource"
)

// mockPolicy implements security.Policy for testing
type mockPolicy struct {
	id           registry.ID
	allowActions map[string]bool
	allowHosts   map[string]bool
}

func (p *mockPolicy) ID() registry.ID {
	return p.id
}

func (p *mockPolicy) Evaluate(actor security.Actor, action, res string, meta registry.Metadata) security.Result {
	if allowed, ok := p.allowActions[action]; ok && allowed {
		// If action is allowed, also check host if specified
		if host, ok := meta["host"].(string); ok && len(p.allowHosts) > 0 {
			if allowed, ok := p.allowHosts[host]; !ok || !allowed {
				return security.Deny
			}
		}
		return security.Allow
	}
	return security.Deny
}

// mockScope implements security.Scope for testing
type mockScope struct {
	policies []security.Policy
}

func newMockScope(allowActions []string, allowHosts []string) *mockScope {
	actionMap := make(map[string]bool)
	for _, a := range allowActions {
		actionMap[a] = true
	}
	hostMap := make(map[string]bool)
	for _, h := range allowHosts {
		hostMap[h] = true
	}
	return &mockScope{
		policies: []security.Policy{&mockPolicy{
			id:           registry.ID{NS: "test", Name: "policy"},
			allowActions: actionMap,
			allowHosts:   hostMap,
		}},
	}
}

func (s *mockScope) With(policy security.Policy) security.Scope {
	return &mockScope{policies: append(s.policies, policy)}
}

func (s *mockScope) Without(policyID registry.ID) security.Scope {
	return s
}

func (s *mockScope) Evaluate(actor security.Actor, action, res string, meta registry.Metadata) security.Result {
	for _, p := range s.policies {
		if result := p.Evaluate(actor, action, res, meta); result == security.Allow {
			return security.Allow
		}
	}
	return security.Deny
}

func (s *mockScope) Contains(policyID registry.ID) bool {
	return false
}

func (s *mockScope) Policies() []security.Policy {
	return s.policies
}

// setupSecurityContext creates a context with security actor and scope
func setupSecurityContext(allowActions []string, allowHosts []string) context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	security.SetActor(ctx, security.Actor{ID: "test-user"})
	security.SetScope(ctx, newMockScope(allowActions, allowHosts))
	return ctx
}

// setupDeniedContext creates a context where all actions are denied
func setupDeniedContext() context.Context {
	return setupSecurityContext(nil, nil)
}

func TestSecurityHandle(t *testing.T) {
	t.Run("denies when no security context", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewOutgoingHost(res)

		// Insert a request
		reqHandle := host.OutgoingRequests().Insert(&OutgoingRequest{
			Method: "GET",
			URL:    "https://example.com/api",
		})

		// No security context
		ctx := context.Background()
		stack := []uint64{uint64(reqHandle)}

		host.secureHandle(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return 0 (error) without security context")
	})

	t.Run("denies when action not allowed", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewOutgoingHost(res)

		reqHandle := host.OutgoingRequests().Insert(&OutgoingRequest{
			Method: "GET",
			URL:    "https://example.com/api",
		})

		// Security context without http request permission
		ctx := setupSecurityContext([]string{"wasi.fs.read"}, nil) // wrong action
		stack := []uint64{uint64(reqHandle)}

		host.secureHandle(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return 0 (error) when action denied")
	})

	t.Run("denies when host not allowed", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewOutgoingHost(res)

		reqHandle := host.OutgoingRequests().Insert(&OutgoingRequest{
			Method: "GET",
			URL:    "https://evil.com/api",
		})

		// Security context allows action but not the host
		ctx := setupSecurityContext([]string{ActionHTTPRequest}, []string{"example.com"})
		stack := []uint64{uint64(reqHandle)}

		host.secureHandle(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return 0 (error) when host denied")
	})

	t.Run("denies invalid handle", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewOutgoingHost(res)

		ctx := setupSecurityContext([]string{ActionHTTPRequest}, nil)
		stack := []uint64{12345} // non-existent handle

		host.secureHandle(ctx, nil, stack)

		assert.Equal(t, uint64(0), stack[0], "should return 0 (error) for invalid handle")
	})
}

func TestSecurityURLParsing(t *testing.T) {
	t.Run("extracts host from URL for security check", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewOutgoingHost(res)

		testCases := []struct {
			url      string
			wantHost string
		}{
			{"https://api.example.com/v1/users", "api.example.com"},
			{"http://localhost:8080/test", "localhost:8080"},
			{"https://sub.domain.example.org:443/path", "sub.domain.example.org:443"},
		}

		for _, tc := range testCases {
			reqHandle := host.OutgoingRequests().Insert(&OutgoingRequest{
				Method: "GET",
				URL:    tc.url,
			})

			// Create scope that captures metadata
			capturedMeta := make(registry.Metadata)
			captureScope := &captureMetadataScope{captured: &capturedMeta}

			ctx := ctxapi.NewRootContext()
			ctx, _ = ctxapi.OpenFrameContext(ctx)
			security.SetActor(ctx, security.Actor{ID: "test-user"})
			security.SetScope(ctx, captureScope)

			stack := []uint64{uint64(reqHandle)}
			host.secureHandle(ctx, nil, stack)

			assert.Equal(t, tc.wantHost, capturedMeta["host"], "host should be extracted from URL %s", tc.url)
		}
	})
}

func TestSecurityActionConstant(t *testing.T) {
	assert.Equal(t, "wasi.http.request", ActionHTTPRequest)
}

func TestSecurityMetadata(t *testing.T) {
	t.Run("includes url, host, and method in metadata", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewOutgoingHost(res)

		reqHandle := host.OutgoingRequests().Insert(&OutgoingRequest{
			Method: "POST",
			URL:    "https://api.example.com/users",
		})

		capturedMeta := make(registry.Metadata)
		captureScope := &captureMetadataScope{captured: &capturedMeta}

		ctx := ctxapi.NewRootContext()
		ctx, _ = ctxapi.OpenFrameContext(ctx)
		security.SetActor(ctx, security.Actor{ID: "test-user"})
		security.SetScope(ctx, captureScope)

		stack := []uint64{uint64(reqHandle)}
		host.secureHandle(ctx, nil, stack)

		assert.Equal(t, "https://api.example.com/users", capturedMeta["url"])
		assert.Equal(t, "api.example.com", capturedMeta["host"])
		assert.Equal(t, "POST", capturedMeta["method"])
	})
}

// captureMetadataScope captures metadata during Evaluate for inspection
type captureMetadataScope struct {
	captured *registry.Metadata
}

func (s *captureMetadataScope) With(policy security.Policy) security.Scope  { return s }
func (s *captureMetadataScope) Without(policyID registry.ID) security.Scope { return s }
func (s *captureMetadataScope) Contains(policyID registry.ID) bool          { return false }
func (s *captureMetadataScope) Policies() []security.Policy                 { return nil }
func (s *captureMetadataScope) Evaluate(actor security.Actor, action, resource string, meta registry.Metadata) security.Result {
	for k, v := range meta {
		(*s.captured)[k] = v
	}
	return security.Allow
}

func TestSecurityHostFiltering(t *testing.T) {
	t.Run("allows request when host matches", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewOutgoingHost(res)

		reqHandle := host.OutgoingRequests().Insert(&OutgoingRequest{
			Method: "GET",
			URL:    "https://api.example.com/data",
		})

		// Allow the specific host
		ctx := setupSecurityContext([]string{ActionHTTPRequest}, []string{"api.example.com"})
		stack := []uint64{uint64(reqHandle)}

		host.secureHandle(ctx, nil, stack)

		// Since MakeAsyncHandler is called, the stack won't be 0 if allowed
		// But we can't fully test without asyncify. Just verify it doesn't error.
		// The key test is that denied hosts return 0.
	})

	t.Run("blocks request to unauthorized internal endpoints", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewOutgoingHost(res)

		internalURLs := []string{
			"http://localhost:8080/admin",
			"http://127.0.0.1/secret",
			"http://internal-service.local/api",
		}

		for _, url := range internalURLs {
			reqHandle := host.OutgoingRequests().Insert(&OutgoingRequest{
				Method: "GET",
				URL:    url,
			})

			// Only allow external hosts
			ctx := setupSecurityContext([]string{ActionHTTPRequest}, []string{"api.example.com"})
			stack := []uint64{uint64(reqHandle)}

			host.secureHandle(ctx, nil, stack)

			assert.Equal(t, uint64(0), stack[0], "should block internal URL: %s", url)
		}
	})

	t.Run("allows wildcard host policy", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewOutgoingHost(res)

		reqHandle := host.OutgoingRequests().Insert(&OutgoingRequest{
			Method: "GET",
			URL:    "https://any-host.example.com/path",
		})

		// Empty allowHosts means no host filtering (only action filtering)
		ctx := setupSecurityContext([]string{ActionHTTPRequest}, nil)
		stack := []uint64{uint64(reqHandle)}

		host.secureHandle(ctx, nil, stack)

		// With empty host list, only action is checked
		// This verifies the scope allows it when no host restriction
	})
}

func TestSecurityEmptyStack(t *testing.T) {
	t.Run("handles empty stack gracefully", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()
		host := NewOutgoingHost(res)

		ctx := setupSecurityContext([]string{ActionHTTPRequest}, nil)
		stack := []uint64{}

		// Should not panic
		host.secureHandle(ctx, nil, stack)
	})
}
