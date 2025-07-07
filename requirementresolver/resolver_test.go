package requirementresolver

import (
	"fmt"
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
			Kind: registry.KindNamespaceDefinition,
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
			Kind: registry.KindNamespaceDefinition,
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
			Kind: registry.KindNamespaceDefinition,
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
		Kind: registry.KindNamespaceDefinition,
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
		Kind: registry.KindNamespaceDefinition,
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

func TestApplyPathValueToEntriesWithGojq(t *testing.T) {
	tests := []struct {
		name     string
		jqQuery  string
		value    string
		entries  []registry.Entry
		expected []registry.Entry
		wantErr  bool
		errMsg   string
	}{
		{
			name:    "set simple field",
			jqQuery: `.meta.router =`,
			value:   "system:api",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test.ns", Name: "entry1"},
					Meta: registry.Metadata{
						"existing": "value",
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test.ns", Name: "entry1"},
					Meta: registry.Metadata{
						"existing": "value",
						"router":   "system:api",
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "append to array",
			jqQuery: `.meta.tags +=`,
			value:   "jq",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test.ns", Name: "entry1"},
					Meta: registry.Metadata{
						"tags": []interface{}{"go", "test"},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test.ns", Name: "entry1"},
					Meta: registry.Metadata{
						"tags": []interface{}{"go", "test", "jq"},
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "set nested field",
			jqQuery: `.meta.config.database.host =`,
			value:   "localhost",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test.ns", Name: "entry1"},
					Meta: registry.Metadata{
						"config": map[string]interface{}{
							"database": map[string]interface{}{
								"port": float64(5432),
							},
						},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test.ns", Name: "entry1"},
					Meta: registry.Metadata{
						"config": map[string]interface{}{
							"database": map[string]interface{}{
								"port": float64(5432),
								"host": "localhost",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "set kind field",
			jqQuery: "kind",
			value:   "new.kind",
			entries: []registry.Entry{
				{
					ID:   registry.ID{NS: "test.ns", Name: "entry1"},
					Kind: "old.kind",
				},
			},
			expected: []registry.Entry{
				{
					ID:   registry.ID{NS: "test.ns", Name: "entry1"},
					Kind: "new.kind",
				},
			},
			wantErr: false,
		},
		{
			name:    "multiple entries",
			jqQuery: `.meta.version =`,
			value:   "2.0.0",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test.ns", Name: "entry1"},
					Meta: registry.Metadata{
						"existing": "value",
					},
				},
				{
					ID: registry.ID{NS: "test.ns", Name: "entry2"},
					Meta: registry.Metadata{
						"other": "value",
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test.ns", Name: "entry1"},
					Meta: registry.Metadata{
						"existing": "value",
						"version":  "2.0.0",
					},
				},
				{
					ID: registry.ID{NS: "test.ns", Name: "entry2"},
					Meta: registry.Metadata{
						"other":   "value",
						"version": "2.0.0",
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "entry with nil meta",
			jqQuery: `.meta.router =`,
			value:   "system:api",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test.ns", Name: "entry1"},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test.ns", Name: "entry1"},
					Meta: registry.Metadata{
						"router": "system:api",
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "complex jq operations",
			jqQuery: `.meta.services[0].endpoints +=`,
			value:   "/api/v2",
			entries: []registry.Entry{
				{
					ID: registry.ID{NS: "test.ns", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name":      "api",
								"endpoints": []interface{}{"/api/v1"},
							},
						},
					},
				},
			},
			expected: []registry.Entry{
				{
					ID: registry.ID{NS: "test.ns", Name: "entry1"},
					Meta: registry.Metadata{
						"services": []interface{}{
							map[string]interface{}{
								"name":      "api",
								"endpoints": []interface{}{"/api/v1", "/api/v2"},
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a copy of entries for testing
			testEntries := make([]registry.Entry, len(tt.entries))
			for i, entry := range tt.entries {
				testEntries[i] = entry
				if entry.Meta != nil {
					testEntries[i].Meta = make(registry.Metadata)
					for k, v := range entry.Meta {
						testEntries[i].Meta[k] = v
					}
				}
			}

			err := applyPathValueToEntriesWithGojq(tt.jqQuery, tt.value, testEntries)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, testEntries)
			}
		})
	}
}

func TestFindDefinitionTargetEntries(t *testing.T) {
	tests := []struct {
		name               string
		definitionTarget   RequirementTarget
		ns                 string
		entries            []registry.Entry
		expectedEntryNames []string
	}{
		{
			name: "match by entry",
			definitionTarget: RequirementTarget{
				Entry: "target_entry",
				Path:  "meta.router",
			},
			ns: "test.ns",
			entries: []registry.Entry{
				{ID: registry.ID{NS: "test.ns", Name: "target_entry"}},
				{ID: registry.ID{NS: "test.ns", Name: "other_entry"}},
				{ID: registry.ID{NS: "other.ns", Name: "target_entry"}},
			},
			expectedEntryNames: []string{"target_entry"},
		},
		{
			name: "match by path when entry is empty",
			definitionTarget: RequirementTarget{
				Entry: "",
				Path:  "meta.depends_on[]",
			},
			ns: "test.ns",
			entries: []registry.Entry{
				{ID: registry.ID{NS: "test.ns", Name: "entry1"}},
				{ID: registry.ID{NS: "test.ns", Name: "entry2"}},
				{ID: registry.ID{NS: "other.ns", Name: "entry3"}},
			},
			expectedEntryNames: []string{"entry1", "entry2"},
		},
		{
			name: "no matches in namespace",
			definitionTarget: RequirementTarget{
				Entry: "target_entry",
				Path:  "meta.router",
			},
			ns: "test.ns",
			entries: []registry.Entry{
				{ID: registry.ID{NS: "other.ns", Name: "target_entry"}},
			},
			expectedEntryNames: []string{},
		},
		{
			name: "empty path when entry is empty",
			definitionTarget: RequirementTarget{
				Entry: "",
				Path:  "",
			},
			ns: "test.ns",
			entries: []registry.Entry{
				{ID: registry.ID{NS: "test.ns", Name: "entry1"}},
			},
			expectedEntryNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findDefinitionTargetEntries(tt.definitionTarget, tt.ns, tt.entries)

			assert.Len(t, result, len(tt.expectedEntryNames))
			for i, expectedName := range tt.expectedEntryNames {
				assert.Equal(t, expectedName, result[i].ID.Name)
			}
		})
	}
}

func TestGetDefinitionTargets(t *testing.T) {
	tests := []struct {
		name            string
		definition      registry.Entry
		expectedTargets []RequirementTarget
		wantErr         bool
		errMsg          string
	}{
		{
			name: "valid definition with targets",
			definition: registry.Entry{
				ID: registry.ID{NS: "test.ns", Name: "test_definition"},
				Data: payload.New(map[string]interface{}{
					"targets": []interface{}{
						map[string]interface{}{
							"entry": "target1",
							"path":  "meta.router",
						},
						map[string]interface{}{
							"entry": "",
							"path":  "meta.depends_on[]",
						},
					},
				}),
			},
			expectedTargets: []RequirementTarget{
				{Entry: "target1", Path: "meta.router"},
				{Entry: "", Path: "meta.depends_on[]"},
			},
			wantErr: false,
		},
		{
			name: "definition without targets",
			definition: registry.Entry{
				ID: registry.ID{NS: "test.ns", Name: "test_definition"},
				Data: payload.New(map[string]interface{}{
					"other_field": "value",
				}),
			},
			wantErr: true,
			errMsg:  "invalid requirement data in definition test_definition",
		},
		{
			name: "definition with invalid targets format",
			definition: registry.Entry{
				ID: registry.ID{NS: "test.ns", Name: "test_definition"},
				Data: payload.New(map[string]interface{}{
					"targets": "not_an_array",
				}),
			},
			wantErr: true,
			errMsg:  "invalid requirement data in definition test_definition",
		},
		{
			name: "definition with invalid target format",
			definition: registry.Entry{
				ID: registry.ID{NS: "test.ns", Name: "test_definition"},
				Data: payload.New(map[string]interface{}{
					"targets": []interface{}{
						"not_a_map",
					},
				}),
			},
			expectedTargets: []RequirementTarget{},
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := getDefinitionTargets(tt.definition)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedTargets, result)
			}
		})
	}
}

func TestFindRequirementDefinition(t *testing.T) {
	tests := []struct {
		name           string
		requirement    registry.Entry
		nsDefinitions  map[string]registry.Entry
		expectedResult registry.Entry
		wantErr        bool
		errMsg         string
	}{
		{
			name: "definition found",
			requirement: registry.Entry{
				ID: registry.ID{NS: "test.ns", Name: "API_ROUTER"},
			},
			nsDefinitions: map[string]registry.Entry{
				"API_ROUTER": {
					ID:   registry.ID{NS: "test.ns", Name: "API_ROUTER"},
					Kind: registry.KindNamespaceRequirement,
				},
			},
			expectedResult: registry.Entry{
				ID:   registry.ID{NS: "test.ns", Name: "API_ROUTER"},
				Kind: registry.KindNamespaceRequirement,
			},
			wantErr: false,
		},
		{
			name: "definition not found",
			requirement: registry.Entry{
				ID: registry.ID{NS: "test.ns", Name: "NON_EXISTENT"},
			},
			nsDefinitions: map[string]registry.Entry{
				"API_ROUTER": {
					ID:   registry.ID{NS: "test.ns", Name: "API_ROUTER"},
					Kind: registry.KindNamespaceRequirement,
				},
			},
			wantErr: true,
			errMsg:  "definition for requirement NON_EXISTENT not found",
		},
		{
			name: "empty definitions map",
			requirement: registry.Entry{
				ID: registry.ID{NS: "test.ns", Name: "API_ROUTER"},
			},
			nsDefinitions: map[string]registry.Entry{},
			wantErr:       true,
			errMsg:        "definition for requirement API_ROUTER not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := findRequirementDefinition(tt.requirement, tt.nsDefinitions)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedResult, result)
			}
		})
	}
}

func TestResolveModuleRequirements(t *testing.T) {
	logger := zap.NewNop()
	resolver := NewResolver(logger)

	// Create test entries
	entries := []registry.Entry{
		// Dependency
		{
			ID:   registry.ID{NS: "test.ns", Name: "test_dependency"},
			Kind: registry.KindNamespaceDependency,
			Data: payload.New(map[string]interface{}{
				"parameters": []interface{}{
					map[string]interface{}{
						"name":  "api_router",
						"value": "system:api",
					},
					map[string]interface{}{
						"name":  "text",
						"value": "Hello World",
					},
				},
				"namespace": "test.ns",
			}),
		},
		// Requirement
		{
			ID:   registry.ID{NS: "test.ns", Name: "API_ROUTER"},
			Kind: registry.KindNamespaceDefinition,
			Data: payload.New(map[string]interface{}{
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "test_dependency",
						"path":  `.parameters[] | select(.name == "api_router") | .value`,
					},
				},
			}),
		},
		// Definition
		{
			ID:   registry.ID{NS: "test.ns", Name: "API_ROUTER"},
			Kind: registry.KindNamespaceRequirement,
			Data: payload.New(map[string]interface{}{
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "test_endpoint",
						"path":  ".meta.router",
					},
				},
			}),
		},
		// Target entry to be modified
		{
			ID:   registry.ID{NS: "test.ns", Name: "test_endpoint"},
			Kind: "http.endpoint",
			Meta: registry.Metadata{
				"existing": "value",
			},
		},
	}

	err := resolver.ResolveModuleRequirements(entries)
	require.NoError(t, err)

	// Find the target entry that should have been modified
	var targetEntry *registry.Entry
	for i := range entries {
		if entries[i].ID.Name == "test_endpoint" {
			targetEntry = &entries[i]
			break
		}
	}

	require.NotNil(t, targetEntry)
	assert.Equal(t, "system:api", targetEntry.Meta["router"])
}

func TestResolveModuleRequirements_NoRequirements(t *testing.T) {
	logger := zap.NewNop()
	resolver := NewResolver(logger)

	entries := []registry.Entry{
		{
			ID:   registry.ID{NS: "test.ns", Name: "test"},
			Kind: "test",
		},
	}

	err := resolver.ResolveModuleRequirements(entries)
	require.NoError(t, err)
}

func TestResolveModuleRequirements_RequirementNotFound(t *testing.T) {
	logger := zap.NewNop()
	resolver := NewResolver(logger)

	entries := []registry.Entry{
		// Requirement without matching dependency
		{
			ID:   registry.ID{NS: "test.ns", Name: "API_ROUTER"},
			Kind: registry.KindNamespaceDefinition,
			Data: payload.New(map[string]interface{}{
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "non_existent_dependency",
						"path":  "some.path",
					},
				},
			}),
		},
	}

	err := resolver.ResolveModuleRequirements(entries)
	require.NoError(t, err) // Should not error, just log warning
}

