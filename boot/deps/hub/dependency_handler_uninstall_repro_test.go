// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

// mcpServerWappEntries mirrors the real butschster/mcp-server layout: entries
// live in the "mcp" namespace (not org.module), three SSE endpoints default to
// router app:api.public at /mcp, and a module-owned ns.requirement relocates
// their meta.router. Installing with param butschster.mcp-server:router=app:api
// moves them off app:api.public so they stop colliding with the host's own
// public MCP endpoint on that router.
func mcpServerWappEntries() []wapp.Entry {
	endpoint := func(name, method string) wapp.Entry {
		return wapp.Entry{
			ID:   wapp.NewID("mcp", name),
			Kind: "http.endpoint",
			Meta: map[string]any{"router": "app:api.public"},
			Data: map[string]any{"method": method, "path": "/mcp"},
		}
	}
	return []wapp.Entry{
		{
			ID:   wapp.NewID("mcp", "router"),
			Kind: regapi.NamespaceRequirement,
			Data: map[string]any{
				"targets": []any{
					map[string]any{"entry": "mcp:sse_endpoint_post", "path": "meta.router"},
					map[string]any{"entry": "mcp:sse_endpoint_get", "path": "meta.router"},
					map[string]any{"entry": "mcp:sse_endpoint_delete", "path": "meta.router"},
				},
			},
		},
		endpoint("sse_endpoint_post", "POST"),
		endpoint("sse_endpoint_get", "GET"),
		endpoint("sse_endpoint_delete", "DELETE"),
	}
}

// installedMcpSnapshotEntries is the post-install registry state for
// butschster/mcp-server installed with the router parameter: entries are stamped
// with the owning module and the endpoints carry the parameterized router.
func installedMcpSnapshotEntries(routerValue string) []regapi.Entry {
	mod := map[string]any{metaModuleKey: "butschster/mcp-server", metaModuleVersionKey: "1.6.2"}
	endpoint := func(name, method string) regapi.Entry {
		return regapi.Entry{
			ID:   regapi.NewID("mcp", name),
			Kind: "http.endpoint",
			Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "butschster/mcp-server", metaModuleVersionKey: "1.6.2", "router": routerValue}),
			Data: payload.NewPayload(fmt.Sprintf(`{"method":%q,"path":"/mcp"}`, method), payload.JSON),
		}
	}
	return []regapi.Entry{
		{
			ID:   regapi.NewID("mcp", "router"),
			Kind: regapi.NamespaceRequirement,
			Meta: attrs.NewBagFrom(mod),
			Data: payload.NewPayload(`{"targets":[{"entry":"mcp:sse_endpoint_post","path":"meta.router"},{"entry":"mcp:sse_endpoint_get","path":"meta.router"},{"entry":"mcp:sse_endpoint_delete","path":"meta.router"}]}`, payload.JSON),
		},
		endpoint("sse_endpoint_post", "POST"),
		endpoint("sse_endpoint_get", "GET"),
		endpoint("sse_endpoint_delete", "DELETE"),
	}
}

