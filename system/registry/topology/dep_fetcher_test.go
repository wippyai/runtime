package topology

import (
	"encoding/json"
	"testing"

	"github.com/ponyruntime/pony/api/payload"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
)

// DepTestSDK provides a fluent API for testing dependency extraction
type DepTestSDK struct {
	t *testing.T
}

// NewDepTestSDK creates a new test SDK instance
func NewDepTestSDK(t *testing.T) *DepTestSDK {
	return &DepTestSDK{t: t}
}

// TestPayload implements payload.Payload for testing
type TestPayload struct {
	data any
}

func (p *TestPayload) Data() any {
	return p.data
}

func (p *TestPayload) Format() payload.Format {
	return payload.Golang
}

// EntryFromJSON creates a registry entry from JSON string
func (sdk *DepTestSDK) EntryFromJSON(jsonStr string) registry.Entry {
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
		ID:   registry.ParseID(entryData.ID),
		Kind: entryData.Kind,
		Meta: entryData.Meta,
	}

	if entryData.Data != nil {
		entry.Data = &TestPayload{data: entryData.Data}
	}

	return entry
}

// DepTest represents a single dependency test case
type DepTest struct {
	Name     string
	Entry    string // JSON string
	Expected []string
}

// DepTestSuite represents a collection of related dependency tests
type DepTestSuite struct {
	Name  string
	Tests []DepTest
}

// RunTest executes a single dependency test
func (sdk *DepTestSDK) RunTest(test DepTest) {
	sdk.t.Run(test.Name, func(t *testing.T) {
		entry := sdk.EntryFromJSON(test.Entry)
		deps := fetchDependencies(entry)
		assert.ElementsMatch(t, test.Expected, deps,
			"Dependencies mismatch for entry: %s", test.Name)
	})
}

// RunSuite executes a test suite
func (sdk *DepTestSDK) RunSuite(suite DepTestSuite) {
	sdk.t.Run(suite.Name, func(t *testing.T) {
		testSDK := NewDepTestSDK(t)
		for _, test := range suite.Tests {
			testSDK.RunTest(test)
		}
	})
}

// ExpectDeps is a fluent assertion helper
func (sdk *DepTestSDK) ExpectDeps(entryJSON string) *DepAssertion {
	entry := sdk.EntryFromJSON(entryJSON)
	deps := fetchDependencies(entry)
	return &DepAssertion{
		t:    sdk.t,
		deps: deps,
		json: entryJSON,
	}
}

// DepAssertion provides fluent dependency assertions
type DepAssertion struct {
	t    *testing.T
	deps []string
	json string
}

// ToEqual asserts exact dependency match
func (da *DepAssertion) ToEqual(expected ...string) *DepAssertion {
	assert.ElementsMatch(da.t, expected, da.deps,
		"Dependencies mismatch for entry JSON: %s", da.json)
	return da
}

// ToContain asserts dependencies contain specific values
func (da *DepAssertion) ToContain(expected ...string) *DepAssertion {
	for _, exp := range expected {
		assert.Contains(da.t, da.deps, exp,
			"Expected dependency '%s' not found in: %v", exp, da.deps)
	}
	return da
}

// ToNotContain asserts dependencies don't contain specific values
func (da *DepAssertion) ToNotContain(unexpected ...string) *DepAssertion {
	for _, unexp := range unexpected {
		assert.NotContains(da.t, da.deps, unexp,
			"Unexpected dependency '%s' found in: %v", unexp, da.deps)
	}
	return da
}

// ToBeEmpty asserts no dependencies found
func (da *DepAssertion) ToBeEmpty() *DepAssertion {
	assert.Empty(da.t, da.deps, "Expected no dependencies but found: %v", da.deps)
	return da
}

// ToHaveCount asserts specific dependency count
func (da *DepAssertion) ToHaveCount(count int) *DepAssertion {
	assert.Len(da.t, da.deps, count,
		"Expected %d dependencies but found %d: %v", count, len(da.deps), da.deps)
	return da
}

// WithDetails prints the found dependencies for debugging
func (da *DepAssertion) WithDetails() *DepAssertion {
	da.t.Logf("Found dependencies: %v", da.deps)
	return da
}

