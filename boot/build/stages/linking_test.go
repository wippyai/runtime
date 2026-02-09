package stages

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

func TestLink_WithDefault(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "req1"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "default_value",
				"targets": []any{
					map[string]any{
						"entry": "target1",
						"path":  ".field",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "target1"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Verify value was set
	target := findEntry(entries, "test", "target1")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	assert.Equal(t, "default_value", data["field"])
}

func TestLink_FromDependency(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.module"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/module",
				"parameters": []any{
					map[string]any{
						"name":  "db_url",
						"value": "postgres://localhost",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "db_url"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "service",
						"path":  ".database.url",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Verify dependency parameter was used
	target := findEntry(entries, "vendor.module", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	database := data["database"].(map[string]any)
	assert.Equal(t, "postgres://localhost", database["url"])
}

func TestLink_ExplicitDependenciesOverride(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.module"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/module",
				"parameters": []any{
					map[string]any{
						"name":  "db_url",
						"value": "postgres://from-entries",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "db_url"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "service",
						"path":  ".database.url",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	explicitDeps := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.override"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/module",
				"parameters": []any{
					map[string]any{
						"name":  "db_url",
						"value": "postgres://explicit",
					},
				},
			}),
		},
	}

	stage := Link(WithDependencies(explicitDeps))
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	target := findEntry(entries, "vendor.module", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	database := data["database"].(map[string]any)
	assert.Equal(t, "postgres://explicit", database["url"])
}

func TestLink_ConflictError(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.module1"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/module",
				"parameters": []any{
					map[string]any{
						"name":  "api_key",
						"value": "key1",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("app", "__dependency.module2"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/module",
				"parameters": []any{
					map[string]any{
						"name":  "api_key",
						"value": "key2", // Different value!
					},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "api_key"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "service",
						"path":  ".api_key",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	// Linking stage now logs warnings instead of returning errors
	require.NoError(t, err)
}

func TestLink_ModuleScopedParameters(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.modA"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/a",
				"parameters": []any{
					map[string]any{
						"name":  "router",
						"value": "app:router_a",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("app", "__dependency.modB"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/b",
				"parameters": []any{
					map[string]any{
						"name":  "router",
						"value": "app:router_b",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.a", "router"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "endpoint",
						"path":  ".meta.router",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.a", "endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	target := findEntry(entries, "vendor.a", "endpoint")
	require.NotNil(t, target)
	assert.Equal(t, "app:router_a", target.Meta["router"])
}

func TestLink_FullIDParameterName(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.telegram"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "butschster/telegram",
				"parameters": []any{
					map[string]any{
						"name":  "telegram:env_storage",
						"value": "app:env_file",
					},
					map[string]any{
						"name":  "telegram:webhook_router",
						"value": "app:router",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("telegram", "env_storage"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "telegram:bot_token",
						"path":  ".storage",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("telegram", "webhook_router"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "telegram.handler:webhook_endpoint",
						"path":  ".meta.router",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("telegram", "bot_token"),
			Kind: "env.variable",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("telegram.handler", "webhook_endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Storage set via data path
	botToken := findEntry(entries, "telegram", "bot_token")
	require.NotNil(t, botToken)
	data := botToken.Data.Data().(map[string]any)
	assert.Equal(t, "app:env_file", data["storage"])

	// Router set via meta path
	endpoint := findEntry(entries, "telegram.handler", "webhook_endpoint")
	require.NotNil(t, endpoint)
	assert.Equal(t, "app:router", endpoint.Meta["router"])
}

func TestLink_NoValueError(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "missing_param"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "service",
						"path":  ".field",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	// Linking stage now logs warnings instead of returning errors
	require.NoError(t, err)
}

func TestLink_AppendOperator(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "dep_req"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "new_dep",
				"targets": []any{
					map[string]any{
						"entry": "service",
						"path":  ".depends_on +=",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"depends_on": []any{"existing_dep"},
			}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Verify value was appended
	target := findEntry(entries, "test", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	deps := data["depends_on"].([]any)
	assert.Equal(t, []any{"existing_dep", "new_dep"}, deps)
}

func TestLink_SetValue(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "host_req"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "localhost",
				"targets": []any{
					map[string]any{
						"entry": "service",
						"path":  ".host",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Verify value was set
	target := findEntry(entries, "test", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	assert.Equal(t, "localhost", data["host"])
}

func TestLink_EmptyEntryError(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "global_config"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "shared_value",
				"targets": []any{
					map[string]any{
						"entry": "", // Empty entry not supported
						"path":  ".shared",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "service1"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	// Linking stage now logs warnings instead of returning errors
	require.NoError(t, err)
}

func TestLink_CrossNamespace(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "api_url"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "https://api.example.com",
				"targets": []any{
					map[string]any{
						"entry": "other:service", // Cross-namespace
						"path":  ".api.url",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("other", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Verify value was set in different namespace
	target := findEntry(entries, "other", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	api := data["api"].(map[string]any)
	assert.Equal(t, "https://api.example.com", api["url"])
}

func TestLink_MultipleTargets(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "db_url"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "postgres://db",
				"targets": []any{
					map[string]any{
						"entry": "service1",
						"path":  ".database.url",
					},
					map[string]any{
						"entry": "service2",
						"path":  ".db_connection",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "service1"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("test", "service2"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Verify both targets were updated
	service1 := findEntry(entries, "test", "service1")
	require.NotNil(t, service1)
	data1 := service1.Data.Data().(map[string]any)
	database := data1["database"].(map[string]any)
	assert.Equal(t, "postgres://db", database["url"])

	service2 := findEntry(entries, "test", "service2")
	require.NotNil(t, service2)
	data2 := service2.Data.Data().(map[string]any)
	assert.Equal(t, "postgres://db", data2["db_connection"])
}

func TestLink_BarePath(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "storage_req"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "/tmp/storage",
				"targets": []any{
					map[string]any{
						"entry": "service",
						"path":  ".default", // Bare path -> data.default
					},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Verify bare path was treated as data.default
	target := findEntry(entries, "test", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	assert.Equal(t, "/tmp/storage", data["default"])
}

func TestLink_MetaPath(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "router_req"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "app:api",
				"targets": []any{
					map[string]any{
						"entry": "endpoint",
						"path":  ".meta.router",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Verify meta field was set
	target := findEntry(entries, "test", "endpoint")
	require.NotNil(t, target)
	assert.Equal(t, "app:api", target.Meta["router"])
}

func TestLink_MultipleRequirements(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "host"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "localhost",
				"targets": []any{
					map[string]any{
						"entry": "service",
						"path":  ".host",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "port"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "8080",
				"targets": []any{
					map[string]any{
						"entry": "service",
						"path":  ".port",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	// Verify both requirements were applied
	target := findEntry(entries, "test", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	assert.Equal(t, "localhost", data["host"])
	assert.Equal(t, "8080", data["port"])
}

// Helper functions

type mockTranscoder struct{}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	data := p.Data()
	return payload.NewPayload(data, payload.Golang), nil
}

func (m *mockTranscoder) Marshal(v any) (payload.Payload, error) {
	return payload.New(v), nil
}

func (m *mockTranscoder) Unmarshal(p payload.Payload, v any) error {
	// Use JSON-like conversion for testing
	data := p.Data()
	if dataMap, ok := data.(map[string]any); ok {
		// Simple reflection-based assignment for test structs
		if reqDef, ok := v.(*RequirementDefinition); ok {
			if def, ok := dataMap["default"].(string); ok {
				reqDef.Default = def
			}
			if targets, ok := dataMap["targets"].([]any); ok {
				for _, t := range targets {
					if tMap, ok := t.(map[string]any); ok {
						target := RequirementTarget{}
						if entry, ok := tMap["entry"].(string); ok {
							target.Entry = entry
						}
						if path, ok := tMap["path"].(string); ok {
							target.Path = path
						}
						reqDef.Targets = append(reqDef.Targets, target)
					}
				}
			}
		} else if depDef, ok := v.(*DependencyDefinition); ok {
			if comp, ok := dataMap["component"].(string); ok {
				depDef.Component = comp
			}
			if ver, ok := dataMap["version"].(string); ok {
				depDef.Version = ver
			}
			if params, ok := dataMap["parameters"].([]any); ok {
				for _, p := range params {
					if pMap, ok := p.(map[string]any); ok {
						param := Parameter{}
						if name, ok := pMap["name"].(string); ok {
							param.Name = name
						}
						if value, ok := pMap["value"].(string); ok {
							param.Value = value
						}
						depDef.Parameters = append(depDef.Parameters, param)
					}
				}
			}
		} else if modDef, ok := v.(*ModuleDefinition); ok {
			if module, ok := dataMap["module"].(string); ok {
				modDef.Module = module
			}
			if readme, ok := dataMap["readme"].(string); ok {
				modDef.Readme = readme
			}
		}
	}
	return nil
}

func setupTestContext() (context.Context, payload.Transcoder) {
	transcoder := &mockTranscoder{}
	appCtx := ctxapi.NewAppContext()
	ctx := context.Background()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx = payload.WithTranscoder(ctx, transcoder)
	return ctx, transcoder
}

func findEntry(entries []registry.Entry, ns, name string) *registry.Entry {
	for i := range entries {
		if entries[i].ID.NS == ns && entries[i].ID.Name == name {
			return &entries[i]
		}
	}
	return nil
}
