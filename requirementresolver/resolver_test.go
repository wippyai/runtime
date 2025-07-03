package requirementresolver

import (
	"testing"

	"github.com/itchyny/gojq"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindDependencyRequirements(t *testing.T) {
	// Test using the exact data structure from the comments in findDependencyRequirements function

	// nsDependency from the comment:
	// ID: app.requirements.demo:hello_world_dependency
	// Kind: ns.dependency
	// Meta: (registry.Metadata) (len=3) with description, comment, depends_on
	// Data: (payload.payload) with data: (map[string]interface {}) (len=7)
	nsDependency := registry.Entry{
		ID: registry.ID{
			NS:   "app.requirements.demo",
			Name: "hello_world_dependency",
		},
		Kind: registry.KindNamespaceDependency,
		Meta: registry.Metadata{
			"description": "Component dependency management demo example",
			"comment":     "Requirements and Dependencies Demo Application",
			"depends_on":  []interface{}{"ns:system"},
		},
		Data: payload.New(map[string]interface{}{
			"component": "igor-test-3/test-2",
			"kind":      "ns.dependency",
			"meta": map[string]interface{}{
				"description": "Component dependency management demo example",
			},
			"name":      "hello_world_dependency",
			"namespace": "app.requirements.demo",
			"parameters": []interface{}{
				map[string]interface{}{
					"name":  "api_router",
					"value": "system:api",
				},
				map[string]interface{}{
					"name":  "text",
					"value": "Updated Text",
				},
			},
			"version": ">=v0.0.1",
		}),
	}

	// nsRequirements from the comment:
	// Three requirements: NAMESPACE, API_ROUTER, TEXT
	// Each has targets with entry and path fields
	// Meta: (registry.Metadata) (len=2) with comment, depends_on
	// Data: (payload.payload) with data: (map[string]interface {}) (len=3)
	nsRequirements := map[string]registry.Entry{
		"NAMESPACE": {
			ID: registry.ID{
				NS:   "app.requirements.demo",
				Name: "NAMESPACE",
			},
			Kind: registry.KindNamespaceRequirement,
			Meta: registry.Metadata{
				"comment":    "Requirements and Dependencies Demo Application",
				"depends_on": []interface{}{"ns:system"},
			},
			Data: payload.New(map[string]interface{}{
				"kind": "ns.requirement",
				"name": "NAMESPACE",
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "hello_world_dependency",
						"path":  "namespace",
					},
				},
			}),
		},
		"API_ROUTER": {
			ID: registry.ID{
				NS:   "app.requirements.demo",
				Name: "API_ROUTER",
			},
			Kind: registry.KindNamespaceRequirement,
			Meta: registry.Metadata{
				"depends_on": []interface{}{"ns:system"},
				"comment":    "Requirements and Dependencies Demo Application",
			},
			Data: payload.New(map[string]interface{}{
				"kind": "ns.requirement",
				"name": "API_ROUTER",
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "hello_world_dependency",
						"path":  "parameters[name=api_router].value",
					},
				},
			}),
		},
		"TEXT": {
			ID: registry.ID{
				NS:   "app.requirements.demo",
				Name: "TEXT",
			},
			Kind: registry.KindNamespaceRequirement,
			Meta: registry.Metadata{
				"comment":    "Requirements and Dependencies Demo Application",
				"depends_on": []interface{}{"ns:system"},
			},
			Data: payload.New(map[string]interface{}{
				"kind": "ns.requirement",
				"name": "TEXT",
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "hello_world_dependency",
						"path":  "parameters[name=text].value",
					},
				},
			}),
		},
	}

	// Call the function with the exact data from comments
	result := findDependencyRequirements(nsDependency, nsRequirements)

	// The function now handles the new requirement format with targets field
	// So it should find all three requirements that target hello_world_dependency
	assert.Len(t, result, 3, "Function should find all three requirements with raw map data format")

	// Verify all expected requirements are found
	expectedNames := []string{"NAMESPACE", "API_ROUTER", "TEXT"}
	foundNames := make([]string, 0, len(result))
	for _, req := range result {
		foundNames = append(foundNames, req.ID.Name)
	}

	for _, expectedName := range expectedNames {
		assert.Contains(t, foundNames, expectedName, "Expected requirement %s not found", expectedName)
	}

	// Test with proper requirement format to ensure compatibility
	nsRequirementsProper := map[string]registry.Entry{
		"NAMESPACE": {
			ID: registry.ID{
				NS:   "app.requirements.demo",
				Name: "NAMESPACE",
			},
			Kind: registry.KindNamespaceRequirement,
			Data: payload.New(map[string]interface{}{
				"kind": "ns.requirement",
				"name": "NAMESPACE",
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "hello_world_dependency",
						"path":  "namespace",
					},
				},
			}),
		},
		"API_ROUTER": {
			ID: registry.ID{
				NS:   "app.requirements.demo",
				Name: "API_ROUTER",
			},
			Kind: registry.KindNamespaceRequirement,
			Data: payload.New(map[string]interface{}{
				"kind": "ns.requirement",
				"name": "API_ROUTER",
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "hello_world_dependency",
						"path":  "parameters[name=api_router].value",
					},
				},
			}),
		},
		"TEXT": {
			ID: registry.ID{
				NS:   "app.requirements.demo",
				Name: "TEXT",
			},
			Kind: registry.KindNamespaceRequirement,
			Data: payload.New(map[string]interface{}{
				"kind": "ns.requirement",
				"name": "TEXT",
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "hello_world_dependency",
						"path":  "parameters[name=text].value",
					},
				},
			}),
		},
	}

	resultProper := findDependencyRequirements(nsDependency, nsRequirementsProper)
	assert.Len(t, resultProper, 3, "Should find all three requirements with proper format")

	// Verify all expected requirements are found with proper format too
	foundNamesProper := make([]string, 0, len(resultProper))
	for _, req := range resultProper {
		foundNamesProper = append(foundNamesProper, req.ID.Name)
	}

	for _, expectedName := range expectedNames {
		assert.Contains(t, foundNamesProper, expectedName, "Expected requirement %s not found with proper format", expectedName)
	}
}

