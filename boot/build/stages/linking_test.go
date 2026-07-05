// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"context"
	"encoding/json"
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

func TestLink_BareParameterMatchesRequirementModuleMeta(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.dataflow"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "wippy/dataflow",
				"parameters": []any{
					map[string]any{
						"name":  "target_db",
						"value": "app:db",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("userspace.dataflow", "target_db"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{
				"module": "wippy/dataflow",
			},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "app:db",
						"path":  ".file",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("app", "db"),
			Kind: "db.sql.sqlite",
			Data: payload.New(map[string]any{
				"file": ":memory:",
			}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	target := findEntry(entries, "app", "db")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	assert.Equal(t, "app:db", data["file"])
}

func TestLink_BareParameterDoesNotCrossDifferentModuleMeta(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.dataflow"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "wippy/dataflow",
				"parameters": []any{
					map[string]any{
						"name":  "target_db",
						"value": "app:db",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("userspace.dataflow", "target_db"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{
				"module": "wippy/dataflow",
			},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "app:db1",
						"path":  ".file",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("other.bundle", "target_db"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{
				"module": "other/module",
			},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "app:db2",
						"path":  ".file",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("app", "db1"),
			Kind: "db.sql.sqlite",
			Data: payload.New(map[string]any{
				"file": ":memory1:",
			}),
		},
		{
			ID:   registry.NewID("app", "db2"),
			Kind: "db.sql.sqlite",
			Data: payload.New(map[string]any{
				"file": ":memory2:",
			}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	target1 := findEntry(entries, "app", "db1")
	require.NotNil(t, target1)
	data1 := target1.Data.Data().(map[string]any)
	assert.Equal(t, "app:db", data1["file"])

	target2 := findEntry(entries, "app", "db2")
	require.NotNil(t, target2)
	data2 := target2.Data.Data().(map[string]any)
	assert.Equal(t, ":memory2:", data2["file"])
}

func TestLink_BareParametersWithSameNameAreScopedToDependencyComponent(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app.deps", "views"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "wippy/views",
				"parameters": []any{
					map[string]any{"name": "api_router", "value": "app:api.views"},
				},
			}),
		},
		{
			ID:   registry.NewID("app.deps", "skills"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "kickside/skills",
				"parameters": []any{
					map[string]any{"name": "api_router", "value": "app:api"},
				},
			}),
		},
		{
			ID:   registry.NewID("wippy.views", "api_router"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "wippy/views"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "pages_endpoint", "path": ".meta.router"},
				},
			}),
		},
		{
			ID:   registry.NewID("wippy.views", "pages_endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirementModules([]string{"wippy/views"}))
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	target := findEntry(entries, "wippy.views", "pages_endpoint")
	require.NotNil(t, target)
	assert.Equal(t, "app:api.views", target.Meta["router"])
}

// TestLink_DuplicateDependencyParametersForSameRequirementConflict verifies
// that two dependencies of equal provenance resolving the same concrete
// requirement id to different values conflict rather than silently picking one.
// Both dependencies are root dependencies (no meta.module), so the deps merge
// layer applies no precedence and both parameters reach the linker; the linker
// is provenance-blind, so the disagreement surfaces as a conflict and, under
// strict enforcement for that module, leaves the requirement unresolved.
func TestLink_DuplicateDependencyParametersForSameRequirementConflict(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app.deps", "bootloader"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "wippy/bootloader",
				"parameters": []any{
					map[string]any{"name": "wippy.bootloader:env_storage", "value": "app.env:store"},
				},
			}),
		},
		{
			ID:   registry.NewID("app.deps", "bootloader_alt"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "wippy/bootloader",
				"parameters": []any{
					map[string]any{"name": "wippy.bootloader:env_storage", "value": "app:env_storage"},
				},
			}),
		},
		{
			ID:   registry.NewID("wippy.bootloader", "env_storage"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "wippy/bootloader"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "env_loader", "path": ".meta.storage"},
				},
			}),
		},
		{
			ID:   registry.NewID("wippy.bootloader", "env_loader"),
			Kind: "env.loader",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirementModules([]string{"wippy/bootloader"}))
	err := stage.Execute(ctx, &entries)
	require.Error(t, err)
	assert.ErrorContains(t, err, "parameter conflict")
	assert.ErrorContains(t, err, "env_storage=app.env:store")
	assert.ErrorContains(t, err, "env_storage=app:env_storage")

	target := findEntry(entries, "wippy.bootloader", "env_loader")
	require.NotNil(t, target)
	_, set := target.Meta["storage"]
	assert.False(t, set)
}

