package topology

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ResolverTestSDK provides a fluent API for testing the Resolver
type ResolverTestSDK struct {
	t        *testing.T
	resolver *Resolver
}

// NewResolverTestSDK creates a new test SDK instance with a fresh resolver
func NewResolverTestSDK(t *testing.T) *ResolverTestSDK {
	return &ResolverTestSDK{
		t:        t,
		resolver: NewResolver(),
	}
}

// TestPayloadForResolver implements payload.Payload for testing
type TestPayloadForResolver struct {
	data any
}

func (p *TestPayloadForResolver) Data() any {
	return p.data
}

func (p *TestPayloadForResolver) Format() payload.Format {
	return payload.Golang
}

// EntryFromJSON creates a registry entry from JSON string
func (sdk *ResolverTestSDK) EntryFromJSON(jsonStr string) registry.Entry {
	var entryData struct {
		ID   string         `json:"id"`
		Kind string         `json:"kind"`
		Meta map[string]any `json:"meta,omitempty"`
		Data map[string]any `json:"data,omitempty"`
	}

	err := json.Unmarshal([]byte(jsonStr), &entryData)
	if err != nil {
		sdk.t.Fatalf("Failed to unmarshal entry JSON: %v", err)
	}

	entry := registry.Entry{
		Kind: entryData.Kind,
		Meta: entryData.Meta,
	}

	if entryData.Data != nil {
		entry.Data = &TestPayloadForResolver{data: entryData.Data}
	}

	return entry
}

// RegisterPattern adds a pattern to the resolver
func (sdk *ResolverTestSDK) RegisterPattern(path, description string, allowWildcard bool) *ResolverTestSDK {
	sdk.resolver.RegisterPattern(registry.DependencyPattern{
		Path:          path,
		Description:   description,
		AllowWildcard: allowWildcard,
	})
	return sdk
}

// ExpectDeps extracts dependencies and returns an assertion helper
func (sdk *ResolverTestSDK) ExpectDeps(entryJSON string) *ResolverDepAssertion {
	entry := sdk.EntryFromJSON(entryJSON)
	deps := sdk.resolver.Extract(entry)
	return &ResolverDepAssertion{
		t:    sdk.t,
		deps: deps,
	}
}

// ResolverDepAssertion provides fluent assertions for dependency results
type ResolverDepAssertion struct {
	t    *testing.T
	deps []string
}

func (a *ResolverDepAssertion) ToEqual(expected ...string) {
	assert.ElementsMatch(a.t, expected, a.deps, "Dependencies mismatch")
}

func (a *ResolverDepAssertion) ToBeEmpty() {
	assert.Empty(a.t, a.deps, "Expected no dependencies")
}

func (a *ResolverDepAssertion) ToContain(expected ...string) {
	for _, exp := range expected {
		assert.Contains(a.t, a.deps, exp, "Expected dependency %s", exp)
	}
}

// Test basic resolver functionality
func TestResolver_BasicExtraction(t *testing.T) {
	sdk := NewResolverTestSDK(t)

	sdk.RegisterPattern("meta.router", "Router reference", false)
	sdk.RegisterPattern("data.server", "Server reference", false)

	sdk.ExpectDeps(`{
		"kind": "http.endpoint",
		"meta": {"router": "core:api"},
		"data": {"server": "app:http"}
	}`).ToEqual("core:api", "app:http")
}

// Test pattern registration
func TestResolver_PatternRegistration(t *testing.T) {
	resolver := NewResolver()

	resolver.RegisterPattern(registry.DependencyPattern{Path: "meta.custom", Description: "Custom pattern", AllowWildcard: false})
	resolver.RegisterPattern(registry.DependencyPattern{Path: "data.custom", Description: "Another custom", AllowWildcard: true})

	patterns := resolver.Patterns()
	require.Len(t, patterns, 2, "Should have 2 registered patterns")

	var foundCustom, foundData bool
	for _, p := range patterns {
		if p.Path == "meta.custom" {
			foundCustom = true
			assert.Equal(t, "Custom pattern", p.Description)
			assert.False(t, p.AllowWildcard)
		}
		if p.Path == "data.custom" {
			foundData = true
			assert.True(t, p.AllowWildcard)
		}
	}

	assert.True(t, foundCustom, "Should find meta.custom pattern")
	assert.True(t, foundData, "Should find data.custom pattern")
}

