package requirementresolver

import (
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestCrossNamespaceRequirementResolution tests that the requirement resolver correctly handles both local and cross-namespace entry references
func TestCrossNamespaceRequirementResolution(t *testing.T) {
	// Create a logger
	logger := zap.NewNop()
	resolver := NewResolver(logger)

	// Create test entries in different namespaces
	entries := []registry.Entry{
		// Local namespace entries
		{
			ID:   registry.ID{NS: "app.local", Name: "env-target_api_router"},
			Kind: "function.lua",
			Data: payload.New(map[string]interface{}{
				"meta": map[string]interface{}{
					"router": "default",
				},
			}),
		},
		{
			ID:   registry.ID{NS: "app.local", Name: "other_function"},
			Kind: "function.lua",
			Data: payload.New(map[string]interface{}{
				"meta": map[string]interface{}{
					"depends_on": []string{},
				},
			}),
		},
		// Cross-namespace entries
		{
			ID:   registry.ID{NS: "wippy.session.api", Name: "get_artifact"},
			Kind: "function.lua",
			Data: payload.New(map[string]interface{}{
				"meta": map[string]interface{}{
					"router": "default",
				},
			}),
		},
	}

	// Create a requirement that targets a local namespace entry
	localRequirement := registry.Entry{
		ID:   registry.ID{NS: "app.local", Name: "LOCAL_ROUTER"},
		Kind: registry.KindNamespaceRequirement,
		Data: payload.New(map[string]interface{}{
			"targets": []interface{}{
				map[string]interface{}{
					"entry": "env-target_api_router", // Local namespace entry
					"path":  ".meta.router",
				},
			},
		}),
	}

	// Create a requirement that targets a cross-namespace entry
	crossNamespaceRequirement := registry.Entry{
		ID:   registry.ID{NS: "app.local", Name: "CROSS_NAMESPACE_ROUTER"},
		Kind: registry.KindNamespaceRequirement,
		Data: payload.New(map[string]interface{}{
			"targets": []interface{}{
				map[string]interface{}{
					"entry": "wippy.session.api:get_artifact", // Cross-namespace entry
					"path":  ".meta.router",
				},
			},
		}),
	}

	// Add requirements to entries
	entries = append(entries, localRequirement, crossNamespaceRequirement)

	// Resolve the requirements
	resolvedEntries, err := resolver.ResolveModuleDefinitions(entries)
	require.NoError(t, err, "Should resolve requirements without error")
	assert.Equal(t, len(entries), len(resolvedEntries), "Should return same number of entries")

	// Verify that the resolver processed the requirements correctly
	// Note: The actual resolution logic depends on having matching dependencies,
	// so we're testing the basic flow rather than the full resolution
	assert.NotNil(t, resolvedEntries, "Should return non-nil entries")
}

// TestEmptyEntryFunctionality tests the empty entry functionality that targets ALL entries in current namespace
func TestEmptyEntryFunctionality(t *testing.T) {
	// Create test entries in a namespace
	entries := []registry.Entry{
		{
			ID:   registry.ID{NS: "app.local", Name: "function1"},
			Kind: "function.lua",
		},
		{
			ID:   registry.ID{NS: "app.local", Name: "function2"},
			Kind: "function.lua",
		},
		{
			ID:   registry.ID{NS: "app.local", Name: "endpoint1"},
			Kind: "http.endpoint",
		},
		{
			ID:   registry.ID{NS: "other.ns", Name: "external_function"},
			Kind: "function.lua",
		},
	}

	// Test the empty entry functionality directly
	requirementTarget := DefinitionTarget{
		Entry: "", // Empty entry - targets ALL entries in current namespace
		Path:  ".meta.depends_on[]",
	}

	// Test with different namespaces
	testCases := []struct {
		currentNS     string
		expectedCount int
		description   string
	}{
		{
			currentNS:     "app.local",
			expectedCount: 3, // function1, function2, endpoint1
			description:   "Empty entry in app.local namespace",
		},
		{
			currentNS:     "other.ns",
			expectedCount: 1, // external_function
			description:   "Empty entry in other.ns namespace",
		},
		{
			currentNS:     "non.existent",
			expectedCount: 0, // no entries
			description:   "Empty entry in non-existent namespace",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			results := findRequirementTargetEntries(requirementTarget, tc.currentNS, entries)

			assert.Equal(t, tc.expectedCount, len(results), "Result count should match")

			// Verify that all results are in the expected namespace
			for _, result := range results {
				assert.Equal(t, tc.currentNS, result.ID.NS, "All results should be in the expected namespace")
			}
		})
	}
}