// SimpleTest creates a quick test case
func SimpleTest(name, entryJSON string, expected ...string) DepTest {
	return DepTest{
		Name:     name,
		Entry:    entryJSON,
		Expected: expected,
	}
}

func TestDependencyExtraction_MetaPaths(t *testing.T) {
	sdk := NewDepTestSDK(t)

	// Test all meta.* dependency paths
	sdk.RunSuite(DepTestSuite{
		Name: "Meta Path Dependencies",
		Tests: []DepTest{
			// meta.server
			SimpleTest("meta.server reference",
				`{"id": "test", "kind": "service", "meta": {"server": "gateway"}}`,
				"gateway"),

			// meta.router
			SimpleTest("meta.router reference",
				`{"id": "test", "kind": "router", "meta": {"router": "main-router"}}`,
				"main-router"),

			// meta.parent
			SimpleTest("meta.parent reference",
				`{"id": "test", "kind": "service", "meta": {"parent": "parent-service"}}`,
				"parent-service"),

			// meta.groups
			SimpleTest("meta.groups string",
				`{"id": "test", "kind": "service", "meta": {"groups": "backend-services"}}`,
				"backend-services"),
			SimpleTest("meta.groups array",
				`{"id": "test", "kind": "service", "meta": {"groups": ["group1", "group2"]}}`,
				"group1", "group2"),
		},
	})
}

func TestDependencyExtraction_MetaDependsOn(t *testing.T) {
	sdk := NewDepTestSDK(t)

	// Test meta.depends_on - the manual fallback for when automatic detection fails
	sdk.RunSuite(DepTestSuite{
		Name: "Meta Depends On (Manual Fallback)",
		Tests: []DepTest{
			SimpleTest("single depends_on string",
				`{"id": "test", "kind": "service", "meta": {"depends_on": "database"}}`,
				"database"),
			SimpleTest("depends_on array",
				`{"id": "test", "kind": "service", "meta": {"depends_on": ["db", "cache", "queue"]}}`,
				"db", "cache", "queue"),
			SimpleTest("namespace dependency",
				`{"id": "test", "kind": "service", "meta": {"depends_on": ["ns:userspace.oauth:connector"]}}`,
				"ns:userspace.oauth:connector"),
			SimpleTest("group dependency",
				`{"id": "test", "kind": "service", "meta": {"depends_on": ["group:backend-services"]}}`,
				"group:backend-services"),
			SimpleTest("mixed dependency types",
				`{"id": "test", "kind": "service", "meta": {"depends_on": ["database", "ns:shared", "group:apis"]}}`,
				"database", "ns:shared", "group:apis"),
			SimpleTest("complex namespaced dependencies",
				`{"id": "test", "kind": "service", "meta": {"depends_on": ["app:database", "shared:config", "ns:backend"]}}`,
				"app:database", "shared:config", "ns:backend"),
		},
	})
}

func TestDependencyExtraction_DataPaths(t *testing.T) {
	sdk := NewDepTestSDK(t)

	// Test basic data.* dependency paths
	sdk.RunSuite(DepTestSuite{
		Name: "Data Path Dependencies",
		Tests: []DepTest{
			// Basic data fields
			SimpleTest("data.server",
				`{"id": "test", "kind": "service", "data": {"server": "http-server"}}`,
				"http-server"),
			SimpleTest("data.fs",
				`{"id": "test", "kind": "service", "data": {"fs": "public-fs"}}`,
				"public-fs"),
			SimpleTest("data.store",
				`{"id": "test", "kind": "service", "data": {"store": "session-store"}}`,
				"session-store"),
			SimpleTest("data.set",
				`{"id": "test", "kind": "service", "data": {"set": "template-set"}}`,
				"template-set"),
			SimpleTest("data.host",
				`{"id": "test", "kind": "service", "data": {"host": "process-host"}}`,
				"process-host"),
			SimpleTest("data.process",
				`{"id": "test", "kind": "service", "data": {"process": "worker-process"}}`,
				"worker-process"),
			SimpleTest("data.bucket",
				`{"id": "test", "kind": "service", "data": {"bucket": "s3-bucket"}}`,
				"s3-bucket"),
			SimpleTest("data.config",
				`{"id": "test", "kind": "service", "data": {"config": "aws-config"}}`,
				"aws-config"),
			SimpleTest("data.func",
				`{"id": "test", "kind": "service", "data": {"func": "handler-function"}}`,
				"handler-function"),
		},
	})

	// Test token store specifically
	sdk.RunSuite(DepTestSuite{
		Name: "Token Store Dependencies",
		Tests: []DepTest{
			SimpleTest("data.token_store",
				`{"id": "test", "kind": "service", "data": {"token_store": "app:security.tokens"}}`,
				"app:security.tokens"),
			SimpleTest("data.security.token_store",
				`{"id": "test", "kind": "service", "data": {"security": {"token_store": "auth-tokens"}}}`,
				"auth-tokens"),
		},
	})

	// Test array dependencies
	sdk.RunSuite(DepTestSuite{
		Name: "Array Data Dependencies",
		Tests: []DepTest{
			SimpleTest("data.middleware array",
				`{"id": "test", "kind": "router", "data": {"middleware": ["cors", "auth", "logging"]}}`,
				"cors", "auth", "logging"),
			SimpleTest("data.post_middleware array",
				`{"id": "test", "kind": "router", "data": {"post_middleware": ["firewall", "cleanup"]}}`,
				"firewall", "cleanup"),
			SimpleTest("data.groups array",
				`{"id": "test", "kind": "service", "data": {"groups": ["backend", "api"]}}`,
				"backend", "api"),
		},
	})
}