func TestFindRequirementDependency(t *testing.T) {
	// Test using the exact data structure from the comments in findRequirementDependency function

	// nsDependencies from the comment:
	// ID: app.requirements.demo:hello_world_dependency
	// Kind: ns.dependency
	// Meta: (registry.Metadata) (len=3) with description, comment, depends_on
	// Data: (payload.payload) with data: (map[string]interface {}) (len=7)
	nsDependencies := map[string]registry.Entry{
		"hello_world_dependency": {
			ID: registry.ID{
				NS:   "app.requirements.demo",
				Name: "hello_world_dependency",
			},
			Kind: registry.KindNamespaceDependency,
			Meta: registry.Metadata{
				"description": "Component dependency management demo example",
				"comment":     "Requirements and Dependencies Demo Application",
				"depends_on":  []interface{}{"ns:system"},
			},
			Data: payload.New(map[string]interface{}{
				"component": "igor-test-3/test-2",
				"kind":      "ns.dependency",
				"meta": map[string]interface{}{
					"description": "Component dependency management demo example",
				},
				"name":      "hello_world_dependency",
				"namespace": "app.requirements.demo",
				"parameters": []interface{}{
					map[string]interface{}{
						"name":  "api_router",
						"value": "system:api",
					},
					map[string]interface{}{
						"name":  "text",
						"value": "Updated Text",
					},
				},
				"version": ">=v0.0.1",
			}),
		},
	}

	// nsRequirements from the comment:
	// Three requirements: NAMESPACE, API_ROUTER, TEXT
	// Each has targets with entry and path fields
	// Meta: (registry.Metadata) (len=2) with comment, depends_on
	// Data: (payload.payload) with data: (map[string]interface {}) (len=3)
	nsRequirements := map[string]registry.Entry{
		"NAMESPACE": {
			ID: registry.ID{
				NS:   "app.requirements.demo",
				Name: "NAMESPACE",
			},
			Kind: registry.KindNamespaceRequirement,
			Meta: registry.Metadata{
				"comment":    "Requirements and Dependencies Demo Application",
				"depends_on": []interface{}{"ns:system"},
			},
			Data: payload.New(map[string]interface{}{
				"kind": "ns.requirement",
				"name": "NAMESPACE",
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "hello_world_dependency",
						"path":  "namespace",
					},
				},
			}),
		},
		"API_ROUTER": {
			ID: registry.ID{
				NS:   "app.requirements.demo",
				Name: "API_ROUTER",
			},
			Kind: registry.KindNamespaceRequirement,
			Meta: registry.Metadata{
				"depends_on": []interface{}{"ns:system"},
				"comment":    "Requirements and Dependencies Demo Application",
			},
			Data: payload.New(map[string]interface{}{
				"kind": "ns.requirement",
				"name": "API_ROUTER",
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "hello_world_dependency",
						"path":  "parameters[name=api_router].value",
					},
				},
			}),
		},
		"TEXT": {
			ID: registry.ID{
				NS:   "app.requirements.demo",
				Name: "TEXT",
			},
			Kind: registry.KindNamespaceRequirement,
			Meta: registry.Metadata{
				"comment":    "Requirements and Dependencies Demo Application",
				"depends_on": []interface{}{"ns:system"},
			},
			Data: payload.New(map[string]interface{}{
				"kind": "ns.requirement",
				"name": "TEXT",
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "hello_world_dependency",
						"path":  "parameters[name=text].value",
					},
				},
			}),
		},
	}

	// Test each requirement to find its dependency
	testCases := []struct {
		name         string
		requirement  registry.Entry
		expectedPath string
		shouldFind   bool
	}{
		{
			name:         "NAMESPACE requirement should find hello_world_dependency",
			requirement:  nsRequirements["NAMESPACE"],
			expectedPath: "namespace",
			shouldFind:   true,
		},
		{
			name:         "API_ROUTER requirement should find hello_world_dependency",
			requirement:  nsRequirements["API_ROUTER"],
			expectedPath: "parameters[name=api_router].value",
			shouldFind:   true,
		},
		{
			name:         "TEXT requirement should find hello_world_dependency",
			requirement:  nsRequirements["TEXT"],
			expectedPath: "parameters[name=text].value",
			shouldFind:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dependency, path, err := findRequirementDependency(tc.requirement, nsDependencies)

			if tc.shouldFind {
				assert.NoError(t, err, "Should find dependency for requirement %s", tc.requirement.ID.Name)
				assert.Equal(t, "hello_world_dependency", dependency.ID.Name, "Should find correct dependency")
				assert.Equal(t, "app.requirements.demo", dependency.ID.NS, "Should have correct namespace")
				assert.Equal(t, tc.expectedPath, path, "Should return correct path")
			} else {
				assert.Error(t, err, "Should not find dependency for requirement %s", tc.requirement.ID.Name)
			}
		})
	}

	// Test with requirement that doesn't match any dependency
	nonMatchingRequirement := registry.Entry{
		ID: registry.ID{
			NS:   "app.requirements.demo",
			Name: "NON_MATCHING",
		},
		Kind: registry.KindNamespaceRequirement,
		Data: payload.New(map[string]interface{}{
			"kind": "ns.requirement",
			"name": "NON_MATCHING",
			"targets": []interface{}{
				map[string]interface{}{
					"entry": "non_existent_dependency",
					"path":  "some.path",
				},
			},
		}),
	}

	t.Run("non-matching requirement should return error", func(t *testing.T) {
		dependency, path, err := findRequirementDependency(nonMatchingRequirement, nsDependencies)

		assert.Error(t, err, "Should return error for non-matching requirement")
		assert.Equal(t, registry.Entry{}, dependency, "Should return empty dependency")
		assert.Equal(t, "", path, "Should return empty path")
		assert.Contains(t, err.Error(), "dependency for requirement NON_MATCHING not found")
	})

	// Test with requirement that has different namespace
	differentNamespaceRequirement := registry.Entry{
		ID: registry.ID{
			NS:   "different.namespace",
			Name: "DIFFERENT_NAMESPACE",
		},
		Kind: registry.KindNamespaceRequirement,
		Data: payload.New(map[string]interface{}{
			"kind": "ns.requirement",
			"name": "DIFFERENT_NAMESPACE",
			"targets": []interface{}{
				map[string]interface{}{
					"entry": "hello_world_dependency",
					"path":  "some.path",
				},
			},
		}),
	}

	t.Run("requirement with different namespace should return error", func(t *testing.T) {
		dependency, path, err := findRequirementDependency(differentNamespaceRequirement, nsDependencies)

		assert.Error(t, err, "Should return error for requirement with different namespace")
		assert.Equal(t, registry.Entry{}, dependency, "Should return empty dependency")
		assert.Equal(t, "", path, "Should return empty path")
		assert.Contains(t, err.Error(), "dependency for requirement DIFFERENT_NAMESPACE not found")
	})
}

