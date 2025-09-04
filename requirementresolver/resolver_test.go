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

// TestFindDependencyByParameterNameEdgeCases tests edge cases for findDependencyByParameterName
func TestFindDependencyByParameterNameEdgeCases(t *testing.T) {
	testCases := []struct {
		name            string
		nsDependencies  map[string]registry.Entry
		requirementName string
		shouldFind      bool
		description     string
	}{
		{
			name: "dependency_without_parameters_field",
			nsDependencies: map[string]registry.Entry{
				"dependency1": {
					ID:   registry.ID{NS: "app.local", Name: "dependency1"},
					Kind: registry.KindNamespaceDependency,
					Data: payload.New(map[string]interface{}{
						"component": "wippy/llm",
						"version":   ">=v0.0.7",
						// No parameters field
					}),
				},
			},
			requirementName: "PARAM1",
			shouldFind:      false,
			description:     "Dependency without parameters field",
		},
		{
			name: "dependency_with_nil_parameters_field",
			nsDependencies: map[string]registry.Entry{
				"dependency1": {
					ID:   registry.ID{NS: "app.local", Name: "dependency1"},
					Kind: registry.KindNamespaceDependency,
					Data: payload.New(map[string]interface{}{
						"component":  "wippy/llm",
						"version":    ">=v0.0.7",
						"parameters": nil, // nil parameters field
					}),
				},
			},
			requirementName: "PARAM1",
			shouldFind:      false,
			description:     "Dependency with nil parameters field",
		},
		{
			name: "dependency_with_malformed_parameters_field",
			nsDependencies: map[string]registry.Entry{
				"dependency1": {
					ID:   registry.ID{NS: "app.local", Name: "dependency1"},
					Kind: registry.KindNamespaceDependency,
					Data: payload.New(map[string]interface{}{
						"component":  "wippy/llm",
						"version":    ">=v0.0.7",
						"parameters": "not_an_array", // malformed parameters field
					}),
				},
			},
			requirementName: "PARAM1",
			shouldFind:      false,
			description:     "Dependency with malformed parameters field",
		},
		{
			name: "dependency_with_empty_parameters_array",
			nsDependencies: map[string]registry.Entry{
				"dependency1": {
					ID:   registry.ID{NS: "app.local", Name: "dependency1"},
					Kind: registry.KindNamespaceDependency,
					Data: payload.New(map[string]interface{}{
						"component":  "wippy/llm",
						"version":    ">=v0.0.7",
						"parameters": []interface{}{}, // empty parameters array
					}),
				},
			},
			requirementName: "PARAM1",
			shouldFind:      false,
			description:     "Dependency with empty parameters array",
		},
		{
			name: "dependency_with_valid_parameters",
			nsDependencies: map[string]registry.Entry{
				"dependency1": {
					ID:   registry.ID{NS: "app.local", Name: "dependency1"},
					Kind: registry.KindNamespaceDependency,
					Data: payload.New(map[string]interface{}{
						"component": "wippy/llm",
						"version":   ">=v0.0.7",
						"parameters": []interface{}{
							map[string]interface{}{
								"name":  "PARAM1",
								"value": "value1",
							},
						},
					}),
				},
			},
			requirementName: "PARAM1",
			shouldFind:      true,
			description:     "Dependency with valid parameters",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			dependency, paramValue, err := findDependencyByParameterName(tc.requirementName, tc.nsDependencies)

			if tc.shouldFind {
				require.NoError(t, err, "Should find dependency parameter")
				assert.Equal(t, "dependency1", dependency.ID.Name)
				assert.Equal(t, "value1", paramValue)
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

// TestRequirementDefaultValues tests the new default value functionality
func TestRequirementDefaultValues(t *testing.T) {
	logger := zap.NewNop()
	resolver := NewResolver(logger)

	// Test case 1: Requirement with default value, no dependency parameter provided
	t.Run("Requirement with default value, no dependency", func(t *testing.T) {
		entries := []registry.Entry{
			// Target entry that will receive the default value
			{
				ID:   registry.ID{NS: "app.local", Name: "target_function"},
				Kind: "function.lua",
				Data: payload.New(map[string]interface{}{
					"meta": map[string]interface{}{
						"host": "old_value",
					},
				}),
			},
			// Requirement with default value
			{
				ID:   registry.ID{NS: "app.local", Name: "application_host"},
				Kind: registry.KindNamespaceRequirement,
				Data: payload.New(map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"entry": "target_function",
							"path":  ".meta.host",
						},
					},
					"default": "app:processes",
				}),
			},
		}

		resolvedEntries, err := resolver.ResolveModuleDefinitions(entries)
		require.NoError(t, err, "Should resolve requirements without error")
		assert.Equal(t, len(entries), len(resolvedEntries), "Should return same number of entries")

		// Verify that the default value was applied
		targetEntry := resolvedEntries[0]
		data := targetEntry.Data.Data()
		if meta, ok := data.(map[string]interface{})["meta"].(map[string]interface{}); ok {
			assert.Equal(t, "app:processes", meta["host"], "Default value should be applied")
		}
	})

	// Test case 2: Requirement with default value, but dependency parameter is provided (should use dependency value)
	t.Run("Requirement with default value, dependency parameter provided", func(t *testing.T) {
		entries := []registry.Entry{
			// Target entry that will receive the value
			{
				ID:   registry.ID{NS: "app.local", Name: "target_function"},
				Kind: "function.lua",
				Data: payload.New(map[string]interface{}{
					"meta": map[string]interface{}{
						"host": "old_value",
					},
				}),
			},
			// Dependency that provides the parameter value
			{
				ID:   registry.ID{NS: "app.local", Name: "host_dependency"},
				Kind: registry.KindNamespaceDependency,
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "application_host",
							"value": "system:processes",
						},
					},
				}),
			},
			// Requirement with default value (should be overridden by dependency)
			{
				ID:   registry.ID{NS: "app.local", Name: "application_host"},
				Kind: registry.KindNamespaceRequirement,
				Data: payload.New(map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"entry": "target_function",
							"path":  ".meta.host",
						},
					},
					"default": "app:processes",
				}),
			},
		}

		resolvedEntries, err := resolver.ResolveModuleDefinitions(entries)
		require.NoError(t, err, "Should resolve requirements without error")
		assert.Equal(t, len(entries), len(resolvedEntries), "Should return same number of entries")

		// Verify that the dependency value was used (not the default)
		targetEntry := resolvedEntries[0]
		data := targetEntry.Data.Data()
		if meta, ok := data.(map[string]interface{})["meta"].(map[string]interface{}); ok {
			assert.Equal(t, "system:processes", meta["host"], "Dependency value should override default")
		}
	})

	// Test case 3: Requirement without default value and no dependency parameter (should fail gracefully)
	t.Run("Requirement without default value, no dependency", func(t *testing.T) {
		entries := []registry.Entry{
			// Target entry
			{
				ID:   registry.ID{NS: "app.local", Name: "target_function"},
				Kind: "function.lua",
				Data: payload.New(map[string]interface{}{
					"meta": map[string]interface{}{
						"host": "old_value",
					},
				}),
			},
			// Requirement without default value
			{
				ID:   registry.ID{NS: "app.local", Name: "application_host"},
				Kind: registry.KindNamespaceRequirement,
				Data: payload.New(map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"entry": "target_function",
							"path":  ".meta.host",
						},
					},
				}),
			},
		}

		resolvedEntries, err := resolver.ResolveModuleDefinitions(entries)
		require.NoError(t, err, "Should resolve requirements without error")
		assert.Equal(t, len(entries), len(resolvedEntries), "Should return same number of entries")

		// Verify that the original value was not changed (no injection occurred)
		targetEntry := resolvedEntries[0]
		data := targetEntry.Data.Data()
		if meta, ok := data.(map[string]interface{})["meta"].(map[string]interface{}); ok {
			assert.Equal(t, "old_value", meta["host"], "Original value should remain unchanged")
		}
	})

	// Test case 4: Requirement with default value, dependency has no parameters field
	t.Run("Requirement with default value, dependency has no parameters field", func(t *testing.T) {
		entries := []registry.Entry{
			// Target entry that will receive the default value
			{
				ID:   registry.ID{NS: "app.local", Name: "target_function"},
				Kind: "function.lua",
				Data: payload.New(map[string]interface{}{
					"meta": map[string]interface{}{
						"host": "old_value",
					},
				}),
			},
			// Dependency without parameters field
			{
				ID:   registry.ID{NS: "app.local", Name: "host_dependency"},
				Kind: registry.KindNamespaceDependency,
				Data: payload.New(map[string]interface{}{
					"component": "wippy/llm",
					"version":   ">=v0.0.7",
					// No parameters field
				}),
			},
			// Requirement with default value
			{
				ID:   registry.ID{NS: "app.local", Name: "application_host"},
				Kind: registry.KindNamespaceRequirement,
				Data: payload.New(map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"entry": "target_function",
							"path":  ".meta.host",
						},
					},
					"default": "app:processes",
				}),
			},
		}

		resolvedEntries, err := resolver.ResolveModuleDefinitions(entries)
		require.NoError(t, err, "Should resolve requirements without error")
		assert.Equal(t, len(entries), len(resolvedEntries), "Should return same number of entries")

		// Verify that the default value was applied
		targetEntry := resolvedEntries[0]
		data := targetEntry.Data.Data()
		if meta, ok := data.(map[string]interface{})["meta"].(map[string]interface{}); ok {
			assert.Equal(t, "app:processes", meta["host"], "Default value should be applied when parameters field is missing")
		}
	})

	// Test case 5: Requirement with default value, dependency has nil parameters field
	t.Run("Requirement with default value, dependency has nil parameters field", func(t *testing.T) {
		entries := []registry.Entry{
			// Target entry that will receive the default value
			{
				ID:   registry.ID{NS: "app.local", Name: "target_function"},
				Kind: "function.lua",
				Data: payload.New(map[string]interface{}{
					"meta": map[string]interface{}{
						"host": "old_value",
					},
				}),
			},
			// Dependency with nil parameters field
			{
				ID:   registry.ID{NS: "app.local", Name: "host_dependency"},
				Kind: registry.KindNamespaceDependency,
				Data: payload.New(map[string]interface{}{
					"component":  "wippy/llm",
					"version":    ">=v0.0.7",
					"parameters": nil, // nil parameters field
				}),
			},
			// Requirement with default value
			{
				ID:   registry.ID{NS: "app.local", Name: "application_host"},
				Kind: registry.KindNamespaceRequirement,
				Data: payload.New(map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"entry": "target_function",
							"path":  ".meta.host",
						},
					},
					"default": "app:processes",
				}),
			},
		}

		resolvedEntries, err := resolver.ResolveModuleDefinitions(entries)
		require.NoError(t, err, "Should resolve requirements without error")
		assert.Equal(t, len(entries), len(resolvedEntries), "Should return same number of entries")

		// Verify that the default value was applied
		targetEntry := resolvedEntries[0]
		data := targetEntry.Data.Data()
		if meta, ok := data.(map[string]interface{})["meta"].(map[string]interface{}); ok {
			assert.Equal(t, "app:processes", meta["host"], "Default value should be applied when parameters field is nil")
		}
	})

	// Test case 6: Requirement with default value, dependency has malformed parameters field
	t.Run("Requirement with default value, dependency has malformed parameters field", func(t *testing.T) {
		entries := []registry.Entry{
			// Target entry that will receive the default value
			{
				ID:   registry.ID{NS: "app.local", Name: "target_function"},
				Kind: "function.lua",
				Data: payload.New(map[string]interface{}{
					"meta": map[string]interface{}{
						"host": "old_value",
					},
				}),
			},
			// Dependency with malformed parameters field (not an array)
			{
				ID:   registry.ID{NS: "app.local", Name: "host_dependency"},
				Kind: registry.KindNamespaceDependency,
				Data: payload.New(map[string]interface{}{
					"component":  "wippy/llm",
					"version":    ">=v0.0.7",
					"parameters": "not_an_array", // malformed parameters field
				}),
			},
			// Requirement with default value
			{
				ID:   registry.ID{NS: "app.local", Name: "application_host"},
				Kind: registry.KindNamespaceRequirement,
				Data: payload.New(map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"entry": "target_function",
							"path":  ".meta.host",
						},
					},
					"default": "app:processes",
				}),
			},
		}

		resolvedEntries, err := resolver.ResolveModuleDefinitions(entries)
		require.NoError(t, err, "Should resolve requirements without error")
		assert.Equal(t, len(entries), len(resolvedEntries), "Should return same number of entries")

		// Verify that the default value was applied
		targetEntry := resolvedEntries[0]
		data := targetEntry.Data.Data()
		if meta, ok := data.(map[string]interface{})["meta"].(map[string]interface{}); ok {
			assert.Equal(t, "app:processes", meta["host"], "Default value should be applied when parameters field is malformed")
		}
	})
}