// Test meta path extraction
func TestResolver_MetaPaths(t *testing.T) {
	sdk := NewResolverTestSDK(t)

	sdk.RegisterPattern("meta.server", "Server", false).
		RegisterPattern("meta.router", "Router", false).
		RegisterPattern("meta.parent", "Parent", false)

	sdk.ExpectDeps(`{
		"kind": "http.endpoint",
		"meta": {
			"server": "app:http",
			"router": "core:api",
			"parent": "app:gateway"
		}
	}`).ToEqual("app:http", "core:api", "app:gateway")
}

// Test meta.depends_on wildcard array
func TestResolver_MetaDependsOn(t *testing.T) {
	sdk := NewResolverTestSDK(t)
	sdk.RegisterPattern("meta.depends_on", "Dependencies", true)

	sdk.ExpectDeps(`{
		"kind": "function.lua",
		"meta": {
			"depends_on": ["db:main", "cache:redis", "auth:service"]
		}
	}`).ToEqual("db:main", "cache:redis", "auth:service")
}

// Test data path extraction
func TestResolver_DataPaths(t *testing.T) {
	sdk := NewResolverTestSDK(t)

	sdk.RegisterPattern("data.fs", "Filesystem", false).
		RegisterPattern("data.store", "Store", false).
		RegisterPattern("data.env", "Environment", false)

	sdk.ExpectDeps(`{
		"kind": "process.lua",
		"data": {
			"fs": "app:filesystem",
			"store": "session:main",
			"env": "app:env"
		}
	}`).ToEqual("app:filesystem", "session:main", "app:env")
}

// Test wildcard paths
func TestResolver_WildcardPaths(t *testing.T) {
	sdk := NewResolverTestSDK(t)
	sdk.RegisterPattern("meta.groups", "Groups", true)

	sdk.ExpectDeps(`{
		"kind": "function.lua",
		"meta": {
			"groups": ["admin", "user", "moderator"]
		}
	}`).ToBeEmpty()

	sdk.ExpectDeps(`{
		"kind": "function.lua",
		"meta": {
			"groups": ["group:admin", "group:user"]
		}
	}`).ToEqual("group:admin", "group:user")
}

// Test security paths
func TestResolver_SecurityPaths(t *testing.T) {
	sdk := NewResolverTestSDK(t)
	sdk.RegisterPattern("data.token_store", "Token store", false)

	sdk.ExpectDeps(`{
		"kind": "http.endpoint",
		"data": {
			"token_store": "security:tokens"
		}
	}`).ToEqual("security:tokens")
}

// Test env pattern extraction
func TestResolver_EnvPattern(t *testing.T) {
	sdk := NewResolverTestSDK(t)
	sdk.RegisterPattern("data.env", "Environment", false)

	sdk.ExpectDeps(`{
		"kind": "process.lua",
		"data": {
			"env": "app:env"
		}
	}`).ToEqual("app:env")
}

// Test real-world examples
func TestResolver_RealWorldExamples(t *testing.T) {
	sdk := NewResolverTestSDK(t)

	sdk.RegisterPattern("meta.server", "Server", false).
		RegisterPattern("meta.router", "Router", false).
		RegisterPattern("data.fs", "Filesystem", false).
		RegisterPattern("data.token_store", "Token store", false)

	// HTTP endpoint with server and router
	sdk.ExpectDeps(`{
		"kind": "http.endpoint",
		"meta": {
			"server": "app:http",
			"router": "core:api"
		},
		"data": {
			"token_store": "security:tokens"
		}
	}`).ToEqual("app:http", "core:api", "security:tokens")

	// Process with filesystem
	sdk.ExpectDeps(`{
		"kind": "process.lua",
		"data": {
			"fs": "app:filesystem"
		}
	}`).ToEqual("app:filesystem")
}

// Test edge cases
func TestResolver_EdgeCases(t *testing.T) {
	sdk := NewResolverTestSDK(t)
	sdk.RegisterPattern("meta.router", "Router", false)

	// Empty entry
	sdk.ExpectDeps(`{"kind": "test"}`).ToBeEmpty()

	// Null values
	sdk.ExpectDeps(`{
		"kind": "test",
		"meta": {"router": null}
	}`).ToBeEmpty()

	// Non-string values (should be ignored)
	sdk.ExpectDeps(`{
		"kind": "test",
		"meta": {"router": 123}
	}`).ToBeEmpty()

	// Empty strings (should be filtered)
	sdk.ExpectDeps(`{
		"kind": "test",
		"meta": {"router": ""}
	}`).ToBeEmpty()
}

// Test nested path extraction
func TestResolver_NestedPaths(t *testing.T) {
	sdk := NewResolverTestSDK(t)
	sdk.RegisterPattern("data.connection.host", "Host", false)

	sdk.ExpectDeps(`{
		"kind": "sql.connection",
		"data": {
			"connection": {
				"host": "db:main"
			}
		}
	}`).ToEqual("db:main")
}