func TestGetValueFromEntry(t *testing.T) {
	tests := []struct {
		name     string
		entry    registry.Entry
		path     string
		expected string
		wantErr  bool
		errMsg   string
	}{
		{
			name: "simple field access - namespace",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"namespace": "app.requirements.demo",
					"version":   ">=v0.0.1",
					"component": "igor-test-3/test-2",
				}),
			},
			path:     ".namespace",
			expected: "app.requirements.demo",
			wantErr:  false,
		},
		{
			name: "simple field access - version",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"namespace": "app.requirements.demo",
					"version":   ">=v0.0.1",
					"component": "igor-test-3/test-2",
				}),
			},
			path:     ".version",
			expected: ">=v0.0.1",
			wantErr:  false,
		},
		{
			name: "array filter with equality - parameters[name=text].value",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "api_router",
							"value": "system:api",
						},
						map[string]interface{}{
							"name":  "text",
							"value": "Updated Text",
						},
					},
				}),
			},
			path:     `.parameters[] | select(.name == "text") | .value`,
			expected: "Updated Text",
			wantErr:  false,
		},
		{
			name: "array filter with equality - parameters[name=api_router].value",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "api_router",
							"value": "system:api",
						},
						map[string]interface{}{
							"name":  "text",
							"value": "Updated Text",
						},
					},
				}),
			},
			path:     `.parameters[] | select(.name == "api_router") | .value`,
			expected: "system:api",
			wantErr:  false,
		},
		{
			name: "nested field access",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"meta": map[string]interface{}{
						"description": "Component dependency management demo example",
						"comment":     "Requirements and Dependencies Demo Application",
					},
				}),
			},
			path:     ".meta.description",
			expected: "Component dependency management demo example",
			wantErr:  false,
		},
		{
			name: "array filter with numeric values",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"items": []interface{}{
						map[string]interface{}{
							"id":    1,
							"name":  "first",
							"value": "alpha",
						},
						map[string]interface{}{
							"id":    2,
							"name":  "second",
							"value": "beta",
						},
					},
				}),
			},
			path:     `.items[] | select(.id == 2) | .value`,
			expected: "beta",
			wantErr:  false,
		},
		{
			name: "array filter with boolean values",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"flags": []interface{}{
						map[string]interface{}{
							"name":  "enabled",
							"value": true,
						},
						map[string]interface{}{
							"name":  "disabled",
							"value": false,
						},
					},
				}),
			},
			path:     `.flags[] | select(.name == "enabled") | .value`,
			expected: "true",
			wantErr:  false,
		},
		{
			name: "array filter with inequality operators",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"numbers": []interface{}{
						map[string]interface{}{
							"id":    1,
							"value": 10,
						},
						map[string]interface{}{
							"id":    2,
							"value": 20,
						},
						map[string]interface{}{
							"id":    3,
							"value": 30,
						},
					},
				}),
			},
			path:     `.numbers[] | select(.value > 15) | .id`,
			expected: "2",
			wantErr:  false,
		},
		{
			name: "array filter with not equals",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"items": []interface{}{
						map[string]interface{}{
							"name":  "excluded",
							"value": "skip",
						},
						map[string]interface{}{
							"name":  "included",
							"value": "keep",
						},
					},
				}),
			},
			path:     `.items[] | select(.name != "excluded") | .value`,
			expected: "keep",
			wantErr:  false,
		},
		{
			name: "complex nested path",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"config": map[string]interface{}{
						"services": []interface{}{
							map[string]interface{}{
								"name": "api",
								"settings": map[string]interface{}{
									"port":    8080,
									"timeout": "30s",
								},
							},
							map[string]interface{}{
								"name": "db",
								"settings": map[string]interface{}{
									"port":    5432,
									"timeout": "60s",
								},
							},
						},
					},
				}),
			},
			path:     `.config.services[] | select(.name == "api") | .settings.port`,
			expected: "8080",
			wantErr:  false,
		},
		{
			name: "empty path",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"value": "test",
				}),
			},
			path:    "",
			wantErr: true,
			errMsg:  "path cannot be empty",
		},
		{
			name: "nil entry data",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(nil),
			},
			path:    ".value",
			wantErr: true,
			errMsg:  "entry data is nil",
		},
		{
			name: "field not found",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"existing": "value",
				}),
			},
			path:    ".nonexistent",
			wantErr: true,
			errMsg:  "no results found",
		},
		{
			name: "array filter no matches",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "api_router",
							"value": "system:api",
						},
					},
				}),
			},
			path:    `.parameters[] | select(.name == "text") | .value`,
			wantErr: true,
			errMsg:  "no results found",
		},
		{
			name: "multiple array matches",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"items": []interface{}{
						map[string]interface{}{
							"type":  "service",
							"value": "first",
						},
						map[string]interface{}{
							"type":  "service",
							"value": "second",
						},
					},
				}),
			},
			path:     `.items[] | select(.type == "service") | .value`,
			expected: "first",
			wantErr:  false,
		},
		{
			name: "invalid path - unmatched bracket",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"value": "test",
				}),
			},
			path:    "value[invalid",
			wantErr: true,
			errMsg:  "failed to parse jq query",
		},
		{
			name: "access field on non-map",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New("string value"),
			},
			path:    ".field",
			wantErr: true,
			errMsg:  "jq query error",
		},
		{
			name: "apply filter on non-slice",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"value": "not an array",
				}),
			},
			path:    `.value[] | select(.name == "test")`,
			wantErr: true,
			errMsg:  "jq query error",
		},
		{
			name: "numeric comparison with strings",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"items": []interface{}{
						map[string]interface{}{
							"name":  "item1",
							"score": "100",
						},
						map[string]interface{}{
							"name":  "item2",
							"score": "200",
						},
					},
				}),
			},
			path:     `.items[] | select(.score > "150") | .name`,
			expected: "item2",
			wantErr:  false,
		},
		{
			name: "string comparison with numbers",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"items": []interface{}{
						map[string]interface{}{
							"name":  "alpha",
							"order": 1,
						},
						map[string]interface{}{
							"name":  "beta",
							"order": 2,
						},
					},
				}),
			},
			path:     `.items[] | select(.name > "alpha") | .name`,
			expected: "beta",
			wantErr:  false,
		},
		{
			name: "real-world data structure from comments",
			entry: registry.Entry{
				ID:   registry.ID{NS: "app.requirements.demo", Name: "hello_world_dependency"},
				Kind: "ns.dependency",
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "api_router",
							"value": "system:api",
						},
						map[string]interface{}{
							"name":  "text",
							"value": "Updated Text",
						},
					},
					"version":   ">=v0.0.1",
					"component": "igor-test-3/test-2",
					"kind":      "ns.dependency",
					"meta": map[string]interface{}{
						"description": "Component dependency management demo example",
					},
					"name":      "hello_world_dependency",
					"namespace": "app.requirements.demo",
				}),
			},
			path:     `.parameters[] | select(.name == "text") | .value`,
			expected: "Updated Text",
			wantErr:  false,
		},
		{
			name: "real-world data structure - namespace field",
			entry: registry.Entry{
				ID:   registry.ID{NS: "app.requirements.demo", Name: "hello_world_dependency"},
				Kind: "ns.dependency",
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "api_router",
							"value": "system:api",
						},
						map[string]interface{}{
							"name":  "text",
							"value": "Updated Text",
						},
					},
					"version":   ">=v0.0.1",
					"component": "igor-test-3/test-2",
					"kind":      "ns.dependency",
					"meta": map[string]interface{}{
						"description": "Component dependency management demo example",
					},
					"name":      "hello_world_dependency",
					"namespace": "app.requirements.demo",
				}),
			},
			path:     ".namespace",
			expected: "app.requirements.demo",
			wantErr:  false,
		},
		{
			name: "real-world data structure - api_router parameter",
			entry: registry.Entry{
				ID:   registry.ID{NS: "app.requirements.demo", Name: "hello_world_dependency"},
				Kind: "ns.dependency",
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "api_router",
							"value": "system:api",
						},
						map[string]interface{}{
							"name":  "text",
							"value": "Updated Text",
						},
					},
					"version":   ">=v0.0.1",
					"component": "igor-test-3/test-2",
					"kind":      "ns.dependency",
					"meta": map[string]interface{}{
						"description": "Component dependency management demo example",
					},
					"name":      "hello_world_dependency",
					"namespace": "app.requirements.demo",
				}),
			},
			path:     `.parameters[] | select(.name == "api_router") | .value`,
			expected: "system:api",
			wantErr:  false,
		},
		{
			name: "two nested slices",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"groups": []interface{}{
						map[string]interface{}{
							"name": "admins",
							"users": []interface{}{
								map[string]interface{}{
									"username": "alice",
									"email":    "alice@example.com",
								},
								map[string]interface{}{
									"username": "bob",
									"email":    "bob@example.com",
								},
							},
						},
						map[string]interface{}{
							"name": "guests",
							"users": []interface{}{
								map[string]interface{}{
									"username": "carol",
									"email":    "carol@example.com",
								},
							},
						},
					},
				}),
			},
			path:     `.groups[] | select(.name == "admins") | .users[] | select(.username == "alice") | .email`,
			expected: "alice@example.com",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getValueFromEntry(tt.entry, tt.path)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParsePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected []interface{}
	}{
		{
			name: "simple field",
			path: "namespace",
			expected: []interface{}{
				"namespace",
			},
		},
		{
			name: "nested fields",
			path: "meta.description",
			expected: []interface{}{
				"meta",
				"description",
			},
		},
		{
			name: "array filter",
			path: "parameters[name=text]",
			expected: []interface{}{
				"parameters",
				&arrayFilter{field: "name", operator: "=", value: "text"},
			},
		},
		{
			name: "array filter with field access",
			path: "parameters[name=text].value",
			expected: []interface{}{
				"parameters",
				&arrayFilter{field: "name", operator: "=", value: "text"},
				"value",
			},
		},
		{
			name: "complex nested path",
			path: "config.services[name=api].settings.port",
			expected: []interface{}{
				"config",
				"services",
				&arrayFilter{field: "name", operator: "=", value: "api"},
				"settings",
				"port",
			},
		},
		{
			name: "inequality operators",
			path: "items[score>100]",
			expected: []interface{}{
				"items",
				&arrayFilter{field: "score", operator: ">", value: "100"},
			},
		},
		{
			name: "not equals operator",
			path: "items[type!=excluded]",
			expected: []interface{}{
				"items",
				&arrayFilter{field: "type", operator: "!=", value: "excluded"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parsePath(tt.path)
			assert.Equal(t, len(tt.expected), len(result))

			for i, expected := range tt.expected {
				if i < len(result) {
					switch exp := expected.(type) {
					case string:
						assert.Equal(t, exp, result[i])
					case *arrayFilter:
						if act, ok := result[i].(*arrayFilter); ok {
							assert.Equal(t, exp.field, act.field)
							assert.Equal(t, exp.operator, act.operator)
							assert.Equal(t, exp.value, act.value)
						} else {
							t.Errorf("Expected arrayFilter at position %d, got %T", i, result[i])
						}
					}
				}
			}
		})
	}
}