func TestLink_FullAndBareParametersForSameRequirementConflict(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app.deps", "views"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "wippy/views",
				"parameters": []any{
					map[string]any{"name": "wippy.views:api_router", "value": "app:api.views"},
					map[string]any{"name": "api_router", "value": "app:api"},
				},
			}),
		},
		{
			ID:   registry.NewID("wippy.views", "api_router"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "wippy/views"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "pages_endpoint", "path": ".meta.router"},
				},
			}),
		},
		{
			ID:   registry.NewID("wippy.views", "pages_endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirementModules([]string{"wippy/views"}))
	err := stage.Execute(ctx, &entries)
	require.Error(t, err)
	assert.ErrorContains(t, err, "parameter conflict")
	assert.ErrorContains(t, err, "api_router=app:api.views")
	assert.ErrorContains(t, err, "api_router=app:api")
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

func TestLink_ComponentNamespaceFullIDParameterName(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.telegram"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "butschster/telegram",
				"parameters": []any{
					map[string]any{
						"name":  "butschster.telegram:env_storage",
						"value": "app.env:file",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("telegram", "env_storage"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{
				"module": "butschster/telegram",
			},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "telegram:webhook_url",
						"path":  ".storage",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("telegram", "webhook_url"),
			Kind: "env.variable",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	webhookURL := findEntry(entries, "telegram", "webhook_url")
	require.NotNil(t, webhookURL)
	data := webhookURL.Data.Data().(map[string]any)
	assert.Equal(t, "app.env:file", data["storage"])
}

func TestLink_FullyQualifiedParameterDoesNotCrossRequirementNamespace(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.facade"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "wippy/facade",
				"parameters": []any{
					map[string]any{
						"name":  "wippy.facade:router",
						"value": "app:api.public",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("app", "__dependency.dummy"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "wippy/dummy",
				"parameters": []any{
					map[string]any{
						"name":  "wippy.dummy:router",
						"value": "app:api",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("wippy.facade", "router"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "wippy/facade"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "facade_endpoint",
						"path":  ".meta.router",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("wippy.dummy", "router"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "wippy/dummy"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "dummy_endpoint",
						"path":  ".meta.router",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("wippy.facade", "facade_endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("wippy.dummy", "dummy_endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	facadeEndpoint := findEntry(entries, "wippy.facade", "facade_endpoint")
	require.NotNil(t, facadeEndpoint)
	assert.Equal(t, "app:api.public", facadeEndpoint.Meta["router"])

	dummyEndpoint := findEntry(entries, "wippy.dummy", "dummy_endpoint")
	require.NotNil(t, dummyEndpoint)
	assert.Equal(t, "app:api", dummyEndpoint.Meta["router"])
}

// TestLink_SameBareNameOwnedRequirementsResolveByFullID verifies that a
// dependency owning two requirements with the same bare name in different
// namespaces links cleanly when its parameters address each by its full
// requirement id. Owning same-bare-named requirements is legal; the collision
// only matters when a parameter uses the ambiguous bare name.
func TestLink_SameBareNameOwnedRequirementsResolveByFullID(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app.deps", "core"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "kickside/core",
				"parameters": []any{
					map[string]any{"name": "kickside.core.projections:api_router", "value": "app:router.proj"},
					map[string]any{"name": "kickside.core.retention:api_router", "value": "app:router.ret"},
				},
			}),
		},
		{
			ID:   registry.NewID("kickside.core.projections", "api_router"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "kickside/core"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "proj_endpoint", "path": ".meta.router"},
				},
			}),
		},
		{
			ID:   registry.NewID("kickside.core.retention", "api_router"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "kickside/core"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "ret_endpoint", "path": ".meta.router"},
				},
			}),
		},
		{
			ID:   registry.NewID("kickside.core.projections", "proj_endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("kickside.core.retention", "ret_endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirementModules([]string{"kickside/core"}))
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	proj := findEntry(entries, "kickside.core.projections", "proj_endpoint")
	require.NotNil(t, proj)
	assert.Equal(t, "app:router.proj", proj.Meta["router"])

	ret := findEntry(entries, "kickside.core.retention", "ret_endpoint")
	require.NotNil(t, ret)
	assert.Equal(t, "app:router.ret", ret.Meta["router"])
}

// TestLink_AmbiguousBareNameOwnedRequirementsFails verifies that when a
// dependency owns two requirements with the same bare name in different
// namespaces, a parameter using the ambiguous bare name fails loudly rather
// than silently binding to one of them.
func TestLink_AmbiguousBareNameOwnedRequirementsFails(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app.deps", "core"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "kickside/core",
				"parameters": []any{
					map[string]any{"name": "api_router", "value": "app:router"},
				},
			}),
		},
		{
			ID:   registry.NewID("kickside.core.projections", "api_router"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "kickside/core"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "proj_endpoint", "path": ".meta.router"},
				},
			}),
		},
		{
			ID:   registry.NewID("kickside.core.retention", "api_router"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "kickside/core"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "ret_endpoint", "path": ".meta.router"},
				},
			}),
		},
	}

	stage := Link(WithStrictRequirementModules([]string{"kickside/core"}))
	err := stage.Execute(ctx, &entries)
	require.Error(t, err)
	assert.ErrorContains(t, err, "ambiguously addresses")
	assert.ErrorContains(t, err, "api_router")
	assert.ErrorContains(t, err, "kickside.core.projections:api_router")
	assert.ErrorContains(t, err, "kickside.core.retention:api_router")
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