func TestDependencyExtraction_WildcardPaths(t *testing.T) {
	sdk := NewDepTestSDK(t)

	// Test wildcard paths like data.imports.*, data.contracts.*
	sdk.RunSuite(DepTestSuite{
		Name: "Wildcard Dependencies",
		Tests: []DepTest{
			// data.imports.*
			SimpleTest("data.imports map",
				`{"id": "test", "kind": "service", "data": {"imports": {"lib1": "library-1", "lib2": "library-2"}}}`,
				"library-1", "library-2"),

			// data.contracts.*.contract
			SimpleTest("contract bindings",
				`{"id": "test", "kind": "service", "data": {"contracts": {"api": {"contract": "api-contract"}, "db": {"contract": "db-contract"}}}}`,
				"api-contract", "db-contract"),

			// data.contracts.*.methods.*
			SimpleTest("contract method implementations",
				`{"id": "test", "kind": "service", "data": {"contracts": {"api": {"methods": {"get": "get-handler", "post": "post-handler"}}}}}`,
				"get-handler", "post-handler"),

			// data.*.depends_on
			SimpleTest("nested depends_on in data",
				`{"id": "test", "kind": "service", "data": {"section1": {"depends_on": ["dep1", "dep2"]}, "section2": {"depends_on": "dep3"}}}`,
				"dep1", "dep2", "dep3"),
		},
	})
}

func TestDependencyExtraction_SecurityPaths(t *testing.T) {
	sdk := NewDepTestSDK(t)

	// Test security-related dependency paths
	sdk.RunSuite(DepTestSuite{
		Name: "Security Dependencies",
		Tests: []DepTest{
			// Lifecycle security
			SimpleTest("lifecycle security policies",
				`{"id": "test", "kind": "service", "data": {"lifecycle": {"security": {"policies": ["policy1", "policy2"]}}}}`,
				"policy1", "policy2"),
			SimpleTest("lifecycle security groups",
				`{"id": "test", "kind": "service", "data": {"lifecycle": {"security": {"groups": ["admin", "user"]}}}}`,
				"admin", "user"),
			SimpleTest("lifecycle depends_on",
				`{"id": "test", "kind": "service", "data": {"lifecycle": {"depends_on": ["startup-service"]}}}`,
				"startup-service"),

			// Direct security
			SimpleTest("direct security policies",
				`{"id": "test", "kind": "service", "data": {"security": {"policies": ["read-policy", "write-policy"]}}}`,
				"read-policy", "write-policy"),
			SimpleTest("direct security groups",
				`{"id": "test", "kind": "service", "data": {"security": {"groups": ["developers", "admins"]}}}`,
				"developers", "admins"),
		},
	})
}