func TestResolveModuleRequirements_DefinitionNotFound(t *testing.T) {
	logger := zap.NewNop()
	resolver := NewResolver(logger)

	entries := []registry.Entry{
		// Dependency
		{
			ID:   registry.ID{NS: "test.ns", Name: "test_dependency"},
			Kind: registry.KindNamespaceDependency,
			Data: payload.New(map[string]interface{}{
				"parameters": []interface{}{
					map[string]interface{}{
						"name":  "api_router",
						"value": "system:api",
					},
				},
			}),
		},
		// Requirement without matching definition
		{
			ID:   registry.ID{NS: "test.ns", Name: "API_ROUTER"},
			Kind: registry.KindNamespaceDefinition,
			Data: payload.New(map[string]interface{}{
				"targets": []interface{}{
					map[string]interface{}{
						"entry": "test_dependency",
						"path":  `.parameters[] | select(.name == "api_router") | .value`,
					},
				},
			}),
		},
	}

	err := resolver.ResolveModuleRequirements(entries)
	require.NoError(t, err) // Should not error, just log warning
}

func TestEntryToRawJSONMap(t *testing.T) {
	entry := registry.Entry{
		ID:   registry.ID{NS: "test.ns", Name: "test"},
		Kind: "test.kind",
		Meta: registry.Metadata{
			"key1": "value1",
			"key2": 42,
		},
		Data: payload.New(map[string]interface{}{
			"data_key": "data_value",
		}),
	}

	result, err := entryToRawJSONMap(&entry)
	require.NoError(t, err)

	// Verify the result contains the expected fields
	assert.Equal(t, "test.ns:test", result["id"])
	assert.Equal(t, "test.kind", result["kind"])

	// Verify meta was preserved
	meta, ok := result["meta"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "value1", meta["key1"])
	assert.Equal(t, float64(42), meta["key2"])

	// Verify Data field was temporarily removed during conversion
	_, hasData := result["data"]
	assert.True(t, hasData) // Data field is present but set to null
}