func TestLink_StrictRequirementsFailsOnMissingValue(t *testing.T) {
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

	stage := Link(WithStrictRequirements())
	err := stage.Execute(ctx, &entries)
	require.ErrorContains(t, err, "unresolved requirements")
}

func TestLink_StrictRequirementsFailsOnMissingTarget(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.telegram"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "butschster/telegram",
				"parameters": []any{
					map[string]any{
						"name":  "telegram:webhook_router",
						"value": "app:api",
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
			ID:   registry.NewID("telegram.handler", "webhook.endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{
				"router": "telegram:router",
			},
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirements())
	err := stage.Execute(ctx, &entries)
	require.ErrorContains(t, err, "unresolved requirements")
	require.ErrorContains(t, err, "telegram.handler:webhook_endpoint")
}

func TestLink_StrictRequirementModulesIgnoresUnrelatedRequirements(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "unconfigured_req"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "service",
						"path":  ".ignored",
					},
				},
			}),
		},
		{
			ID:   registry.NewID("acme.module", "router"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{
				"module": "acme/module",
			},
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
			ID:   registry.NewID("acme.module", "endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirementModules([]string{"acme/module"}))
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	endpoint := findEntry(entries, "acme.module", "endpoint")
	require.NotNil(t, endpoint)
	assert.Equal(t, "app:api", endpoint.Meta["router"])
}

func TestLink_StrictRequirementModulesEmptyScopeDoesNotFailAppRequirements(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "unconfigured_req"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{
						"entry": "missing",
						"path":  ".value",
					},
				},
			}),
		},
	}

	stage := Link(WithStrictRequirementModules(nil))
	err := stage.Execute(ctx, &entries)
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

// TestLink_EmptyDefaultResolvesUnderStrict verifies that a requirement whose
// default is present but empty ("") is treated as a valid resolved value
// rather than an unresolved requirement, even under strict enforcement.
func TestLink_EmptyDefaultResolvesUnderStrict(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("acme.module", "opt"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "acme/module"},
			Data: payload.New(map[string]any{
				"default": "", // present but empty: optional, resolves to ""
				"targets": []any{
					map[string]any{"entry": "endpoint", "path": ".meta.opt"},
				},
			}),
		},
		{
			ID:   registry.NewID("acme.module", "endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirementModules([]string{"acme/module"}))
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	endpoint := findEntry(entries, "acme.module", "endpoint")
	require.NotNil(t, endpoint)
	assert.Equal(t, "", endpoint.Meta["opt"])
}

// TestLink_EmptyProvidedValueResolvesUnderStrict verifies that a dependency
// parameter supplying an empty value satisfies a requirement (and does not
// fall through to the unresolved path) under strict enforcement.
func TestLink_EmptyProvidedValueResolvesUnderStrict(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.module"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/module",
				"parameters": []any{
					map[string]any{"name": "opt", "value": ""},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "opt"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "vendor/module"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "endpoint", "path": ".meta.opt"},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{},
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirementModules([]string{"vendor/module"}))
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)
}