func TestDependencyExtraction_RealWorldExamples(t *testing.T) {
	sdk := NewDepTestSDK(t)

	// Test patterns from actual YAML files
	sdk.RunSuite(DepTestSuite{
		Name: "Real World Examples",
		Tests: []DepTest{
			// OAuth base connector with meta.depends_on fallback
			SimpleTest("oauth base connector (meta fallback)",
				`{
					"id": "userspace.oauth:base_connector",
					"kind": "contract.binding", 
					"meta": {
						"depends_on": ["ns:userspace.oauth:connector"]
					}
				}`,
				"ns:userspace.oauth:connector"),

			// Function with imports (automatic detection)
			SimpleTest("function with imports (automatic)",
				`{
					"id": "userspace.credentials:delete_func",
					"kind": "function.lua",
					"data": {
						"imports": {
							"component": "userspace.component:component",
							"credentials_repo": "userspace.credentials.persist:credentials_repo"
						}
					}
				}`,
				"userspace.component:component", "userspace.credentials.persist:credentials_repo"),

			// Contract binding with complex contracts (automatic detection)
			SimpleTest("complex contract binding (automatic)",
				`{
					"id": "userspace.credentials:credentials_store",
					"kind": "contract.binding",
					"data": {
						"contracts": {
							"main": {
								"contract": "userspace.credentials:credentials_contract",
								"methods": {
									"get_info": "userspace.credentials:get_info_func",
									"store": "userspace.credentials:store_credentials_func"
								}
							},
							"deletable": {
								"contract": "userspace.contract:deletable",
								"methods": {
									"delete": "userspace.credentials:delete_func"
								}
							}
						}
					}
				}`,
				"userspace.credentials:credentials_contract", "userspace.contract:deletable",
				"userspace.credentials:get_info_func", "userspace.credentials:store_credentials_func", "userspace.credentials:delete_func"),

			// App API router (combination of automatic + fallback)
			SimpleTest("app api router (combined automatic + fallback)",
				`{
					"id": "app:api",
					"kind": "http.router",
					"meta": {
						"depends_on": ["gateway"],
						"server": "gateway"
					},
					"data": {
						"middleware": ["cors", "websocket_relay", "token_auth"],
						"post_middleware": ["endpoint_firewall"]
					}
				}`,
				"gateway", "cors", "websocket_relay", "token_auth", "endpoint_firewall"),

			// Frontend static service
			SimpleTest("frontend static service",
				`{
					"id": "app:frontend", 
					"kind": "http.static",
					"meta": {
						"depends_on": ["gateway", "public"],
						"server": "gateway"
					},
					"data": {
						"fs": "public"
					}
				}`,
				"gateway", "public"),

			// Security token store
			SimpleTest("security token store",
				`{
					"id": "app:security.tokens",
					"kind": "security.token_store",
					"data": {
						"store": "session"
					}
				}`,
				"session"),

			// S3 storage with config
			SimpleTest("s3 storage with config",
				`{
					"id": "app:uploads.s3",
					"kind": "cloudstorage.s3",
					"meta": {
						"depends_on": ["uploads_aws_config"]
					},
					"data": {
						"config": "app:uploads_aws_config"
					}
				}`,
				"uploads_aws_config", "app:uploads_aws_config"),
		},
	})
}

func TestDependencyExtraction_AutomaticVsFallback(t *testing.T) {
	sdk := NewDepTestSDK(t)

	// Test the relationship between automatic detection and meta.depends_on fallback
	sdk.RunSuite(DepTestSuite{
		Name: "Automatic vs Fallback Dependencies",
		Tests: []DepTest{
			// When automatic detection works, both should be found
			SimpleTest("automatic + fallback combined",
				`{
					"id": "test",
					"kind": "service",
					"meta": {
						"depends_on": ["fallback-dep"]
					},
					"data": {
						"store": "automatic-dep",
						"middleware": ["auto-middleware"]
					}
				}`,
				"fallback-dep", "automatic-dep", "auto-middleware"),

			// When automatic detection fails, fallback provides dependencies
			SimpleTest("fallback when automatic fails",
				`{
					"id": "test",
					"kind": "service", 
					"meta": {
						"depends_on": ["manual-override"]
					},
					"data": {
						"some_field": "not-a-dependency"
					}
				}`,
				"manual-override"),

			// Ensure no duplication between automatic and fallback
			SimpleTest("no duplication between automatic and fallback",
				`{
					"id": "test",
					"kind": "service",
					"meta": {
						"depends_on": ["shared-dep"]
					},
					"data": {
						"store": "shared-dep"
					}
				}`,
				"shared-dep"), // Should appear only once

			// Complex real-world case: some deps automatic, some manual
			SimpleTest("complex real-world mixed dependencies",
				`{
					"id": "plugins.semantic_kb:semantic_kb", 
					"kind": "contract.binding",
					"meta": {
						"depends_on": ["wippy.llm:llm", "plugins.semantic_kb.persist:semantic_store"]
					},
					"data": {
						"imports": {
							"component": "userspace.component:component"
						},
						"contracts": {
							"queryable": {
								"contract": "userspace.contract:queryable",
								"methods": {
									"query": "plugins.semantic_kb:query_func"
								}
							}
						}
					}
				}`,
				"wippy.llm:llm", "plugins.semantic_kb.persist:semantic_store",
				"userspace.component:component", "userspace.contract:queryable", "plugins.semantic_kb:query_func"),
		},
	})
}