func TestParseArrayFilter(t *testing.T) {
	tests := []struct {
		name     string
		filter   string
		expected *arrayFilter
	}{
		{
			name:   "equality",
			filter: "name=text",
			expected: &arrayFilter{
				field:    "name",
				operator: "=",
				value:    "text",
			},
		},
		{
			name:   "not equals",
			filter: "type!=excluded",
			expected: &arrayFilter{
				field:    "type",
				operator: "!=",
				value:    "excluded",
			},
		},
		{
			name:   "greater than",
			filter: "score>100",
			expected: &arrayFilter{
				field:    "score",
				operator: ">",
				value:    "100",
			},
		},
		{
			name:   "less than",
			filter: "count<10",
			expected: &arrayFilter{
				field:    "count",
				operator: "<",
				value:    "10",
			},
		},
		{
			name:   "greater than or equal",
			filter: "version>=1.0",
			expected: &arrayFilter{
				field:    "version",
				operator: ">=",
				value:    "1.0",
			},
		},
		{
			name:   "less than or equal",
			filter: "limit<=50",
			expected: &arrayFilter{
				field:    "limit",
				operator: "<=",
				value:    "50",
			},
		},
		{
			name:   "with spaces",
			filter: "name = text",
			expected: &arrayFilter{
				field:    "name",
				operator: "=",
				value:    "text",
			},
		},
		{
			name:   "no operator",
			filter: "field",
			expected: &arrayFilter{
				field:    "field",
				operator: "=",
				value:    "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseArrayFilter(tt.filter)
			assert.Equal(t, tt.expected.field, result.field)
			assert.Equal(t, tt.expected.operator, result.operator)
			assert.Equal(t, tt.expected.value, result.value)
		})
	}
}