func mcpRootDep() regapi.Entry {
	return regapi.Entry{
		ID:   regapi.NewID("app.deps", "mcp-server"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"butschster/mcp-server","version":"1.6.2","parameters":[{"name":"butschster.mcp-server:router","value":"app:api"}]}`, payload.JSON),
	}
}

// DEFECT A (wedge): uninstalling the parameterized mcp-server root must remove
// its endpoints outright. Expansion must never re-admit an endpoint carrying the
// module default router app:api.public, which is what re-collides with the host's
// own POST /api/public/mcp and wedges the uninstall.
func TestReproDeleteParameterizedMcpRootRemovesEndpoints(t *testing.T) {
	ctx := newTestContext()
	vendorDir := filepath.Join(t.TempDir(), "vendor")
	writeWapp(t, filepath.Join(vendorDir, "butschster", "mcp-server-1.6.2.wapp"), mcpServerWappEntries())

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
			return &ModuleManifest{Org: org, Name: module, Version: version}, nil
		}},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	root := mcpRootDep()
	snapshot := append(regapi.State{root}, installedMcpSnapshotEntries("app:api")...)

	result, err := handler.Expand(ctx,
		regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: root.ID}},
		snapshot,
	)
	require.NoError(t, err)
	require.True(t, result.Applied)

	deleted := map[regapi.ID]bool{}
	for _, scoped := range result.Additional {
		op := scoped.Operation
		if op.Kind == regapi.EntryDelete {
			deleted[op.Entry.ID] = true
		}
		if (op.Kind == regapi.EntryCreate || op.Kind == regapi.EntryUpdate) && op.Entry.Kind == "http.endpoint" {
			router := op.Entry.Meta.GetString("router", "")
			t.Fatalf("endpoint %s re-admitted (op %v) with router=%q on mcp-server uninstall", op.Entry.ID, op.Kind, router)
		}
	}
	for _, name := range []string{"sse_endpoint_post", "sse_endpoint_get", "sse_endpoint_delete"} {
		assert.True(t, deleted[regapi.NewID("mcp", name)], "endpoint mcp:%s must be deleted on uninstall", name)
	}
	assert.True(t, deleted[regapi.NewID("mcp", "router")], "module requirement mcp:router must be deleted on uninstall")
}

// DEFECT A (misattributed conflict): deleting an unrelated parameterized root must
// leave the still-installed mcp-server endpoints on their parameterized router.
// A revert to app:api.public here is what surfaced the mcp route conflict while
// uninstalling wippy/dummy.
func TestReproDeleteUnrelatedRootPreservesMcpRouterParam(t *testing.T) {
	ctx := newTestContext()
	vendorDir := filepath.Join(t.TempDir(), "vendor")
	writeWapp(t, filepath.Join(vendorDir, "butschster", "mcp-server-1.6.2.wapp"), mcpServerWappEntries())
	writeWapp(t, filepath.Join(vendorDir, "wippy", "dummy-v1.0.0.wapp"), []wapp.Entry{
		{
			ID:   wapp.NewID("dummy", "router_req"),
			Kind: regapi.NamespaceRequirement,
			Data: map[string]any{"targets": []any{map[string]any{"entry": "endpoint", "path": "meta.router"}}},
		},
		{
			ID:   wapp.NewID("dummy", "endpoint"),
			Kind: "http.endpoint",
			Meta: map[string]any{"router": "app:api.public"},
			Data: map[string]any{"method": "POST", "path": "/dummy"},
		},
	})

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
			return &ModuleManifest{Org: org, Name: module, Version: version}, nil
		}},
		Logger:    zap.NewNop(),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	mcpRoot := mcpRootDep()
	dummyRoot := regapi.Entry{
		ID:   regapi.NewID("app.deps", "dummy"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"wippy/dummy","version":"v1.0.0","parameters":[{"name":"wippy.dummy:router_req","value":"app:api"}]}`, payload.JSON),
	}
	dummyReq := regapi.Entry{
		ID:   regapi.NewID("dummy", "router_req"),
		Kind: regapi.NamespaceRequirement,
		Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "wippy/dummy", metaModuleVersionKey: "v1.0.0"}),
		Data: payload.NewPayload(`{"targets":[{"entry":"dummy:endpoint","path":"meta.router"}]}`, payload.JSON),
	}
	dummyEndpoint := regapi.Entry{
		ID:   regapi.NewID("dummy", "endpoint"),
		Kind: "http.endpoint",
		Meta: attrs.NewBagFrom(map[string]any{metaModuleKey: "wippy/dummy", metaModuleVersionKey: "v1.0.0", "router": "app:api"}),
		Data: payload.NewPayload(`{"method":"POST","path":"/dummy"}`, payload.JSON),
	}

	snapshot := regapi.State{mcpRoot, dummyRoot, dummyReq, dummyEndpoint}
	snapshot = append(snapshot, installedMcpSnapshotEntries("app:api")...)

	result, err := handler.Expand(ctx,
		regapi.Operation{Kind: regapi.EntryDelete, Entry: regapi.Entry{ID: dummyRoot.ID}},
		snapshot,
	)
	require.NoError(t, err)
	require.True(t, result.Applied)

	for _, scoped := range result.Additional {
		op := scoped.Operation
		if op.Entry.ID.NS == "mcp" && op.Entry.Kind == "http.endpoint" {
			if op.Kind == regapi.EntryDelete {
				t.Fatalf("still-installed mcp endpoint %s deleted by unrelated dummy uninstall", op.Entry.ID)
			}
			assert.Equal(t, "app:api", op.Entry.Meta.GetString("router", ""),
				"mcp endpoint %s must keep its install-time router param during unrelated uninstall", op.Entry.ID)
		}
	}
}