// TestGetRequirementDefaultValue tests the getRequirementDefaultValue function
func TestGetRequirementDefaultValue(t *testing.T) {
	testCases := []struct {
		name          string
		requirement   registry.Entry
		expectedValue string
		expectedFound bool
		description   string
	}{
		{
			name: "requirement_with_default",
			requirement: registry.Entry{
				ID:   registry.ID{NS: "app.local", Name: "application_host"},
				Kind: registry.KindNamespaceRequirement,
				Data: payload.New(map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"entry": "target_function",
							"path":  ".meta.host",
						},
					},
					"default": "app:processes",
				}),
			},
			expectedValue: "app:processes",
			expectedFound: true,
			description:   "Requirement with default value",
		},
		{
			name: "requirement_without_default",
			requirement: registry.Entry{
				ID:   registry.ID{NS: "app.local", Name: "application_host"},
				Kind: registry.KindNamespaceRequirement,
				Data: payload.New(map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"entry": "target_function",
							"path":  ".meta.host",
						},
					},
				}),
			},
			expectedValue: "",
			expectedFound: false,
			description:   "Requirement without default value",
		},
		{
			name: "requirement_with_empty_default",
			requirement: registry.Entry{
				ID:   registry.ID{NS: "app.local", Name: "application_host"},
				Kind: registry.KindNamespaceRequirement,
				Data: payload.New(map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"entry": "target_function",
							"path":  ".meta.host",
						},
					},
					"default": "",
				}),
			},
			expectedValue: "",
			expectedFound: true,
			description:   "Requirement with empty default value",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			value, found := getRequirementDefaultValue(tc.requirement)
			assert.Equal(t, tc.expectedValue, value, "Default value should match")
			assert.Equal(t, tc.expectedFound, found, "Found flag should match")
		})
	}
}

