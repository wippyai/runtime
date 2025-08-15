package requirementresolver

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNewResolver(t *testing.T) {
	logger := zap.NewNop()
	resolver := NewResolver(logger)

	assert.NotNil(t, resolver)
	assert.Equal(t, logger, resolver.logger)
}

func TestParseEntryReference(t *testing.T) {
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
			entryRef:     "simple_entry",
			currentNS:    "test.namespace",
			expectedNS:   "test.namespace",
			expectedName: "simple_entry",
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

func TestFindRequirementTargetEntries(t *testing.T) {
	// Create test entries in different namespaces
	entries := []registry.Entry{
		{
			ID:   registry.ID{NS: "app.local", Name: "env-target_api_router"},
			Kind: "function.lua",
		},
		{
			ID:   registry.ID{NS: "wippy.session.api", Name: "get_artifact"},
			Kind: "function.lua",
		},
		{
			ID:   registry.ID{NS: "app.local", Name: "other_function"},
			Kind: "function.lua",
		},
		{
			ID:   registry.ID{NS: "different.ns", Name: "some_entry"},
			Kind: "http.endpoint",
		},
		{
			ID:   registry.ID{NS: "app.local", Name: "third_function"},
			Kind: "function.lua",
		},
	}

	testCases := []struct {
		requirementTarget DefinitionTarget
		currentNS         string
		expectedCount     int
		expectedEntries   []string
		description       string
	}{
		{
			requirementTarget: DefinitionTarget{
				Entry: "env-target_api_router",
				Path:  ".meta.router",
			},
			currentNS:       "app.local",
			expectedCount:   1,
			expectedEntries: []string{"app.local:env-target_api_router"},
			description:     "Local namespace entry reference",
		},
		{
			requirementTarget: DefinitionTarget{
				Entry: "wippy.session.api:get_artifact",
				Path:  ".meta.router",
			},
			currentNS:       "app.local",
			expectedCount:   1,
			expectedEntries: []string{"wippy.session.api:get_artifact"},
			description:     "Cross-namespace entry reference",
		},
		{
			requirementTarget: DefinitionTarget{
				Entry: "",
				Path:  ".meta.depends_on[]",
			},
			currentNS:       "app.local",
			expectedCount:   3,
			expectedEntries: []string{"app.local:env-target_api_router", "app.local:other_function", "app.local:third_function"},
			description:     "Empty entry with path - should find all entries in current namespace",
		},
		{
			requirementTarget: DefinitionTarget{
				Entry: "",
				Path:  ".meta.router",
			},
			currentNS:       "different.ns",
			expectedCount:   1,
			expectedEntries: []string{"different.ns:some_entry"},
			description:     "Empty entry with path in different namespace - should find entries in that namespace",
		},
		{
			requirementTarget: DefinitionTarget{
				Entry: "",
				Path:  "",
			},
			currentNS:       "app.local",
			expectedCount:   0,
			expectedEntries: []string{},
			description:     "Empty entry with empty path - should find no entries",
		},
		{
			requirementTarget: DefinitionTarget{
				Entry: "non_existent",
				Path:  ".meta.router",
			},
			currentNS:       "app.local",
			expectedCount:   0,
			expectedEntries: []string{},
			description:     "Non-existent entry should return empty results",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			results := findRequirementTargetEntries(tc.requirementTarget, tc.currentNS, entries)

			assert.Equal(t, tc.expectedCount, len(results), "Result count should match")

			// Check that the expected entries are found
			resultIDs := make([]string, len(results))
			for i, result := range results {
				resultIDs[i] = result.ID.String()
			}

			for _, expectedEntry := range tc.expectedEntries {
				assert.Contains(t, resultIDs, expectedEntry, "Should contain expected entry")
			}
		})
	}
}