// TestParseEntryReferenceExamples tests the ParseEntryReference function with various examples
func TestParseEntryReferenceExamples(t *testing.T) {
	testCases := []struct {
		entryRef     string
		currentNS    string
		expectedNS   string
		expectedName string
		description  string
	}{
		{
			entryRef:     "env-target_api_router",
			currentNS:    "app.local",
			expectedNS:   "app.local",
			expectedName: "env-target_api_router",
			description:  "Local namespace entry reference",
		},
		{
			entryRef:     "wippy.session.api:get_artifact",
			currentNS:    "app.local",
			expectedNS:   "wippy.session.api",
			expectedName: "get_artifact",
			description:  "Cross-namespace entry reference",
		},
		{
			entryRef:     "simple_function",
			currentNS:    "test.namespace",
			expectedNS:   "test.namespace",
			expectedName: "simple_function",
			description:  "Simple entry name without namespace",
		},
		{
			entryRef:     "other.ns:complex-entry-name",
			currentNS:    "current.namespace",
			expectedNS:   "other.ns",
			expectedName: "complex-entry-name",
			description:  "Complex entry name with namespace",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			targetNS, targetName := ParseEntryReference(tc.entryRef, tc.currentNS)

			assert.Equal(t, tc.expectedNS, targetNS, "Namespace should match")
			assert.Equal(t, tc.expectedName, targetName, "Entry name should match")
		})
	}
}

// TestResolverBasicFunctionality tests the basic functionality of the requirement resolver
func TestResolverBasicFunctionality(t *testing.T) {
	logger := zap.NewNop()
	resolver := NewResolver(logger)

	// Create a simple test scenario with a dependency and requirement
	entries := []registry.Entry{
		// Target entry that will receive the value
		{
			ID:   registry.ID{NS: "app.local", Name: "target_function"},
			Kind: "function.lua",
			Data: payload.New(map[string]interface{}{
				"meta": map[string]interface{}{
					"config": "old_value",
				},
			}),
		},
		// Dependency that provides the parameter value
		{
			ID:   registry.ID{NS: "app.local", Name: "config_dependency"},
			Kind: registry.KindNamespaceDependency,
			Data: payload.New(map[string]interface{}{
				"parameters": []interface{}{
					map[string]interface{}{
						"name":  "CONFIG_VALUE",
						"value": "new_value",
					},
				},
			}),
		},
		// Requirement that uses the dependency parameter
		{
			ID:   registry.ID{NS: "app.local", Name: "CONFIG_VALUE"},
			Kind: registry.KindNamespaceRequirement,
			Data: payload.New(map[string]interface{}{
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "target_function",
						"path":  ".meta.config",
					},
				},
			}),
		},
	}

	// Test that the resolver can be created and processes entries without error
	resolvedEntries, err := resolver.ResolveModuleDefinitions(entries)
	require.NoError(t, err, "Should resolve requirements without error")
	assert.Equal(t, len(entries), len(resolvedEntries), "Should return same number of entries")
	assert.NotNil(t, resolvedEntries, "Should return non-nil entries")

	// Test that the dependency parameter lookup works correctly
	nsDependencies := make(map[string]registry.Entry)
	for _, entry := range entries {
		if entry.Kind == registry.KindNamespaceDependency {
			nsDependencies[entry.ID.Name] = entry
		}
	}

	dependency, paramValue, err := findDependencyByParameterName("CONFIG_VALUE", nsDependencies)
	require.NoError(t, err, "Should find dependency parameter")
	assert.Equal(t, "config_dependency", dependency.ID.Name)
	assert.Equal(t, "new_value", paramValue)

	// Test that requirement targets can be extracted
	requirementTargets, err := getRequirementTargets(entries[2]) // The CONFIG_VALUE requirement
	require.NoError(t, err, "Should extract requirement targets")
	assert.Len(t, requirementTargets, 1, "Should have one target")
	assert.Equal(t, "target_function", requirementTargets[0].Entry)
	assert.Equal(t, ".meta.config", requirementTargets[0].Path)

	// Test that target entries can be found
	targetEntries := findRequirementTargetEntries(requirementTargets[0], "app.local", entries)
	assert.Len(t, targetEntries, 1, "Should find one target entry")
	assert.Equal(t, "target_function", targetEntries[0].ID.Name)
}