func TestEntryToRawJSONMap_EmptyEntry(t *testing.T) {
	entry := registry.Entry{}

	result, err := entryToRawJSONMap(&entry)
	require.NoError(t, err)

	assert.Equal(t, "", result["id"])
	assert.Equal(t, "", result["kind"])
}

func TestEntryToRawJSONMap_NilData(t *testing.T) {
	entry := registry.Entry{
		ID:   registry.ID{NS: "test.ns", Name: "test"},
		Kind: "test.kind",
		Data: payload.New(nil),
	}

	result, err := entryToRawJSONMap(&entry)
	require.NoError(t, err)

	assert.Equal(t, "test.ns:test", result["id"])
	assert.Equal(t, "test.kind", result["kind"])
}

func TestUpdateEntryFromRawJSONMap(t *testing.T) {
	originalEntry := registry.Entry{
		ID:   registry.ID{NS: "test.ns", Name: "test"},
		Kind: "test.kind",
		Meta: registry.Metadata{
			"key1": "value1",
		},
		Data: payload.New(map[string]interface{}{
			"data_key": "data_value",
		}),
	}

	// Create a map with updated values
	updatedMap := map[string]interface{}{
		"id":   "updated.ns:updated_name",
		"kind": "updated.kind",
		"meta": map[string]interface{}{
			"key1": "updated_value",
			"key2": "new_value",
		},
	}

	err := updateEntryFromRawJSONMap(&originalEntry, updatedMap)
	require.NoError(t, err)

	// Verify the entry was updated
	assert.Equal(t, "updated.ns", originalEntry.ID.NS)
	assert.Equal(t, "updated_name", originalEntry.ID.Name)
	assert.Equal(t, "updated.kind", originalEntry.Kind)
	assert.Equal(t, "updated_value", originalEntry.Meta["key1"])
	assert.Equal(t, "new_value", originalEntry.Meta["key2"])

	// Verify Data field was preserved
	assert.NotNil(t, originalEntry.Data)
}