// TestEmptyEntryTargeting specifically tests the empty entry functionality
// This ensures that when requirementTarget.Entry == "", all entries in the current namespace are found
func TestEmptyEntryTargeting(t *testing.T) {
	// Create test entries in different namespaces
	entries := []registry.Entry{
		{
			ID:   registry.ID{NS: "namespace.a", Name: "function1"},
			Kind: "function.lua",
		},
		{
			ID:   registry.ID{NS: "namespace.a", Name: "function2"},
			Kind: "function.lua",
		},
		{
			ID:   registry.ID{NS: "namespace.b", Name: "function3"},
			Kind: "function.lua",
		},
		{
			ID:   registry.ID{NS: "namespace.a", Name: "function4"},
			Kind: "http.endpoint",
		},
	}

	testCases := []struct {
		currentNS       string
		expectedCount   int
		expectedEntries []string
		description     string
	}{
		{
			currentNS:       "namespace.a",
			expectedCount:   3,
			expectedEntries: []string{"namespace.a:function1", "namespace.a:function2", "namespace.a:function4"},
			description:     "Empty entry in namespace.a should find all 3 entries in that namespace",
		},
		{
			currentNS:       "namespace.b",
			expectedCount:   1,
			expectedEntries: []string{"namespace.b:function3"},
			description:     "Empty entry in namespace.b should find 1 entry in that namespace",
		},
		{
			currentNS:       "namespace.c",
			expectedCount:   0,
			expectedEntries: []string{},
			description:     "Empty entry in non-existent namespace should find no entries",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Create a requirement target with empty entry but valid path
			requirementTarget := DefinitionTarget{
				Entry: "", // Empty entry - this is the key test case
				Path:  ".meta.depends_on[]",
			}

			results := findRequirementTargetEntries(requirementTarget, tc.currentNS, entries)

			assert.Equal(t, tc.expectedCount, len(results), "Result count should match")

			// Check that the expected entries are found
			resultIDs := make([]string, len(results))
			for i, result := range results {
				resultIDs[i] = result.ID.String()
			}

			for _, expectedEntry := range tc.expectedEntries {
				assert.Contains(t, resultIDs, expectedEntry, "Should contain expected entry")
			}
		})
	}
}

func TestFindDependencyByParameterName(t *testing.T) {
	// Test the new simplified direct parameter matching approach
	nsDependencies := map[string]registry.Entry{
		"hello_world_dependency": {
			ID: registry.ID{
				NS:   "app.requirements.demo",
				Name: "hello_world_dependency",
			},
			Kind: registry.KindNamespaceDependency,
			Data: payload.New(map[string]interface{}{
				"parameters": []interface{}{
					map[string]interface{}{
						"name":  "API_ROUTER",
						"value": "system:api",
					},
					map[string]interface{}{
						"name":  "NAMESPACE",
						"value": "ns:system",
					},
				},
			}),
		},
	}

	testCases := []struct {
		requirementName string
		expectedValue   string
		shouldFind      bool
	}{
		{
			requirementName: "API_ROUTER",
			expectedValue:   "system:api",
			shouldFind:      true,
		},
		{
			requirementName: "NAMESPACE",
			expectedValue:   "ns:system",
			shouldFind:      true,
		},
		{
			requirementName: "NON_EXISTENT",
			expectedValue:   "",
			shouldFind:      false,
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("Find parameter %s", tc.requirementName), func(t *testing.T) {
			dependency, paramValue, err := findDependencyByParameterName(tc.requirementName, nsDependencies)

			if tc.shouldFind {
				require.NoError(t, err, "Should find dependency parameter")
				assert.Equal(t, "hello_world_dependency", dependency.ID.Name)
				assert.Equal(t, tc.expectedValue, paramValue)
			} else {
				require.Error(t, err, "Should not find dependency parameter")
				assert.Contains(t, err.Error(), "no dependency parameter found")
			}
		})
	}
}

// ParseEntryReference parses an entry reference string to determine target namespace and name
// Supports two formats:
// 1. "entry_name" - resolves to current namespace
// 2. "namespace:entry_name" - resolves to specified namespace
func ParseEntryReference(entryRef string, currentNS string) (targetNS string, targetName string) {
	// Check if the entry reference contains a namespace prefix
	if strings.Contains(entryRef, ":") {
		// Split by ":" to get namespace and entry name
		parts := strings.SplitN(entryRef, ":", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}

	// No namespace prefix, use current namespace
	return currentNS, entryRef
}