func TestMatchesFilter(t *testing.T) {
	tests := []struct {
		name     string
		item     map[string]interface{}
		filter   *arrayFilter
		expected bool
	}{
		{
			name: "string equality match",
			item: map[string]interface{}{
				"name":  "test",
				"value": "result",
			},
			filter: &arrayFilter{
				field:    "name",
				operator: "=",
				value:    "test",
			},
			expected: true,
		},
		{
			name: "string equality no match",
			item: map[string]interface{}{
				"name":  "other",
				"value": "result",
			},
			filter: &arrayFilter{
				field:    "name",
				operator: "=",
				value:    "test",
			},
			expected: false,
		},
		{
			name: "integer comparison",
			item: map[string]interface{}{
				"score": 100,
				"name":  "item",
			},
			filter: &arrayFilter{
				field:    "score",
				operator: ">",
				value:    "50",
			},
			expected: true,
		},
		{
			name: "float comparison",
			item: map[string]interface{}{
				"price": 19.99,
				"name":  "product",
			},
			filter: &arrayFilter{
				field:    "price",
				operator: "<",
				value:    "20.0",
			},
			expected: true,
		},
		{
			name: "boolean comparison",
			item: map[string]interface{}{
				"enabled": true,
				"name":    "feature",
			},
			filter: &arrayFilter{
				field:    "enabled",
				operator: "=",
				value:    "true",
			},
			expected: true,
		},
		{
			name: "field not found",
			item: map[string]interface{}{
				"other": "value",
			},
			filter: &arrayFilter{
				field:    "missing",
				operator: "=",
				value:    "test",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesFilter(tt.item, tt.filter)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestApplyPathValueToEntries(t *testing.T) {
	tests := []struct {
		name       string
		targetPath string
		value      string
		entries    []registry.Entry
		expected   []registry.Entry
	}{
		{
			name:       "append to array with simple path",
			targetPath: "meta.depends_on[]",
			value:      "ns:system",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"depends_on": []interface{}{"existing:value"},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"depends_on": []interface{}{"existing:value", "ns:system"},
					},
				},
			},
		},
		{
			name:       "append to non-existent array",
			targetPath: "meta.depends_on[]",
			value:      "ns:system",
			entries: []registry.Entry{
				{
					ID:   registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"depends_on": []interface{}{"ns:system"},
					},
				},
			},
		},
		{
			name:       "set simple field value",
			targetPath: "meta.router",
			value:      "system:api",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"existing": "value",
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"existing": "value",
						"router":   "system:api",
					},
				},
			},
		},
		{
			name:       "set nested field value",
			targetPath: "meta.config.router",
			value:      "system:api",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"existing": "value",
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"existing": "value",
						"config": map[string]interface{}{
							"router": "system:api",
						},
					},
				},
			},
		},
		{
			name:       "append to array in nested path",
			targetPath: "meta.config.dependencies[]",
			value:      "new:dep",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"config": map[string]interface{}{
							"dependencies": []interface{}{"existing:dep"},
						},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"config": map[string]interface{}{
							"dependencies": []interface{}{"existing:dep", "new:dep"},
						},
					},
				},
			},
		},
		{
			name:       "multiple entries",
			targetPath: "meta.router",
			value:      "system:api",
			entries: []registry.Entry{
				{
					ID:   registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{},
				},
				{
					ID: registry.ID{NS: "test", Name: "entry2"},
					Meta: registry.Metadata{
						"existing": "value",
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"router": "system:api",
					},
				},
				{
					ID: registry.ID{NS: "test", Name: "entry2"},
					Meta: registry.Metadata{
						"existing": "value",
						"router":   "system:api",
					},
				},
			},
		},
		{
			name:       "entry with nil meta",
			targetPath: "meta.router",
			value:      "system:api",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"router": "system:api",
					},
				},
			},
		},
		{
			name:       "complex nested array append with filter",
			targetPath: "meta.services[name=service1].config.dependencies[]",
			value:      "new:service",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name": "service1",
								"config": map[string]interface{}{
									"dependencies": []interface{}{"existing:dep"},
								},
							},
						},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name": "service1",
								"config": map[string]interface{}{
									"dependencies": []interface{}{"existing:dep", "new:service"},
								},
							},
						},
					},
				},
			},
		},
		{
			name:       "set value in filtered array",
			targetPath: "meta.services[name=service1].config.router",
			value:      "system:api",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name": "service1",
								"config": map[string]interface{}{
									"existing": "value",
								},
							},
							map[string]interface{}{
								"name": "service2",
								"config": map[string]interface{}{
									"existing": "value2",
								},
							},
						},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name": "service1",
								"config": map[string]interface{}{
									"existing": "value",
									"router":   "system:api",
								},
							},
							map[string]interface{}{
								"name": "service2",
								"config": map[string]interface{}{
									"existing": "value2",
								},
							},
						},
					},
				},
			},
		},
		{
			name:       "append to array in filtered path",
			targetPath: "meta.services[name=service1].config.dependencies[]",
			value:      "new:dep",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name": "service1",
								"config": map[string]interface{}{
									"dependencies": []interface{}{"existing:dep"},
								},
							},
						},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name": "service1",
								"config": map[string]interface{}{
									"dependencies": []interface{}{"existing:dep", "new:dep"},
								},
							},
						},
					},
				},
			},
		},
		{
			name:       "empty entries slice",
			targetPath: "meta.router",
			value:      "system:api",
			entries:    []registry.Entry{},
			expected:   []registry.Entry{},
		},
		{
			name:       "root level field",
			targetPath: "kind",
			value:      "new_kind",
			entries: []registry.Entry{
				{
					ID:   registry.ID{NS: "test", Name: "entry1"},
					Kind: "old_kind",
				},
			},
			expected: []registry.Entry{
				{
					ID:   registry.ID{NS: "test", Name: "entry1"},
					Kind: "new_kind",
				},
			},
		},
		{
			name:       "root level array append",
			targetPath: "meta.tags[]",
			value:      "new_tag",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"tags": []interface{}{"existing_tag"},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"tags": []interface{}{"existing_tag", "new_tag"},
					},
				},
			},
		},
		{
			name:       "deeply nested path creation",
			targetPath: "level1.level2.level3.level4.value",
			value:      "deep_value",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"existing": "value",
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"existing": "value",
						"level1": map[string]interface{}{
							"level2": map[string]interface{}{
								"level3": map[string]interface{}{
									"level4": map[string]interface{}{
										"value": "deep_value",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:       "multiple array filters",
			targetPath: "meta.services[name=service1].endpoints[port=8080].config.router",
			value:      "system:api",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name": "service1",
								"endpoints": []interface{}{
									map[string]interface{}{
										"port": 8080,
										"config": map[string]interface{}{
											"existing": "value",
										},
									},
								},
							},
						},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name": "service1",
								"endpoints": []interface{}{
									map[string]interface{}{
										"port": 8080,
										"config": map[string]interface{}{
											"existing": "value",
											"router":   "system:api",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of entries to avoid modifying the original
			entriesCopy := make([]registry.Entry, len(tt.entries))
			copy(entriesCopy, tt.entries)

			err := applyPathValueToEntries(tt.targetPath, tt.value, entriesCopy)

			assert.NoError(t, err)
			assert.Len(t, entriesCopy, len(tt.expected))

			for i, expected := range tt.expected {
				actual := entriesCopy[i]
				assert.Equal(t, expected.ID, actual.ID, "Entry %d ID mismatch", i)
				assert.Equal(t, expected.Kind, actual.Kind, "Entry %d Kind mismatch", i)
				assert.Equal(t, expected.Meta, actual.Meta, "Entry %d Meta mismatch", i)
			}
		})
	}
}

func TestApplyPathValueToEntriesErrorCases(t *testing.T) {
	tests := []struct {
		name       string
		targetPath string
		value      string
		entries    []registry.Entry
	}{
		{
			name:       "invalid path with unmatched brackets",
			targetPath: "meta[depends_on",
			value:      "value",
			entries: []registry.Entry{
				{
					ID:   registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{},
				},
			},
		},
		{
			name:       "invalid path with unmatched closing bracket",
			targetPath: "meta]depends_on",
			value:      "value",
			entries: []registry.Entry{
				{
					ID:   registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{},
				},
			},
		},
		{
			name:       "array filter with no matching element",
			targetPath: "meta.services[name=nonexistent].config.router",
			value:      "system:api",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name": "service1",
								"config": map[string]interface{}{
									"router": "old:api",
								},
							},
						},
					},
				},
			},
		},
		{
			name:       "trying to append to non-array field",
			targetPath: "meta.router[]",
			value:      "system:api",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"router": "existing:api",
					},
				},
			},
		},
		{
			name:       "navigating through non-map type",
			targetPath: "meta.router.subfield",
			value:      "value",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"router": "string_value", // Not a map
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of entries to avoid modifying the original
			entriesCopy := make([]registry.Entry, len(tt.entries))
			copy(entriesCopy, tt.entries)

			// The function should handle errors gracefully and continue processing
			// It logs warnings but doesn't return errors
			err := applyPathValueToEntries(tt.targetPath, tt.value, entriesCopy)
			assert.Error(t, err, "applyPathValueToEntries should return an error for invalid input")

			// Verify that entries are not modified when errors occur
			for i, originalEntry := range tt.entries {
				assert.Equal(t, originalEntry.ID, entriesCopy[i].ID, "Entry ID should not be modified when path is invalid")
				assert.Equal(t, originalEntry.Kind, entriesCopy[i].Kind, "Entry Kind should not be modified when path is invalid")
				assert.Equal(t, originalEntry.Meta, entriesCopy[i].Meta, "Entry Meta should not be modified when path is invalid")
			}
		})
	}
}

func TestApplyPathValueToEntriesEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		targetPath string
		value      string
		entries    []registry.Entry
		expected   []registry.Entry
	}{
		{
			name:       "empty value",
			targetPath: "meta.router",
			value:      "",
			entries: []registry.Entry{
				{
					ID:   registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"router": "",
					},
				},
			},
		},
		{
			name:       "empty array append",
			targetPath: "meta.depends_on[]",
			value:      "",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"depends_on": []interface{}{"existing"},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"depends_on": []interface{}{"existing", ""},
					},
				},
			},
		},
		{
			name:       "mixed data types in array",
			targetPath: "meta.values[]",
			value:      "string_value",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"values": []interface{}{1, "existing", true},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"values": []interface{}{1, "existing", true, "string_value"},
					},
				},
			},
		},
		{
			name:       "deeply nested path creation",
			targetPath: "level1.level2.level3.level4.value",
			value:      "deep_value",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"existing": "value",
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"existing": "value",
						"level1": map[string]interface{}{
							"level2": map[string]interface{}{
								"level3": map[string]interface{}{
									"level4": map[string]interface{}{
										"value": "deep_value",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:       "multiple array filters",
			targetPath: "meta.services[name=service1].endpoints[port=8080].config.router",
			value:      "system:api",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name": "service1",
								"endpoints": []interface{}{
									map[string]interface{}{
										"port": 8080,
										"config": map[string]interface{}{
											"existing": "value",
										},
									},
								},
							},
						},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name": "service1",
								"endpoints": []interface{}{
									map[string]interface{}{
										"port": 8080,
										"config": map[string]interface{}{
											"existing": "value",
											"router":   "system:api",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of entries to avoid modifying the original
			entriesCopy := make([]registry.Entry, len(tt.entries))
			copy(entriesCopy, tt.entries)

			err := applyPathValueToEntries(tt.targetPath, tt.value, entriesCopy)
			assert.NoError(t, err)

			assert.Len(t, entriesCopy, len(tt.expected))

			for i, expected := range tt.expected {
				actual := entriesCopy[i]
				assert.Equal(t, expected.ID, actual.ID, "Entry %d ID mismatch", i)
				assert.Equal(t, expected.Kind, actual.Kind, "Entry %d Kind mismatch", i)
				assert.Equal(t, expected.Meta, actual.Meta, "Entry %d Meta mismatch", i)
			}
		})
	}
}

func TestGetValueFromEntryWithGojq(t *testing.T) {
	tests := []struct {
		name     string
		entry    registry.Entry
		path     string
		expected string
		wantErr  bool
		errMsg   string
	}{
		{
			name: "simple field access - namespace",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"namespace": "app.requirements.demo",
					"version":   ">=v0.0.1",
					"component": "igor-test-3/test-2",
				}),
			},
			path:     ".namespace",
			expected: "app.requirements.demo",
			wantErr:  false,
		},
		{
			name: "simple field access - version",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"namespace": "app.requirements.demo",
					"version":   ">=v0.0.1",
					"component": "igor-test-3/test-2",
				}),
			},
			path:     ".version",
			expected: ">=v0.0.1",
			wantErr:  false,
		},
		{
			name: "array filter with equality - parameters[name=text].value",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "api_router",
							"value": "system:api",
						},
						map[string]interface{}{
							"name":  "text",
							"value": "Updated Text",
						},
					},
				}),
			},
			path:     `.parameters[] | select(.name == "text") | .value`,
			expected: "Updated Text",
			wantErr:  false,
		},
		{
			name: "array filter with equality - parameters[name=api_router].value",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "api_router",
							"value": "system:api",
						},
						map[string]interface{}{
							"name":  "text",
							"value": "Updated Text",
						},
					},
				}),
			},
			path:     `.parameters[] | select(.name == "api_router") | .value`,
			expected: "system:api",
			wantErr:  false,
		},
		{
			name: "nested field access",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"meta": map[string]interface{}{
						"description": "Component dependency management demo example",
						"comment":     "Requirements and Dependencies Demo Application",
					},
				}),
			},
			path:     ".meta.description",
			expected: "Component dependency management demo example",
			wantErr:  false,
		},
		{
			name: "complex nested path with array filter",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"config": map[string]interface{}{
						"services": []interface{}{
							map[string]interface{}{
								"name": "api",
								"settings": map[string]interface{}{
									"port":    8080,
									"timeout": "30s",
								},
							},
							map[string]interface{}{
								"name": "db",
								"settings": map[string]interface{}{
									"port":    5432,
									"timeout": "60s",
								},
							},
						},
					},
				}),
			},
			path:     `.config.services[] | select(.name == "api") | .settings.port`,
			expected: "8080",
			wantErr:  false,
		},
		{
			name: "real-world data structure from comments",
			entry: registry.Entry{
				ID:   registry.ID{NS: "app.requirements.demo", Name: "hello_world_dependency"},
				Kind: "ns.dependency",
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "api_router",
							"value": "system:api",
						},
						map[string]interface{}{
							"name":  "text",
							"value": "Updated Text",
						},
					},
					"version":   ">=v0.0.1",
					"component": "igor-test-3/test-2",
					"kind":      "ns.dependency",
					"meta": map[string]interface{}{
						"description": "Component dependency management demo example",
					},
					"name":      "hello_world_dependency",
					"namespace": "app.requirements.demo",
				}),
			},
			path:     `.parameters[] | select(.name == "text") | .value`,
			expected: "Updated Text",
			wantErr:  false,
		},
		{
			name: "real-world data structure - namespace field",
			entry: registry.Entry{
				ID:   registry.ID{NS: "app.requirements.demo", Name: "hello_world_dependency"},
				Kind: "ns.dependency",
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "api_router",
							"value": "system:api",
						},
						map[string]interface{}{
							"name":  "text",
							"value": "Updated Text",
						},
					},
					"version":   ">=v0.0.1",
					"component": "igor-test-3/test-2",
					"kind":      "ns.dependency",
					"meta": map[string]interface{}{
						"description": "Component dependency management demo example",
					},
					"name":      "hello_world_dependency",
					"namespace": "app.requirements.demo",
				}),
			},
			path:     ".namespace",
			expected: "app.requirements.demo",
			wantErr:  false,
		},
		{
			name: "real-world data structure - api_router parameter",
			entry: registry.Entry{
				ID:   registry.ID{NS: "app.requirements.demo", Name: "hello_world_dependency"},
				Kind: "ns.dependency",
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "api_router",
							"value": "system:api",
						},
						map[string]interface{}{
							"name":  "text",
							"value": "Updated Text",
						},
					},
					"version":   ">=v0.0.1",
					"component": "igor-test-3/test-2",
					"kind":      "ns.dependency",
					"meta": map[string]interface{}{
						"description": "Component dependency management demo example",
					},
					"name":      "hello_world_dependency",
					"namespace": "app.requirements.demo",
				}),
			},
			path:     `.parameters[] | select(.name == "api_router") | .value`,
			expected: "system:api",
			wantErr:  false,
		},
		{
			name: "invalid jq query",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"value": "test",
				}),
			},
			path:    "invalid[query",
			wantErr: true,
			errMsg:  "failed to parse jq query",
		},
		{
			name: "field not found",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"existing": "value",
				}),
			},
			path:    ".nonexistent",
			wantErr: true,
			errMsg:  "no results found",
		},
		{
			name: "array filter no matches",
			entry: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "test"},
				Kind: "test",
				Data: payload.New(map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "api_router",
							"value": "system:api",
						},
					},
				}),
			},
			path:    `.parameters[] | select(.name == "text") | .value`,
			wantErr: true,
			errMsg:  "no results found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getValueFromEntry(tt.entry, tt.path)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestGojqDebug(t *testing.T) {
	data := map[string]interface{}{
		"existing": "value",
	}

	query, err := gojq.Parse(".nonexistent")
	require.NoError(t, err)

	iter := query.Run(data)
	v, ok := iter.Next()

	t.Logf("ok: %v, v: %v, type: %T", ok, v, v)

	// Check if there are more results
	v2, ok2 := iter.Next()
	t.Logf("ok2: %v, v2: %v, type: %T", ok2, v2, v2)
}