func TestUpdateEntryFromRawJSONMap_EmptyMap(t *testing.T) {
	originalEntry := registry.Entry{
		ID:   registry.ID{NS: "test.ns", Name: "test"},
		Kind: "test.kind",
		Meta: registry.Metadata{
			"key1": "value1",
		},
		Data: payload.New("test_data"),
	}

	emptyMap := map[string]interface{}{}

	err := updateEntryFromRawJSONMap(&originalEntry, emptyMap)
	require.NoError(t, err)

	// Verify the entry fields remain unchanged when empty map is provided
	// (JSON unmarshaling doesn't overwrite fields not present in JSON)
	assert.Equal(t, "test.ns", originalEntry.ID.NS)
	assert.Equal(t, "test", originalEntry.ID.Name)
	assert.Equal(t, "test.kind", originalEntry.Kind)
	assert.Equal(t, "value1", originalEntry.Meta["key1"])

	// Verify Data field was preserved
	assert.NotNil(t, originalEntry.Data)
}

func TestUpdateEntryFromRawJSONMap_InvalidMap(t *testing.T) {
	originalEntry := registry.Entry{
		ID:   registry.ID{NS: "test.ns", Name: "test"},
		Kind: "test.kind",
	}

	// Create an invalid map that can't be marshaled to JSON
	invalidMap := map[string]interface{}{
		"invalid": make(chan int), // Channels can't be marshaled to JSON
	}

	err := updateEntryFromRawJSONMap(&originalEntry, invalidMap)
	require.Error(t, err)
}