// TestLink_IntDefaultLandsAsInt verifies that an integer default resolves and
// lands in the target entry data as an int, and that decoding the target into a
// struct with an int field yields the value.
func TestLink_IntDefaultLandsAsInt(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "concurrency"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": 20,
				"targets": []any{
					map[string]any{
						"entry": "service",
						"path":  ".concurrency",
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

	target := findEntry(entries, "test", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	assert.Equal(t, 20, data["concurrency"])

	type serviceConfig struct {
		Concurrency int `json:"concurrency"`
	}
	var cfg serviceConfig
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &cfg))
	assert.Equal(t, 20, cfg.Concurrency)
}

// TestLink_StringDefaultStaysString verifies that a quoted numeric default is
// kept as a string end to end.
func TestLink_StringDefaultStaysString(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "label"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "20",
				"targets": []any{
					map[string]any{"entry": "service", "path": ".label"},
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

	target := findEntry(entries, "test", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	assert.Equal(t, "20", data["label"])
}

// TestLink_BoolAndFloatDefaults verifies that bool and float defaults flow into
// targets in their source types.
func TestLink_BoolAndFloatDefaults(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "enabled"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": true,
				"targets": []any{
					map[string]any{"entry": "service", "path": ".enabled"},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "ratio"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": 0.5,
				"targets": []any{
					map[string]any{"entry": "service", "path": ".ratio"},
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

	target := findEntry(entries, "test", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	assert.Equal(t, true, data["enabled"])
	assert.Equal(t, 0.5, data["ratio"])
}

// TestLink_TypedDependencyParameter verifies that a typed dependency parameter
// value flows into the requirement target unchanged.
func TestLink_TypedDependencyParameter(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.module"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/module",
				"parameters": []any{
					map[string]any{"name": "concurrency", "value": 8},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "concurrency"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "service", "path": ".concurrency"},
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

	target := findEntry(entries, "vendor.module", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	assert.Equal(t, 8, data["concurrency"])
}

// TestLink_TypedParameterSameValueNoConflict verifies that two dependencies
// supplying the same non-string value do not raise a conflict.
func TestLink_TypedParameterSameValueNoConflict(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.a"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/module",
				"parameters": []any{
					map[string]any{"name": "workers", "value": 4},
				},
			}),
		},
		{
			ID:   registry.NewID("app", "__dependency.b"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/module",
				"parameters": []any{
					map[string]any{"name": "workers", "value": 4},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "workers"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "vendor/module"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "service", "path": ".workers"},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirementModules([]string{"vendor/module"}))
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	target := findEntry(entries, "vendor.module", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	assert.Equal(t, 4, data["workers"])
}

// TestLink_TypedParameterConflictLeavesUnresolved verifies that two dependencies
// supplying different non-string values conflict, leaving the requirement
// unresolved under strict enforcement with a readable value in the error.
func TestLink_TypedParameterConflictLeavesUnresolved(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "__dependency.a"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/module",
				"parameters": []any{
					map[string]any{"name": "workers", "value": 4},
				},
			}),
		},
		{
			ID:   registry.NewID("app", "__dependency.b"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/module",
				"parameters": []any{
					map[string]any{"name": "workers", "value": 8},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "workers"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "vendor/module"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "service", "path": ".workers"},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.module", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirementModules([]string{"vendor/module"}))
	err := stage.Execute(ctx, &entries)
	require.Error(t, err)
	assert.ErrorContains(t, err, "workers=4")
	assert.ErrorContains(t, err, "workers=8")

	target := findEntry(entries, "vendor.module", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	_, set := data["workers"]
	assert.False(t, set)
}

// TestLink_NullDefaultLeavesMandatory verifies that an explicit null default is
// treated identically to an absent one: the requirement stays mandatory and
// fails under strict enforcement.
func TestLink_NullDefaultLeavesMandatory(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "mandatory"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": nil,
				"targets": []any{
					map[string]any{"entry": "service", "path": ".field"},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirements())
	err := stage.Execute(ctx, &entries)
	require.ErrorContains(t, err, "unresolved requirements")
}

// TestLink_PlaceholderDefaultPassesThroughUntouched verifies that a default
// containing an env placeholder is a string carried into the target verbatim,
// leaving later resolution to service decode time.
func TestLink_PlaceholderDefaultPassesThroughUntouched(t *testing.T) {
	ctx, _ := setupTestContext()

	const placeholder = "${env:DATABASE_URL}"

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "db_url"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": placeholder,
				"targets": []any{
					map[string]any{"entry": "service", "path": ".database.url"},
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

	target := findEntry(entries, "test", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	database := data["database"].(map[string]any)
	assert.Equal(t, placeholder, database["url"])
}

// TestLink_AppendTypedValue verifies that typed values append into an array
// target via the "+=" operator.
func TestLink_AppendTypedValue(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("test", "port_req"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": 9090,
				"targets": []any{
					map[string]any{"entry": "service", "path": ".ports +="},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{
				"ports": []any{8080},
			}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	target := findEntry(entries, "test", "service")
	require.NotNil(t, target)
	data := target.Data.Data().(map[string]any)
	assert.Equal(t, []any{8080, 9090}, data["ports"])
}

// TestLink_BareParameterResolvesWithinOwnModule verifies a bare parameter name
// resolves through its dependency's owned index and does not leak to a
// same-named requirement in an unrelated namespace.
func TestLink_BareParameterResolvesWithinOwnModule(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app.deps", "alpha"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/alpha",
				"parameters": []any{
					map[string]any{"name": "token", "value": "alpha-token"},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.alpha", "token"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "service", "path": ".token"},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.beta", "token"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "beta-default",
				"targets": []any{
					map[string]any{"entry": "beta_service", "path": ".token"},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.alpha", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("vendor.beta", "beta_service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	service := findEntry(entries, "vendor.alpha", "service")
	require.NotNil(t, service)
	assert.Equal(t, "alpha-token", service.Data.Data().(map[string]any)["token"])

	beta := findEntry(entries, "vendor.beta", "beta_service")
	require.NotNil(t, beta)
	assert.Equal(t, "beta-default", beta.Data.Data().(map[string]any)["token"])
}

// TestLink_FullParameterResolvesExactRequirementNotOwned verifies that a full
// ns:name parameter resolves to the exact registry requirement id even when the
// dependency also owns a same-named requirement under its module namespace.
func TestLink_FullParameterResolvesExactRequirementNotOwned(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app.deps", "alpha"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/alpha",
				"parameters": []any{
					map[string]any{"name": "shared:token", "value": "exact-token"},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.alpha", "token"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "owned-default",
				"targets": []any{
					map[string]any{"entry": "service", "path": ".token"},
				},
			}),
		},
		{
			ID:   registry.NewID("shared", "token"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "shared:service", "path": ".token"},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.alpha", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
		{
			ID:   registry.NewID("shared", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	shared := findEntry(entries, "shared", "service")
	require.NotNil(t, shared)
	assert.Equal(t, "exact-token", shared.Data.Data().(map[string]any)["token"])

	owned := findEntry(entries, "vendor.alpha", "service")
	require.NotNil(t, owned)
	assert.Equal(t, "owned-default", owned.Data.Data().(map[string]any)["token"])
}

// TestLink_NormalizationDeterministicAcrossShuffledOrderings verifies that the
// linked result does not depend on entry ordering.
func TestLink_NormalizationDeterministicAcrossShuffledOrderings(t *testing.T) {
	base := []registry.Entry{
		{
			ID:   registry.NewID("app.deps", "alpha"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/alpha",
				"parameters": []any{
					map[string]any{"name": "token", "value": "alpha-token"},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.alpha", "token"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "service", "path": ".token"},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.alpha", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	orderings := [][]int{
		{0, 1, 2},
		{2, 1, 0},
		{1, 0, 2},
		{2, 0, 1},
	}

	for _, order := range orderings {
		ctx, _ := setupTestContext()
		entries := make([]registry.Entry, len(order))
		for i, idx := range order {
			entries[i] = base[idx]
		}
		err := Link().Execute(ctx, &entries)
		require.NoError(t, err)
		service := findEntry(entries, "vendor.alpha", "service")
		require.NotNil(t, service)
		assert.Equal(t, "alpha-token", service.Data.Data().(map[string]any)["token"])
	}
}

// TestLink_OwnedRequirementAddressCollisionFailsLoud verifies that a dependency
// owning two requirements that map to the same address key fails with the
// ambiguity error rather than silently binding one of them.
func TestLink_OwnedRequirementAddressCollisionFailsLoud(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app.deps", "alpha"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "vendor/alpha",
				"parameters": []any{
					map[string]any{"name": "token", "value": "some-token"},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.alpha", "token"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "service", "path": ".token"},
				},
			}),
		},
		{
			ID:   registry.NewID("extra.ns", "token"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "vendor/alpha"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "extra.ns:service", "path": ".token"},
				},
			}),
		},
		{
			ID:   registry.NewID("vendor.alpha", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.Error(t, err)
	assert.ErrorContains(t, err, "ambiguously addresses")
	assert.ErrorContains(t, err, "app.deps:alpha")
	assert.ErrorContains(t, err, "extra.ns:token")
	assert.ErrorContains(t, err, "vendor.alpha:token")
}

// TestLink_TwoRequirementsSameTargetApplyDeterministically verifies that when
// two requirements write the same entry path, the value from the requirement
// with the sorted-last id wins, regardless of entry ordering.
func TestLink_TwoRequirementsSameTargetApplyDeterministically(t *testing.T) {
	base := []registry.Entry{
		{
			ID:   registry.NewID("test", "a_req"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "from_a",
				"targets": []any{
					map[string]any{"entry": "service", "path": ".field"},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "b_req"),
			Kind: registry.NamespaceRequirement,
			Data: payload.New(map[string]any{
				"default": "from_b",
				"targets": []any{
					map[string]any{"entry": "service", "path": ".field"},
				},
			}),
		},
		{
			ID:   registry.NewID("test", "service"),
			Kind: "process.lua",
			Data: payload.New(map[string]any{}),
		},
	}

	orderings := [][]int{
		{0, 1, 2},
		{1, 0, 2},
		{2, 1, 0},
	}

	for _, order := range orderings {
		ctx, _ := setupTestContext()
		entries := make([]registry.Entry, len(order))
		for i, idx := range order {
			entries[i] = base[idx]
		}
		err := Link().Execute(ctx, &entries)
		require.NoError(t, err)
		service := findEntry(entries, "test", "service")
		require.NotNil(t, service)
		assert.Equal(t, "from_b", service.Data.Data().(map[string]any)["field"])
	}
}

// TestLink_RootParameterBeatsTransitiveBareAliasSpelling verifies root-over-
// transitive precedence across alias spellings: a root dependency supplying a
// requirement by its exact id beats a transitive dependency supplying the SAME
// concrete requirement by a bare name. The root value wins with no conflict.
func TestLink_RootParameterBeatsTransitiveBareAliasSpelling(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app.deps", "telegram_root"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "butschster/telegram",
				"parameters": []any{
					map[string]any{"name": "telegram:env_storage", "value": "app:root_env"},
				},
			}),
		},
		{
			ID:   registry.NewID("app", "dep.telegram"),
			Kind: registry.NamespaceDependency,
			Meta: map[string]any{"module": "wippy/migration"},
			Data: payload.New(map[string]any{
				"component": "butschster/telegram",
				"parameters": []any{
					map[string]any{"name": "env_storage", "value": "app:transitive_env"},
				},
			}),
		},
		{
			ID:   registry.NewID("telegram", "env_storage"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "butschster/telegram"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "telegram:bot_token", "path": ".storage"},
				},
			}),
		},
		{
			ID:   registry.NewID("telegram", "bot_token"),
			Kind: "env.variable",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirementModules([]string{"butschster/telegram"}))
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	botToken := findEntry(entries, "telegram", "bot_token")
	require.NotNil(t, botToken)
	data := botToken.Data.Data().(map[string]any)
	assert.Equal(t, "app:root_env", data["storage"])
}

// TestLink_RootParameterBeatsTransitiveComponentNamespaceAliasSpelling verifies
// the same precedence when the transitive dependency addresses the requirement
// via its component-namespace-qualified alias rather than the requirement's own
// registry namespace.
func TestLink_RootParameterBeatsTransitiveComponentNamespaceAliasSpelling(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app.deps", "telegram_root"),
			Kind: registry.NamespaceDependency,
			Data: payload.New(map[string]any{
				"component": "butschster/telegram",
				"parameters": []any{
					map[string]any{"name": "telegram:env_storage", "value": "app:root_env"},
				},
			}),
		},
		{
			ID:   registry.NewID("app", "dep.telegram"),
			Kind: registry.NamespaceDependency,
			Meta: map[string]any{"module": "wippy/migration"},
			Data: payload.New(map[string]any{
				"component": "butschster/telegram",
				"parameters": []any{
					map[string]any{"name": "butschster.telegram:env_storage", "value": "app:transitive_env"},
				},
			}),
		},
		{
			ID:   registry.NewID("telegram", "env_storage"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "butschster/telegram"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "telegram:bot_token", "path": ".storage"},
				},
			}),
		},
		{
			ID:   registry.NewID("telegram", "bot_token"),
			Kind: "env.variable",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link()
	err := stage.Execute(ctx, &entries)
	require.NoError(t, err)

	botToken := findEntry(entries, "telegram", "bot_token")
	require.NotNil(t, botToken)
	data := botToken.Data.Data().(map[string]any)
	assert.Equal(t, "app:root_env", data["storage"])
}

// TestLink_TransitiveOnlyDuplicateForSameRequirementConflicts verifies that two
// transitive dependencies (provenance-equal) resolving the same concrete
// requirement to different values still conflict fail-loud: precedence only
// resolves root-vs-transitive, never transitive-vs-transitive.
func TestLink_TransitiveOnlyDuplicateForSameRequirementConflicts(t *testing.T) {
	ctx, _ := setupTestContext()

	entries := []registry.Entry{
		{
			ID:   registry.NewID("app", "dep.telegram.a"),
			Kind: registry.NamespaceDependency,
			Meta: map[string]any{"module": "wippy/migration"},
			Data: payload.New(map[string]any{
				"component": "butschster/telegram",
				"parameters": []any{
					map[string]any{"name": "telegram:env_storage", "value": "app:env_a"},
				},
			}),
		},
		{
			ID:   registry.NewID("app", "dep.telegram.b"),
			Kind: registry.NamespaceDependency,
			Meta: map[string]any{"module": "wippy/other"},
			Data: payload.New(map[string]any{
				"component": "butschster/telegram",
				"parameters": []any{
					map[string]any{"name": "env_storage", "value": "app:env_b"},
				},
			}),
		},
		{
			ID:   registry.NewID("telegram", "env_storage"),
			Kind: registry.NamespaceRequirement,
			Meta: map[string]any{"module": "butschster/telegram"},
			Data: payload.New(map[string]any{
				"targets": []any{
					map[string]any{"entry": "telegram:bot_token", "path": ".storage"},
				},
			}),
		},
		{
			ID:   registry.NewID("telegram", "bot_token"),
			Kind: "env.variable",
			Data: payload.New(map[string]any{}),
		},
	}

	stage := Link(WithStrictRequirementModules([]string{"butschster/telegram"}))
	err := stage.Execute(ctx, &entries)
	require.Error(t, err)
	assert.ErrorContains(t, err, "parameter conflict")
	assert.ErrorContains(t, err, "env_storage=app:env_a")
	assert.ErrorContains(t, err, "env_storage=app:env_b")

	botToken := findEntry(entries, "telegram", "bot_token")
	require.NotNil(t, botToken)
	data := botToken.Data.Data().(map[string]any)
	_, set := data["storage"]
	assert.False(t, set)
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
			if def, ok := dataMap["default"]; ok {
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
						if value, ok := pMap["value"]; ok {
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