func TestDependencyExtraction_EdgeCases(t *testing.T) {
	sdk := NewDepTestSDK(t)

	sdk.RunSuite(DepTestSuite{
		Name: "Edge Cases",
		Tests: []DepTest{
			// Empty/null values
			SimpleTest("null values ignored",
				`{"id": "test", "kind": "service", "meta": {"server": null}, "data": {"store": null}}`,
				/* no deps expected */),
			SimpleTest("empty strings ignored",
				`{"id": "test", "kind": "service", "meta": {"server": ""}, "data": {"store": ""}}`,
				/* no deps expected */),
			SimpleTest("empty arrays ignored",
				`{"id": "test", "kind": "service", "meta": {"depends_on": []}, "data": {"middleware": []}}`,
				/* no deps expected */),

			// Mixed arrays with empty values
			SimpleTest("mixed array with empty strings",
				`{"id": "test", "kind": "service", "data": {"middleware": ["auth", "", "logging", ""]}}`,
				"auth", "logging"),
			SimpleTest("depends_on array with empty strings",
				`{"id": "test", "kind": "service", "meta": {"depends_on": ["valid-dep", "", "another-dep"]}}`,
				"valid-dep", "another-dep"),

			// Missing fields
			SimpleTest("no meta or data fields",
				`{"id": "test", "kind": "service"}`,
				/* no deps expected */),
			SimpleTest("only meta field",
				`{"id": "test", "kind": "service", "meta": {"server": "my-server"}}`,
				"my-server"),
			SimpleTest("only data field",
				`{"id": "test", "kind": "service", "data": {"store": "my-store"}}`,
				"my-store"),

			// Special characters and long names
			SimpleTest("special characters in dependencies",
				`{"id": "test", "kind": "service", "meta": {"depends_on": ["app-name:service_name@v2", "namespace.with-dashes:component_with_underscores"]}}`,
				"app-name:service_name@v2", "namespace.with-dashes:component_with_underscores"),
			SimpleTest("very long dependency names",
				`{"id": "test", "kind": "service", "meta": {"depends_on": ["very.long.namespace:very.long.component.name.with.many.dots"]}}`,
				"very.long.namespace:very.long.component.name.with.many.dots"),
		},
	})
}

func TestDependencyExtraction_FluentAPI(t *testing.T) {
	sdk := NewDepTestSDK(t)

	// Test fluent API patterns
	t.Run("Fluent Assertions", func(t *testing.T) {
		// Test ToContain
		sdk.ExpectDeps(`{
			"id": "test",
			"kind": "service", 
			"meta": {"depends_on": ["a", "b", "c"]}
		}`).ToContain("a", "b").ToHaveCount(3)

		// Test ToNotContain
		sdk.ExpectDeps(`{
			"id": "test",
			"kind": "service",
			"data": {"middleware": ["auth", "logging"]}
		}`).ToNotContain("cache").ToContain("auth")

		// Test ToBeEmpty
		sdk.ExpectDeps(`{
			"id": "test",
			"kind": "service",
			"meta": {}
		}`).ToBeEmpty()

		// Test comprehensive extraction
		sdk.ExpectDeps(`{
			"id": "test",
			"kind": "service",
			"meta": {"server": "gateway", "depends_on": ["fallback"]},
			"data": {"store": "cache", "token_store": "tokens"}
		}`).ToEqual("gateway", "fallback", "cache", "tokens")
	})
}