func TestSetValueWithGojqReturnMap(t *testing.T) {
	tests := []struct {
		name     string
		data     interface{}
		path     string
		value    string
		expected map[string]interface{}
		wantErr  bool
		errMsg   string
	}{
		{
			name: "simple assignment",
			data: map[string]interface{}{
				"existing": "value",
			},
			path:  ".new_field",
			value: "new_value",
			expected: map[string]interface{}{
				"existing":  "value",
				"new_field": "new_value",
			},
			wantErr: false,
		},
		{
			name: "assignment with existing operator",
			data: map[string]interface{}{
				"existing": "value",
			},
			path:  ".new_field =",
			value: "new_value",
			expected: map[string]interface{}{
				"existing":  "value",
				"new_field": "new_value",
			},
			wantErr: false,
		},
		{
			name: "append to array",
			data: map[string]interface{}{
				"items": []interface{}{"item1", "item2"},
			},
			path:  ".items +=",
			value: "item3",
			expected: map[string]interface{}{
				"items": []interface{}{"item1", "item2", "item3"},
			},
			wantErr: false,
		},
		{
			name: "nested field assignment",
			data: map[string]interface{}{
				"config": map[string]interface{}{
					"existing": "value",
				},
			},
			path:  ".config.database.host",
			value: "localhost",
			expected: map[string]interface{}{
				"config": map[string]interface{}{
					"existing": "value",
					"database": map[string]interface{}{
						"host": "localhost",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "array element assignment",
			data: map[string]interface{}{
				"services": []interface{}{
					map[string]interface{}{
						"name": "api",
						"port": 8080,
					},
				},
			},
			path:  `.services[] | select(.name == "api") | .version =`,
			value: "1.0.0",
			expected: map[string]interface{}{
				"name":    "api",
				"port":    8080,
				"version": "1.0.0",
			},
			wantErr: false,
		},
		{
			name: "invalid jq query",
			data: map[string]interface{}{
				"value": "test",
			},
			path:    "invalid[query",
			value:   "test",
			wantErr: true,
			errMsg:  "failed to parse jq query",
		},
		{
			name:    "non-map result",
			data:    "string_value",
			path:    ".field",
			value:   "test",
			wantErr: true,
			errMsg:  "setpath([\"field\"]; \"test\") cannot be applied to \"string_value\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := setValueWithGojqReturnMap(tt.data, tt.path, tt.value)

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

func TestJoinErrors(t *testing.T) {
	tests := []struct {
		name     string
		errors   []error
		expected string
	}{
		{
			name:     "no errors",
			errors:   []error{},
			expected: "",
		},
		{
			name: "single error",
			errors: []error{
				fmt.Errorf("single error"),
			},
			expected: "single error",
		},
		{
			name: "multiple errors",
			errors: []error{
				fmt.Errorf("first error"),
				fmt.Errorf("second error"),
				fmt.Errorf("third error"),
			},
			expected: "first error\nsecond error\nthird error",
		},
		{
			name: "nil errors",
			errors: []error{
				nil,
				fmt.Errorf("valid error"),
				nil,
			},
			expected: "valid error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := joinErrors(tt.errors)

			if tt.expected == "" {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expected, result.Error())
			}
		})
	}
}