// TestValidateParameterMatching tests the validateParameterMatching function
func TestValidateParameterMatching(t *testing.T) {
	logger := zap.NewNop()
	resolver := NewResolver(logger)

	// Test case 1: All parameters have corresponding requirements
	t.Run("All parameters have requirements", func(t *testing.T) {
		nsDependencies := map[string]registry.Entry{
			"dependency1": {
				ID:   registry.ID{NS: "app.local", Name: "dependency1"},
				Kind: registry.KindNamespaceDependency,
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "PARAM1",
							"value": "value1",
						},
						map[string]interface{}{
							"name":  "PARAM2",
							"value": "value2",
						},
					},
				}),
			},
		}

		nsRequirements := map[string]registry.Entry{
			"requirement1": {
				ID:   registry.ID{NS: "app.local", Name: "PARAM1"},
				Kind: registry.KindNamespaceRequirement,
			},
			"requirement2": {
				ID:   registry.ID{NS: "app.local", Name: "PARAM2"},
				Kind: registry.KindNamespaceRequirement,
			},
		}

		// This should not panic or error
		resolver.validateParameterMatching(nsDependencies, nsRequirements)
	})

	// Test case 2: Some parameters don't have corresponding requirements
	t.Run("Some parameters missing requirements", func(t *testing.T) {
		nsDependencies := map[string]registry.Entry{
			"dependency1": {
				ID:   registry.ID{NS: "app.local", Name: "dependency1"},
				Kind: registry.KindNamespaceDependency,
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "PARAM1",
							"value": "value1",
						},
						map[string]interface{}{
							"name":  "PARAM2",
							"value": "value2",
						},
						map[string]interface{}{
							"name":  "PARAM3",
							"value": "value3",
						},
					},
				}),
			},
		}

		nsRequirements := map[string]registry.Entry{
			"requirement1": {
				ID:   registry.ID{NS: "app.local", Name: "PARAM1"},
				Kind: registry.KindNamespaceRequirement,
			},
			// PARAM2 and PARAM3 are missing requirements
		}

		// This should not panic or error, but should log warnings
		resolver.validateParameterMatching(nsDependencies, nsRequirements)
	})
}