// Test array handling
func TestResolver_ArrayHandling(t *testing.T) {
	sdk := NewResolverTestSDK(t)
	sdk.RegisterPattern("meta.depends_on", "Dependencies", true)

	// Array of dependencies
	sdk.ExpectDeps(`{
		"kind": "function.lua",
		"meta": {
			"depends_on": ["db:main", "cache:redis"]
		}
	}`).ToEqual("db:main", "cache:redis")

	// Array with duplicates (should be deduplicated)
	sdk.ExpectDeps(`{
		"kind": "function.lua",
		"meta": {
			"depends_on": ["db:main", "db:main", "cache:redis"]
		}
	}`).ToEqual("db:main", "cache:redis")
}

// Test concurrent access (thread safety)
func TestResolver_ThreadSafety(t *testing.T) {
	resolver := NewResolver()

	// Register initial patterns
	resolver.RegisterPattern(registry.DependencyPattern{Path: "meta.router", Description: "Router"})
	resolver.RegisterPattern(registry.DependencyPattern{Path: "data.server", Description: "Server"})

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent pattern registration
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resolver.RegisterPattern(registry.DependencyPattern{
				Path:        "meta.custom" + string(rune(idx)),
				Description: "Custom",
			})
		}(i)
	}

	// Concurrent extraction
	entry := registry.Entry{
		Kind: "test",
		Meta: map[string]any{
			"router": "core:api",
		},
	}

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deps := resolver.Extract(entry)
			assert.Contains(t, deps, "core:api")
		}()
	}

	wg.Wait()

	// Verify patterns were registered
	patterns := resolver.Patterns()
	assert.GreaterOrEqual(t, len(patterns), 2, "Should have at least 2 initial patterns")
}

// Test pattern override (registering same path twice)
func TestResolver_PatternOverride(t *testing.T) {
	resolver := NewResolver()

	resolver.RegisterPattern(registry.DependencyPattern{Path: "meta.custom", Description: "First description", AllowWildcard: false})
	resolver.RegisterPattern(registry.DependencyPattern{Path: "meta.custom", Description: "Second description", AllowWildcard: true})

	patterns := resolver.Patterns()

	customCount := 0
	for _, p := range patterns {
		if p.Path == "meta.custom" {
			customCount++
		}
	}

	assert.Equal(t, 2, customCount, "Should allow duplicate pattern registration")
}

// Test extraction with no patterns
func TestResolver_NoPatternsRegistered(t *testing.T) {
	resolver := &Resolver{patterns: []PathConfig{}}

	entry := registry.Entry{
		Kind: "test",
		Meta: map[string]any{
			"router": "core:api",
		},
	}

	deps := resolver.Extract(entry)
	assert.Empty(t, deps, "Should return empty when no patterns registered")
}

// Test extraction with empty entry
func TestResolver_EmptyEntry(t *testing.T) {
	resolver := NewResolver()

	entry := registry.Entry{
		Kind: "test",
	}

	deps := resolver.Extract(entry)
	assert.Empty(t, deps, "Should return empty for entry with no meta or data")
}

// Test complex nested structure
func TestResolver_ComplexNested(t *testing.T) {
	sdk := NewResolverTestSDK(t)
	sdk.RegisterPattern("data.config.database.primary", "DB Primary", false)

	sdk.ExpectDeps(`{
		"kind": "service",
		"data": {
			"config": {
				"database": {
					"primary": "db:main",
					"replica": "db:replica"
				}
			}
		}
	}`).ToEqual("db:main")
}

// Benchmark resolver extraction
func BenchmarkResolver_Extract(b *testing.B) {
	resolver := NewResolver()

	entry := registry.Entry{
		Kind: "http.endpoint",
		Meta: map[string]any{
			"server":     "app:http",
			"router":     "core:api",
			"parent":     "app:gateway",
			"depends_on": []string{"db:main", "cache:redis", "auth:service"},
		},
		Data: &TestPayloadForResolver{
			data: map[string]any{
				"fs":          "app:filesystem",
				"token_store": "security:tokens",
				"env":         "app:env",
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = resolver.Extract(entry)
	}
}

// Benchmark pattern registration
func BenchmarkResolver_RegisterPattern(b *testing.B) {
	resolver := NewResolver()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolver.RegisterPattern(registry.DependencyPattern{
			Path:        "meta.test",
			Description: "Test pattern",
		})
	}
}
